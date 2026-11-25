// Package httpsource implements the generic HTTP/REST connector, source type "http"
// (FR-040, FR-071…FR-073, ADR-0008).
//
// An instance is an operator-configured base URL with an authentication method. A query
// names a method, a path under the base URL, parameters, pagination and jq selectors;
// every page is fetched through the host's netguard client (NFR-SEC-005), decoded under
// a size and depth cap, and its rows selected with the sandboxed jq evaluator. Selected
// rows become one frame through the JSON format's table builder, so typing and limits
// match file sources.
package httpsource

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/jq"
	"github.com/yesoreyeram/dv/internal/netguard"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/jsonformat"
)

// ID is the plugin ID.
const ID = "http"

// Default result limits when a query sets none (like the file source).
const (
	DefaultMaxRows  = 1_000_000
	DefaultMaxBytes = 64 << 20
	maxRedirects    = 5
	userAgent       = "dv-http-connector/1"
)

// jqLimits are the selector budgets per page (FR-073 AC5).
var jqLimits = jq.Limits{}

// Options configure the source.
type Options struct {
	// Policy is the SSRF policy (NFR-SEC-005); its Allowed list comes from config.
	Policy netguard.Policy
	// Defaults are the result limits used when a query leaves a limit at zero.
	Defaults frame.Limits
	// Logger receives one line per outbound request (NFR-OBS-005); nil uses slog.Default.
	Logger *slog.Logger
	// Client overrides the netguard client (tests only).
	Client *http.Client
}

// Source is the http connector plugin.
type Source struct {
	client   *http.Client
	defaults frame.Limits
	log      *slog.Logger
	json     plugin.Format
}

// New returns the http source.
func New(o Options) *Source {
	if o.Defaults.MaxRows <= 0 {
		o.Defaults.MaxRows = DefaultMaxRows
	}
	if o.Defaults.MaxBytes <= 0 {
		o.Defaults.MaxBytes = DefaultMaxBytes
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	c := o.Client
	if c == nil {
		c = netguard.NewClient(o.Policy, netguard.Options{CheckRedirect: checkRedirect})
	}
	return &Source{client: c, defaults: o.Defaults, log: o.Logger, json: jsonformat.NewJSON()}
}

// safeRedirectHeaders survive a redirect to another origin; everything else, including
// API-key headers, is dropped (NFR-SEC-005 AC5, NFR-SEC-020 AC2).
var safeRedirectHeaders = map[string]bool{"Accept": true, "User-Agent": true, "Accept-Language": true}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	first := via[0].URL
	if req.URL.Scheme != "https" && (req.URL.Scheme != "http" || first.Scheme != "http") {
		return errors.New("redirect to a non-https URL refused")
	}
	if !sameOrigin(req.URL, first) {
		for k := range req.Header {
			if !safeRedirectHeaders[http.CanonicalHeaderKey(k)] {
				req.Header.Del(k)
			}
		}
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}

func port(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// Meta implements plugin.Source.
func (s *Source) Meta() plugin.SourceMeta {
	return plugin.SourceMeta{
		ID:   ID,
		Name: "HTTP API (JSON)",
		Description: "Any JSON HTTP API: basic, bearer or API-key authentication; offset, page, cursor, " +
			"next-URL or Link-header pagination; jq selectors turn responses into tables.",
		Capabilities: []string{"query", "auth", "pagination", "jq"},
	}
}

// Validate implements plugin.Source: settings are checked statically (FR-040 AC7).
func (s *Source) Validate(ctx context.Context, cfg plugin.SourceConfig) error {
	st, err := parseSettings(cfg)
	if err != nil {
		return err
	}
	if st.base.Scheme == "http" {
		s.log.WarnContext(ctx, "data source sends requests over plain http (allowInsecureHTTP); credentials and data are not encrypted in transit",
			"datasource", cfg.UID)
	}
	return nil
}

// Health implements plugin.Source.
func (s *Source) Health(_ context.Context, cfg plugin.SourceConfig) error {
	_, err := parseSettings(cfg)
	return err
}

func (s *Source) limits(q frame.Limits) frame.Limits {
	if q.MaxRows <= 0 {
		q.MaxRows = s.defaults.MaxRows
	}
	if q.MaxBytes <= 0 {
		q.MaxBytes = s.defaults.MaxBytes
	}
	return q
}

type compiled struct {
	rows    *jq.Program
	cursor  *jq.Program
	nextURL *jq.Program
	columns []*jq.Program
}

func compileSelectors(m *QueryModel) (*compiled, error) {
	c := &compiled{}
	comp := func(field, src string) (*jq.Program, error) {
		p, err := jq.Compile(src)
		if err != nil {
			return nil, invalidQuery("%s: %v", field, err)
		}
		return p, nil
	}
	var err error
	rows := m.Rows
	if strings.TrimSpace(rows) == "" {
		rows = defaultRowsSelector
	}
	if c.rows, err = comp("rows", rows); err != nil {
		return nil, err
	}
	if m.Pagination.Type == "cursor" {
		if c.cursor, err = comp("pagination.cursorSelector", m.Pagination.CursorSelector); err != nil {
			return nil, err
		}
	}
	if m.Pagination.Type == "nextUrl" {
		if c.nextURL, err = comp("pagination.nextUrlSelector", m.Pagination.NextURLSelector); err != nil {
			return nil, err
		}
	}
	for i, col := range m.Columns {
		p, err := comp(fmt.Sprintf("columns[%d].selector", i), col.Selector)
		if err != nil {
			return nil, err
		}
		c.columns = append(c.columns, p)
	}
	return c, nil
}

// Query implements plugin.Source.
func (s *Source) Query(ctx context.Context, q plugin.Query) (*frame.Result, error) {
	st, err := parseSettings(q.Source)
	if err != nil {
		return nil, err
	}
	m, err := parseQuery(q.Body, st)
	if err != nil {
		return nil, err
	}
	sel, err := compileSelectors(m)
	if err != nil {
		return nil, err
	}
	reqPath, err := resolvePath(st.base, m.Path)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	var order []string
	for _, p := range m.Params {
		v, err := expandMacros(p.Value, q.TimeRange)
		if err != nil {
			return nil, err
		}
		if _, ok := params[p.Name]; !ok {
			order = append(order, p.Name)
		}
		params.Add(p.Name, v)
	}
	headers := http.Header{}
	for _, h := range st.Headers {
		headers.Add(h.Name, h.Value)
	}
	for _, h := range m.Headers {
		v, err := expandMacros(h.Value, q.TimeRange)
		if err != nil {
			return nil, err
		}
		headers.Set(h.Name, v)
	}
	var body any
	if len(m.Body) > 0 {
		b, err := jq.Decode(bytes.NewReader(m.Body), jq.DefaultMaxDepth)
		if err != nil {
			return nil, invalidQuery("body: %v", err)
		}
		if body, err = expandBody(b, q.TimeRange); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, st.timeout)
	defer cancel()

	lim := s.limits(q.Limits)
	run := &pager{
		s: s, st: st, m: m, sel: sel, sec: q.Source.SecureJSONData, uid: q.Source.UID,
		headers: headers, params: params, order: order, body: body, lim: lim,
	}
	base := *st.base
	base.Path = reqPath
	rows, err := run.fetchAll(ctx, &base)
	if err != nil {
		return nil, err
	}
	fr, err := s.buildFrame(ctx, m, rows, lim)
	if err != nil {
		return nil, err
	}
	fr.Name = path.Base(strings.TrimSuffix(reqPath, "/"))
	if fr.Name == "/" || fr.Name == "." {
		fr.Name = q.Source.Name
	}
	fr.Meta.Source = ID
	fr.Meta.ExecutedQuery = m.Method + " " + reqPath
	fr.Meta.Pages = run.pages
	if run.stoppedByPages {
		fr.Meta.Truncated = true
		fr.AddNotice(frame.SeverityWarning, fmt.Sprintf("Stopped after %d pages (maxPages); more data is available.", run.pages))
	}
	return &frame.Result{Frames: []*frame.Frame{fr}}, nil
}

func expandBody(v any, tr plugin.TimeRange) (any, error) {
	switch t := v.(type) {
	case string:
		return expandMacros(t, tr)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			x, err := expandBody(e, tr)
			if err != nil {
				return nil, err
			}
			out[i] = x
		}
		return out, nil
	case *jq.Object:
		out := jq.NewObject(t.Len())
		for _, k := range t.Keys() {
			e, _ := t.Get(k)
			x, err := expandBody(e, tr)
			if err != nil {
				return nil, err
			}
			out.Set(k, x)
		}
		return out, nil
	}
	return v, nil
}

// buildFrame turns selected rows into a frame with the JSON format's table builder.
func (s *Source) buildFrame(ctx context.Context, m *QueryModel, rows *bytes.Buffer, lim frame.Limits) (*frame.Frame, error) {
	types := map[string]frame.FieldType{}
	for k, v := range m.FieldTypes {
		types[k] = v
	}
	for _, c := range m.Columns {
		if c.Type != "" {
			types[c.Name] = c.Type
		}
	}
	opts, _ := json.Marshal(jsonformat.Options{Flatten: m.Flatten && len(m.Columns) == 0})
	frames, err := s.json.Decode(ctx, rows, plugin.DecodeOptions{Limits: lim, FieldTypes: types, Options: opts})
	if err != nil {
		if errors.Is(err, plugin.ErrOptions) {
			return nil, fmt.Errorf("%w: %w", plugin.ErrInvalidQuery, err)
		}
		return nil, err
	}
	if len(frames) == 0 {
		return frame.New("")
	}
	return frames[0], nil
}

// pager runs the pagination loop (FR-072).
type pager struct {
	s       *Source
	st      *Settings
	m       *QueryModel
	sel     *compiled
	sec     plugin.SecureValues
	uid     string
	headers http.Header
	params  url.Values
	order   []string
	body    any
	lim     frame.Limits

	pages          int
	rows           int
	stoppedByPages bool
	buf            bytes.Buffer
}

func (p *pager) fetchAll(ctx context.Context, first *url.URL) (*bytes.Buffer, error) {
	pg := p.m.Pagination
	p.buf.WriteByte('[')
	cur := first
	params := cloneValues(p.params)
	body := p.body
	var offset, page int
	if pg.Start != nil {
		offset, page = *pg.Start, *pg.Start
	}
	switch pg.Type {
	case "offset":
		params.Set(pg.OffsetParam, strconv.Itoa(offset))
		params.Set(pg.LimitParam, strconv.Itoa(pg.Limit))
	case "page":
		params.Set(pg.PageParam, strconv.Itoa(page))
		if pg.SizeParam != "" {
			params.Set(pg.SizeParam, strconv.Itoa(pg.Size))
		}
	}
	seenCursors := map[string]bool{}
	absolute := false // cur already carries its query (nextUrl / Link)
	for {
		u := *cur
		if !absolute {
			u.RawQuery = p.encode(params)
		}
		doc, hdr, err := p.fetch(ctx, &u, body)
		if err != nil {
			return nil, err
		}
		p.pages++
		n, full, err := p.selectRows(ctx, doc)
		if err != nil {
			return nil, err
		}
		if full {
			break
		}
		// Decide the next page.
		var next *url.URL
		more := false
		switch pg.Type {
		case "none":
		case "offset":
			if n >= pg.Limit {
				offset += n
				params.Set(pg.OffsetParam, strconv.Itoa(offset))
				more = true
			}
		case "page":
			if (pg.Size > 0 && n >= pg.Size) || (pg.Size == 0 && n > 0) {
				page++
				params.Set(pg.PageParam, strconv.Itoa(page))
				more = true
			}
		case "cursor":
			c, ok, err := p.first(ctx, p.sel.cursor, doc, "pagination.cursorSelector")
			if err != nil {
				return nil, err
			}
			cs, isStr := cursorString(c)
			if ok && isStr && cs != "" && !seenCursors[cs] {
				seenCursors[cs] = true
				more = true
				if pg.CursorBodyPath != "" {
					if body, err = setBodyPath(body, pg.CursorBodyPath, cs); err != nil {
						return nil, err
					}
				} else {
					if pg.CursorOnly {
						params = url.Values{}
					}
					params.Set(pg.CursorParam, cs)
				}
			}
		case "nextUrl":
			v, ok, err := p.first(ctx, p.sel.nextURL, doc, "pagination.nextUrlSelector")
			if err != nil {
				return nil, err
			}
			if s, isStr := v.(string); ok && isStr && s != "" {
				if next, err = p.resolveNext(&u, s); err != nil {
					return nil, err
				}
				more = true
			}
		case "linkHeader":
			if s := nextLink(hdr.Values("Link")); s != "" {
				if next, err = p.resolveNext(&u, s); err != nil {
					return nil, err
				}
				more = true
			}
		}
		if !more {
			break
		}
		if p.pages >= pg.MaxPages {
			p.stoppedByPages = true
			break
		}
		if next != nil {
			cur, absolute = next, true
		}
	}
	p.buf.WriteByte(']')
	return &p.buf, nil
}

func cursorString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return string(t), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}
	return "", false
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// encode keeps the query's parameter order, then pagination parameters, then the auth
// query keys (applied in fetch).
func (p *pager) encode(params url.Values) string {
	var b strings.Builder
	done := map[string]bool{}
	write := func(k string) {
		if done[k] {
			return
		}
		done[k] = true
		for _, v := range params[k] {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(v))
		}
	}
	for _, k := range p.order {
		write(k)
	}
	rest := make([]string, 0, len(params))
	for k := range params {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		write(k)
	}
	return b.String()
}

// resolveNext resolves a next-page reference and enforces FR-072 AC5: same origin as the
// base URL and a path under the base path, so credentials never leave the connection.
func (p *pager) resolveNext(cur *url.URL, ref string) (*url.URL, error) {
	if hasControl(ref) || len(ref) > 8192 {
		return nil, fmt.Errorf("%w: invalid next page URL", plugin.ErrUpstream)
	}
	r, err := url.Parse(ref)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid next page URL", plugin.ErrUpstream)
	}
	next := cur.ResolveReference(r)
	next.Fragment = ""
	if !sameOrigin(next, p.st.base) || next.User != nil || !within(p.st.base.Path, path.Clean("/"+next.Path)) {
		return nil, fmt.Errorf("%w: next page URL leaves the connection's base URL", plugin.ErrUpstream)
	}
	return next, nil
}

// fetch sends one request and decodes the JSON body.
func (p *pager) fetch(ctx context.Context, u *url.URL, body any) (any, http.Header, error) {
	// Credentials are added here, per request, and only for the base origin
	// (NFR-SEC-020 AC2); resolveNext already refused other origins.
	if !sameOrigin(u, p.st.base) {
		return nil, nil, fmt.Errorf("%w: request URL leaves the connection's base URL", plugin.ErrUpstream)
	}
	final := *u
	q := final.Query()
	authQuery := false
	for _, k := range p.st.Auth.Keys {
		if k.In == "query" {
			v, _ := p.sec.Get(k.Secret)
			q.Set(k.Name, k.Prefix+v)
			authQuery = true
		}
	}
	if authQuery {
		final.RawQuery = q.Encode()
	}
	var rdr io.Reader
	var bodyBytes []byte
	if body != nil {
		var err error
		if bodyBytes, err = jq.Marshal(body, jq.DefaultMaxDepth); err != nil {
			return nil, nil, invalidQuery("body: %v", err)
		}
		rdr = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, p.m.Method, final.String(), rdr)
	if err != nil {
		return nil, nil, invalidQuery("could not build the request")
	}
	req.Header = p.headers.Clone()
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch p.st.Auth.Type {
	case "basic":
		user := p.st.Auth.Username
		if user == "" {
			user, _ = p.sec.Get("username")
		}
		pass, _ := p.sec.Get("password")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	case "bearer":
		tok, _ := p.sec.Get("token")
		req.Header.Set("Authorization", "Bearer "+tok)
	case "apiKey":
		for _, k := range p.st.Auth.Keys {
			if k.In == "header" {
				v, _ := p.sec.Get(k.Secret)
				req.Header.Set(k.Name, k.Prefix+v)
			}
		}
	}
	start := time.Now()
	logAttrs := []any{"datasource", p.uid, "method", p.m.Method, "host", u.Host, "path", u.Path, "page", p.pages + 1}
	resp, err := p.s.client.Do(req)
	if err != nil {
		p.s.log.WarnContext(ctx, "upstream request failed", append(logAttrs, "duration_ms", time.Since(start).Milliseconds(), "error", classify(err))...)
		return nil, nil, mapTransportError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		p.s.log.WarnContext(ctx, "upstream request failed", append(logAttrs, "status", resp.StatusCode, "duration_ms", time.Since(start).Milliseconds())...)
		return nil, nil, statusError(resp)
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mt != "" && mt != "application/json" && !strings.HasSuffix(mt, "+json") && mt != "text/json" {
		p.s.log.WarnContext(ctx, "upstream request failed", append(logAttrs, "status", resp.StatusCode, "media_type", mt)...)
		return nil, nil, fmt.Errorf("%w: the upstream answered %q; only JSON responses are supported", plugin.ErrUnsupportedMediaType, mt)
	}
	cr := &countingReader{r: netguard.LimitBody(resp.Body, p.st.MaxResponseBytes)}
	doc, err := jq.Decode(cr, jq.DefaultMaxDepth)
	p.s.log.DebugContext(ctx, "upstream request", append(logAttrs, "status", resp.StatusCode, "bytes", cr.n, "duration_ms", time.Since(start).Milliseconds())...)
	if err != nil {
		switch {
		case errors.Is(err, netguard.ErrTooLarge):
			return nil, nil, fmt.Errorf("%w: a page is larger than maxResponseBytes (%d bytes)", plugin.ErrResponseTooLarge, p.st.MaxResponseBytes)
		case ctx.Err() != nil:
			return nil, nil, ctx.Err()
		case errors.Is(err, jq.ErrDepth):
			return nil, nil, fmt.Errorf("%w: the response nests more than %d levels", plugin.ErrUpstream, jq.DefaultMaxDepth)
		}
		return nil, nil, fmt.Errorf("%w: the response is not valid JSON", plugin.ErrUpstream)
	}
	return doc, resp.Header, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// classify names a transport failure for logs without the URL (which may hold keys).
func classify(err error) string {
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		var be *netguard.BlockedError
		if errors.As(err, &be) {
			return "blocked: " + be.Category
		}
		return "blocked"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return "transport error"
}

func mapTransportError(ctx context.Context, err error) error {
	var be *netguard.BlockedError
	switch {
	case errors.As(err, &be):
		return fmt.Errorf("%w: %s", plugin.ErrDestinationNotAllowed, be.Error())
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	}
	msg := "could not connect to the upstream"
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil && strings.Contains(ue.Err.Error(), "redirect") {
		msg = "the upstream redirect was refused: " + ue.Err.Error()
	}
	if strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "x509:") {
		msg = "the TLS handshake with the upstream failed"
	}
	return fmt.Errorf("%w: %s", plugin.ErrUpstreamUnreachable, msg)
}

func statusError(resp *http.Response) error {
	code := resp.StatusCode
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: the upstream answered %d", plugin.ErrUpstreamAuth, code)
	case http.StatusNotFound:
		return fmt.Errorf("%w: the upstream answered 404", plugin.ErrUpstreamNotFound)
	case http.StatusTooManyRequests:
		if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" && len(ra) < 40 && !hasControl(ra) {
			return fmt.Errorf("%w: the upstream answered 429; retry after %s", plugin.ErrUpstreamRateLimited, ra)
		}
		return fmt.Errorf("%w: the upstream answered 429", plugin.ErrUpstreamRateLimited)
	}
	return fmt.Errorf("%w: the upstream answered %d", plugin.ErrUpstream, code)
}

// selectRows runs the rows (and column) selectors on a page and appends rows to the
// buffer. full reports that the row limit (plus one, to mark truncation) is reached or
// the buffer is far past the byte limit, so paging stops (FR-072 AC6, NFR-PERF-006).
func (p *pager) selectRows(ctx context.Context, doc any) (n int, full bool, err error) {
	maxRows := p.lim.MaxRows + 1
	maxBuf := 4*p.lim.MaxBytes + 1024
	stop := errors.New("stop")
	err = p.sel.rows.Run(ctx, doc, jqLimits, func(row any) error {
		n++
		if p.rows >= maxRows {
			full = true
			return stop
		}
		if len(p.sel.columns) > 0 {
			obj := jq.NewObject(len(p.sel.columns))
			for i, c := range p.sel.columns {
				v, _, err := c.First(ctx, row, jqLimits)
				if err != nil {
					var re *jq.RuntimeError
					if !errors.As(err, &re) {
						return err
					}
					v = nil // FR-073 AC6: an error becomes null
				}
				obj.Set(p.m.Columns[i].Name, v)
			}
			row = obj
		}
		if p.rows > 0 {
			p.buf.WriteByte(',')
		}
		if err := jq.AppendJSON(&p.buf, row, jq.DefaultMaxDepth); err != nil {
			return err
		}
		p.rows++
		if int64(p.buf.Len()) > maxBuf {
			full = true
			return stop
		}
		return nil
	})
	if errors.Is(err, stop) {
		err = nil
	}
	if p.rows >= maxRows {
		full = true
	}
	if err != nil {
		return n, full, selectorError("rows", err)
	}
	return n, full, nil
}

func (p *pager) first(ctx context.Context, prog *jq.Program, doc any, field string) (any, bool, error) {
	v, ok, err := prog.First(ctx, doc, jqLimits)
	if err != nil {
		return nil, false, selectorError(field, err)
	}
	return v, ok, nil
}

// selectorError maps jq errors to invalid_query with a bounded message (FR-073 AC5/AC7).
func selectorError(field string, err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, jq.ErrBudget):
		return invalidQuery("%s: %v", field, err)
	}
	var re *jq.RuntimeError
	if errors.As(err, &re) {
		return invalidQuery("%s: %s", field, re.Msg)
	}
	return invalidQuery("%s: %v", field, err)
}

// setBodyPath sets a dot path in the JSON body to the cursor (FR-072 AC3).
func setBodyPath(body any, dotted string, v string) (any, error) {
	root, ok := body.(*jq.Object)
	if body == nil {
		root, ok = jq.NewObject(1), true
	}
	if !ok {
		return nil, invalidQuery("pagination.cursorBodyPath needs a JSON object body")
	}
	var set func(o *jq.Object, segs []string) *jq.Object
	set = func(o *jq.Object, segs []string) *jq.Object {
		out := jq.NewObject(o.Len() + 1)
		for _, k := range o.Keys() {
			x, _ := o.Get(k)
			out.Set(k, x)
		}
		if len(segs) == 1 {
			out.Set(segs[0], v)
			return out
		}
		child, _ := o.Get(segs[0])
		co, isObj := child.(*jq.Object)
		if !isObj {
			co = jq.NewObject(1)
		}
		out.Set(segs[0], set(co, segs[1:]))
		return out
	}
	return set(root, strings.Split(dotted, ".")), nil
}
