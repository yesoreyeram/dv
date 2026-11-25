package httpsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/mockapi"
	"github.com/yesoreyeram/dv/internal/netguard"
	"github.com/yesoreyeram/dv/internal/plugin"
)

type recorder struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (r *recorder) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.reqs = append(r.reqs, req.Clone(req.Context()))
		r.mu.Unlock()
		h.ServeHTTP(w, req)
	})
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

type fixture struct {
	t    testing.TB
	srv  *httptest.Server
	rec  *recorder
	src  *Source
	logs *strings.Builder
}

func newFixture(t testing.TB, extra http.Handler) *fixture {
	t.Helper()
	rec := &recorder{}
	mux := http.NewServeMux()
	mux.Handle("/api/", mockapi.Handler(mockapi.DefaultTokens))
	if extra != nil {
		mux.Handle("/x/", extra)
	}
	srv := httptest.NewServer(rec.wrap(mux))
	t.Cleanup(srv.Close)
	allowed, _ := netguard.ParseAllowed([]string{"127.0.0.1"})
	logs := &strings.Builder{}
	log := slog.New(slog.NewTextHandler(&syncWriter{w: logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	src := New(Options{Policy: netguard.Policy{Allowed: allowed}, Logger: log})
	return &fixture{t: t, srv: srv, rec: rec, src: src, logs: logs}
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (f *fixture) cfg(base string, auth string, secrets map[string]string) plugin.SourceConfig {
	if auth == "" {
		auth = `{"type":"none"}`
	}
	return plugin.SourceConfig{
		UID: "t", Type: ID, Name: "Test",
		JSONData:       json.RawMessage(fmt.Sprintf(`{"baseURL":%q,"allowInsecureHTTP":true,"auth":%s}`, f.srv.URL+base, auth)),
		SecureJSONData: plugin.SecureValues(secrets),
	}
}

func (f *fixture) query(cfg plugin.SourceConfig, body string, lim frame.Limits) (*frame.Frame, error) {
	return f.queryTR(cfg, body, lim, plugin.TimeRange{})
}

func (f *fixture) queryTR(cfg plugin.SourceConfig, body string, lim frame.Limits, tr plugin.TimeRange) (*frame.Frame, error) {
	res, err := f.src.Query(context.Background(), plugin.Query{RefID: "A", Source: cfg, Body: json.RawMessage(body), Limits: lim, TimeRange: tr})
	if err != nil {
		return nil, err
	}
	if len(res.Frames) != 1 {
		f.t.Fatalf("frames = %d", len(res.Frames))
	}
	return res.Frames[0], nil
}

func values(t *testing.T, fr *frame.Frame, name string) string {
	t.Helper()
	fld := fr.Field(name)
	if fld == nil {
		t.Fatalf("no field %q in %v", name, fieldNames(fr))
	}
	var parts []string
	for i := range fld.Len() {
		parts = append(parts, fmt.Sprint(fld.At(i)))
	}
	return strings.Join(parts, ",")
}

func fieldNames(fr *frame.Frame) []string {
	var out []string
	for _, f := range fr.Fields {
		out = append(out, f.Name)
	}
	return out
}

var ghAuth = `{"type":"bearer"}`
var ghSecrets = map[string]string{"token": "s3cr3t-token"}

func TestLinkHeaderPaginationWithBearer(t *testing.T) {
	f := newFixture(t, nil)
	fr, err := f.query(f.cfg("/api", ghAuth, ghSecrets), `{"path":"/github/repos/acme/web/issues","params":[{"name":"per_page","value":"2"}],
		"pagination":{"type":"linkHeader"},"rows":".[] | {number, title, author: .user.login, labels: [.labels[].name]}"}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fieldNames(fr), ","); got != "number,title,author,labels" {
		t.Fatalf("fields = %s", got)
	}
	if got := values(t, fr, "author"); got != "ana,ben,ana,cy,ben" {
		t.Fatalf("authors = %s", got)
	}
	if fr.Meta.Pages != 3 || fr.Meta.Truncated || fr.Meta.ExecutedQuery != "GET /api/github/repos/acme/web/issues" {
		t.Fatalf("meta = %+v", fr.Meta)
	}
	for _, r := range f.rec.reqs {
		if r.Header.Get("Authorization") != "Bearer s3cr3t-token" || r.Header.Get("Accept") != "application/json" {
			t.Fatalf("headers = %v", r.Header)
		}
	}
	if strings.Contains(f.logs.String(), "s3cr3t-token") {
		t.Fatal("token logged")
	}
	if !strings.Contains(f.logs.String(), "upstream request") || !strings.Contains(f.logs.String(), "page=3") {
		t.Fatalf("missing request log lines: %s", f.logs.String())
	}
}

func TestMaxPagesTruncates(t *testing.T) {
	f := newFixture(t, nil)
	fr, err := f.query(f.cfg("/api", ghAuth, ghSecrets), `{"path":"/github/repos/acme/web/issues","params":[{"name":"per_page","value":"2"}],
		"pagination":{"type":"linkHeader","maxPages":2}}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 4 || !fr.Meta.Truncated || len(fr.Meta.Notices) == 0 || !strings.Contains(fr.Meta.Notices[0].Text, "maxPages") {
		t.Fatalf("rows=%d meta=%+v", fr.Rows(), fr.Meta)
	}
}

func TestRowLimitStopsPaging(t *testing.T) {
	f := newFixture(t, nil)
	fr, err := f.query(f.cfg("/api", ghAuth, ghSecrets), `{"path":"/github/repos/acme/web/issues","params":[{"name":"per_page","value":"1"}],
		"pagination":{"type":"linkHeader"}}`, frame.Limits{MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 2 || !fr.Meta.Truncated || f.rec.count() != 3 {
		t.Fatalf("rows=%d truncated=%v requests=%d", fr.Rows(), fr.Meta.Truncated, f.rec.count())
	}
}

func TestSentryLinkResultsFalseStops(t *testing.T) {
	f := newFixture(t, nil)
	fr, err := f.query(f.cfg("/api", `{"type":"bearer"}`, map[string]string{"token": "sentry-token"}),
		`{"path":"/sentry/organizations/acme/issues/","pagination":{"type":"linkHeader"},
		  "columns":[{"name":"id","selector":".id"},{"name":"events","selector":".count | tonumber","type":"number"},{"name":"lastSeen","selector":".lastSeen","type":"time"}]}`,
		frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 3 || fr.Meta.Pages != 2 || fr.Field("lastSeen").Type != frame.TypeTime || values(t, fr, "events") != "1532,418,96" {
		t.Fatalf("rows=%d pages=%d events=%s", fr.Rows(), fr.Meta.Pages, values(t, fr, "events"))
	}
	if !strings.HasSuffix(f.rec.reqs[0].URL.Path, "/issues/") {
		t.Fatalf("trailing slash lost: %s", f.rec.reqs[0].URL.Path)
	}
}

func TestCursorPagination(t *testing.T) {
	f := newFixture(t, nil)
	fr, err := f.query(f.cfg("/api", `{"type":"bearer"}`, map[string]string{"token": "v-token"}),
		`{"path":"/vercel/v6/deployments","pagination":{"type":"cursor","cursorSelector":".pagination.next","cursorParam":"until"},
		  "rows":".deployments[]","columns":[{"name":"uid","selector":".uid"},{"name":"created","selector":".created / 1000 | todate","type":"time"}]}`,
		frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if values(t, fr, "uid") != "dpl_4,dpl_3,dpl_2,dpl_1" || fr.Field("created").Type != frame.TypeTime {
		t.Fatalf("uids = %s", values(t, fr, "uid"))
	}
	if got := f.rec.reqs[1].URL.Query().Get("until"); got != "1764043200000" {
		t.Fatalf("cursor = %q", got)
	}
}

func TestCursorLoopAndCursorOnly(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// Always the same cursor: the pager must stop after the second page.
		_, _ = io.WriteString(w, `{"items":[{"a":1}],"nextPageKey":"K1"}`)
	})
	f := newFixture(t, h)
	fr, err := f.query(f.cfg("", "", nil), `{"path":"/x/metrics","params":[{"name":"metricSelector","value":"cpu"}],
		"pagination":{"type":"cursor","cursorSelector":".nextPageKey","cursorParam":"nextPageKey","cursorOnly":true},"rows":".items[]"}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 2 || len(seen) != 2 || seen[0] != "metricSelector=cpu" || seen[1] != "nextPageKey=K1" {
		t.Fatalf("rows=%d queries=%v", fr.Rows(), seen)
	}
}

func TestCursorInPOSTBody(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = io.WriteString(w, `{"data":[{"m":"a"}],"meta":{"page":{"after":"c2"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"m":"b"}],"meta":{"page":{}}}`)
	})
	f := newFixture(t, h)
	fr, err := f.queryTR(f.cfg("", "", nil), `{"method":"POST","path":"/x/logs","body":{"filter":{"from":"${__from:iso}"},"page":{"limit":1}},
		"pagination":{"type":"cursor","cursorSelector":".meta.page.after","cursorBodyPath":"page.cursor"},"rows":".data[]"}`, frame.Limits{},
		plugin.TimeRange{From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 2 || bodies[0] != `{"filter":{"from":"2026-11-01T00:00:00Z"},"page":{"limit":1}}` ||
		bodies[1] != `{"filter":{"from":"2026-11-01T00:00:00Z"},"page":{"limit":1,"cursor":"c2"}}` {
		t.Fatalf("rows=%d bodies=%v", fr.Rows(), bodies)
	}
	if f.rec.reqs[0].Header.Get("Content-Type") != "application/json" || f.rec.reqs[0].Method != http.MethodPost {
		t.Fatal("POST not sent as JSON")
	}
}

func TestOffsetPaginationWithTwoAPIKeys(t *testing.T) {
	f := newFixture(t, nil)
	auth := `{"type":"apiKey","keys":[{"in":"header","name":"DD-API-KEY","secret":"apiKey"},{"in":"header","name":"DD-APPLICATION-KEY","secret":"appKey"}]}`
	fr, err := f.query(f.cfg("/api", auth, map[string]string{"apiKey": "dd-api-key", "appKey": "app"}),
		`{"path":"/datadog/api/v2/logs","pagination":{"type":"offset","limit":3},"rows":".data[] | .attributes"}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 7 || fr.Meta.Pages != 3 || fr.Field("timestamp").Type != frame.TypeTime {
		t.Fatalf("rows=%d pages=%d", fr.Rows(), fr.Meta.Pages)
	}
	if q := f.rec.reqs[2].URL.Query(); q.Get("offset") != "6" || q.Get("limit") != "3" {
		t.Fatalf("third page query = %v", q)
	}
}

func TestPageAndNextURLPagination(t *testing.T) {
	f := newFixture(t, nil)
	fr, err := f.query(f.cfg("/api", "", nil), `{"path":"/cloudflare/client/v4/zones","pagination":{"type":"page","sizeParam":"per_page","size":2},"rows":".result[]","flatten":true}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 5 || fr.Field("plan.name") == nil {
		t.Fatalf("rows=%d fields=%v", fr.Rows(), fieldNames(fr))
	}
	f2 := newFixture(t, nil)
	fr, err = f2.query(f2.cfg("/api", "", nil), `{"path":"/thousandeyes/v7/tests","pagination":{"type":"nextUrl","nextUrlSelector":"._links.next.href"},"rows":".tests[]"}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if fr.Rows() != 5 || fr.Meta.Pages != 3 {
		t.Fatalf("rows=%d pages=%d", fr.Rows(), fr.Meta.Pages)
	}
}

func TestNextPageToAnotherOriginIsRefused(t *testing.T) {
	f := newFixture(t, nil)
	_, err := f.query(f.cfg("/api", ghAuth, ghSecrets), `{"path":"/evil/redirect-pagination","pagination":{"type":"linkHeader"}}`, frame.Limits{})
	if !errors.Is(err, plugin.ErrUpstream) || !strings.Contains(err.Error(), "leaves the connection's base URL") {
		t.Fatalf("err = %v", err)
	}
	if f.rec.count() != 1 {
		t.Fatalf("requests = %d", f.rec.count())
	}
	// A next URL under another base path of the same host is refused too.
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[],"next":"/admin/secrets"}`)
	})
	f = newFixture(t, h)
	_, err = f.query(f.cfg("/x", "", nil), `{"path":"/a","pagination":{"type":"nextUrl","nextUrlSelector":".next"}}`, frame.Limits{})
	if !errors.Is(err, plugin.ErrUpstream) {
		t.Fatalf("err = %v", err)
	}
}

func TestRedirectToOtherOriginDropsCredentials(t *testing.T) {
	var got http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer other.Close()
	// Same IP, different port: a different origin.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/landing", http.StatusFound)
	})
	f := newFixture(t, h)
	auth := `{"type":"apiKey","keys":[{"in":"header","name":"X-Api-Key","secret":"k"}]}`
	if _, err := f.query(f.cfg("", auth, map[string]string{"k": "topsecret"}), `{"path":"/x/r"}`, frame.Limits{}); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Get("X-Api-Key") != "" || got.Get("Authorization") != "" || got.Get("Accept") == "" {
		t.Fatalf("headers at other origin = %v", got)
	}
}

func TestSSRFGuardBlocksLoopbackWithoutExemption(t *testing.T) {
	f := newFixture(t, nil)
	f.src = New(Options{}) // no exemptions
	_, err := f.query(f.cfg("/api", "", nil), `{"path":"/echo"}`, frame.Limits{})
	if !errors.Is(err, plugin.ErrDestinationNotAllowed) || !strings.Contains(err.Error(), "loopback") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("err = %v", err)
	}
	if f.rec.count() != 0 {
		t.Fatal("upstream reached")
	}
}

func TestUpstreamStatusMapping(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/429":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		case "/x/500":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "internal stack trace secret")
		case "/x/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>")
		case "/x/bad":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"a":`)
		case "/x/big":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `["`+strings.Repeat("x", 4096)+`"]`)
		case "/x/deep":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("[", 100)+strings.Repeat("]", 100))
		default:
			http.NotFound(w, r)
		}
	})
	f := newFixture(t, h)
	for path, want := range map[string]error{
		"/x/429": plugin.ErrUpstreamRateLimited, "/x/500": plugin.ErrUpstream, "/x/html": plugin.ErrUnsupportedMediaType,
		"/x/bad": plugin.ErrUpstream, "/x/deep": plugin.ErrUpstream, "/x/missing": plugin.ErrUpstreamNotFound,
	} {
		_, err := f.query(f.cfg("", "", nil), `{"path":"`+path+`"}`, frame.Limits{})
		if !errors.Is(err, want) || strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: err = %v, want %v", path, err, want)
		}
		if path == "/x/429" && !strings.Contains(err.Error(), "retry after 7") {
			t.Errorf("429 message = %v", err)
		}
	}
	cfg := f.cfg("", "", nil)
	cfg.JSONData = json.RawMessage(strings.Replace(string(cfg.JSONData), `"allowInsecureHTTP":true`, `"allowInsecureHTTP":true,"maxResponseBytes":1024`, 1))
	if _, err := f.query(cfg, `{"path":"/x/big"}`, frame.Limits{}); !errors.Is(err, plugin.ErrResponseTooLarge) {
		t.Errorf("big: %v", err)
	}
	_, err := f.query(f.cfg("/api", ghAuth, map[string]string{"token": "wrong"}), `{"path":"/github/repos/acme/web/issues"}`, frame.Limits{})
	if !errors.Is(err, plugin.ErrUpstreamAuth) || strings.Contains(err.Error(), "Bad credentials") {
		t.Errorf("auth: %v", err)
	}
}

func TestBasicAuthAndQueryAPIKey(t *testing.T) {
	var got *http.Request
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"ok":true}]`)
	})
	f := newFixture(t, h)
	fr, err := f.query(f.cfg("", `{"type":"basic","username":"admin"}`, map[string]string{"password": "pw"}), `{"path":"/x/a"}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if u, p, ok := got.BasicAuth(); !ok || u != "admin" || p != "pw" || fr.Rows() != 1 {
		t.Fatalf("basic auth = %q %q %v", u, p, ok)
	}
	auth := `{"type":"apiKey","keys":[{"in":"query","name":"api_key","secret":"k"},{"in":"header","name":"Authorization","prefix":"Api-Token ","secret":"t"}]}`
	fr, err = f.query(f.cfg("", auth, map[string]string{"k": "qsecret", "t": "dt0c01"}), `{"path":"/x/a","params":[{"name":"q","value":"1"}]}`, frame.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL.Query().Get("api_key") != "qsecret" || got.URL.Query().Get("q") != "1" || got.Header.Get("Authorization") != "Api-Token dt0c01" {
		t.Fatalf("request = %s %v", got.URL.RawQuery, got.Header)
	}
	if strings.Contains(fr.Meta.ExecutedQuery, "qsecret") || strings.Contains(f.logs.String(), "qsecret") || strings.Contains(f.logs.String(), "dt0c01") {
		t.Fatalf("secret leaked: %s / %s", fr.Meta.ExecutedQuery, f.logs.String())
	}
}

func TestMacrosAndColumnsErrorsBecomeNull(t *testing.T) {
	f := newFixture(t, nil)
	tr := plugin.TimeRange{From: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}
	fr, err := f.queryTR(f.cfg("/api", "", nil), `{"path":"/echo","params":[{"name":"a","value":"${__from}"},{"name":"b","value":"${__to:s}"},{"name":"c","value":"${__from:iso}"},{"name":"d","value":"${other}"}],
		"columns":[{"name":"a","selector":".a"},{"name":"b","selector":".b | tonumber","type":"number"},{"name":"c","selector":".c"},{"name":"d","selector":".d"},{"name":"bad","selector":".a.b"}]}`,
		frame.Limits{}, tr)
	if err != nil {
		t.Fatal(err)
	}
	if values(t, fr, "a") != "1793491200000" || values(t, fr, "b") != "1793577600" || values(t, fr, "c") != "2026-11-01 00:00:00 +0000 UTC" ||
		values(t, fr, "d") != "${other}" || values(t, fr, "bad") != "<nil>" {
		t.Fatalf("a=%s b=%s c=%s d=%s bad=%s", values(t, fr, "a"), values(t, fr, "b"), values(t, fr, "c"), values(t, fr, "d"), values(t, fr, "bad"))
	}
	if _, err := f.query(f.cfg("/api", "", nil), `{"path":"/echo","params":[{"name":"a","value":"${__from}"}]}`, frame.Limits{}); !errors.Is(err, plugin.ErrInvalidQuery) {
		t.Fatalf("macro without time range: %v", err)
	}
}

func TestQueryValidation(t *testing.T) {
	f := newFixture(t, nil)
	cfg := f.cfg("/api", `{"type":"apiKey","keys":[{"in":"header","name":"X-Key","secret":"k"},{"in":"query","name":"token","secret":"k"}]}`, map[string]string{"k": "v"})
	cases := map[string]string{
		`{"path":"http://169.254.169.254/latest"}`: "relative path",
		`{"path":"//evil.example/x"}`:              "relative path",
		`{"path":"github"}`:                        "relative path",
		`{"path":"/github/../../admin"}`:           "outside the base path",
		`{"path":"/%2e%2e/admin"}`:                 "outside the base path",
		`{"path":"/a?x=1"}`:                        "use params",
		`{"path":"/a\\b"}`:                         "backslash",
		`{"path":"/a%00"}`:                         "control character",
		`{"path":"/a","headers":[{"name":"Authorization","value":"x"}]}`:                          "may not be set",
		`{"path":"/a","headers":[{"name":"host","value":"x"}]}`:                                   "may not be set",
		`{"path":"/a","headers":[{"name":"X-Forwarded-For","value":"x"}]}`:                        "may not be set",
		`{"path":"/a","headers":[{"name":"x-key","value":"x"}]}`:                                  "may not be set",
		`{"path":"/a","headers":[{"name":"Bad Name","value":"x"}]}`:                               "not a valid header name",
		`{"path":"/a","headers":[{"name":"X-A","value":"a\r\nHost: b"}]}`:                         "control character",
		`{"path":"/a","params":[{"name":"token","value":"x"}]}`:                                   "may not be set",
		`{"path":"/a","pagination":{"type":"offset","offsetParam":"token"}}`:                      "set by the connection's auth",
		`{"path":"/a","rows":"env.HOME"}`:                                                         "not supported",
		`{"path":"/a","method":"DELETE"}`:                                                         "GET or POST",
		`{"path":"/a","body":{"a":1}}`:                                                            "only allowed with method POST",
		`{"path":"/a","pagination":{"type":"magic"}}`:                                             "not supported",
		`{"path":"/a","pagination":{"maxPages":101}}`:                                             "maxPages",
		`{"path":"/a","pagination":{"type":"cursor"}}`:                                            "cursorSelector is required",
		`{"path":"/a","pagination":{"type":"cursor","cursorSelector":".n"}}`:                      "cursorParam is required",
		`{"path":"/a","pagination":{"type":"cursor","cursorSelector":".n","cursorBodyPath":"a"}}`: "needs method POST",
		`{"path":"/a","pagination":{"type":"nextUrl"}}`:                                           "nextUrlSelector is required",
		`{"path":"/a","pagination":{"type":"offset","limit":20000}}`:                              "pagination.limit",
		`{"path":"/a","pagination":{"type":"page","sizeParam":"n"}}`:                              "pagination.size",
		`{"path":"/a","columns":[{"name":"a","selector":"."},{"name":"a","selector":"."}]}`:       "listed twice",
		`{"path":"/a","columns":[{"name":"a","selector":".","type":"blob"}]}`:                     "unsupported type",
		`{"path":"/a","columns":[{"name":"a","selector":"def x: 1; x"}]}`:                         "not supported",
		`{"path":"/a","fieldTypes":{"a":"blob"}}`:                                                 "unsupported type",
		`{"path":"/a","unknown":1}`:                                                               "unknown field",
		``:                                                                                        "required",
	}
	for body, want := range cases {
		_, err := f.query(cfg, body, frame.Limits{})
		if !errors.Is(err, plugin.ErrInvalidQuery) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want invalid query mentioning %q", body, err, want)
		}
	}
	if f.rec.count() != 0 {
		t.Fatalf("invalid queries reached the upstream %d times", f.rec.count())
	}
}

func TestSelectorBudgetIsInvalidQuery(t *testing.T) {
	f := newFixture(t, nil)
	_, err := f.query(f.cfg("/api", "", nil), `{"path":"/echo","rows":"[range(100000000)]"}`, frame.Limits{})
	if !errors.Is(err, plugin.ErrInvalidQuery) || !strings.Contains(err.Error(), "evaluation budget") {
		t.Fatalf("err = %v", err)
	}
	_, err = f.query(f.cfg("/api", "", nil), `{"path":"/echo","rows":".a.b.c"}`, frame.Limits{})
	if err != nil {
		t.Fatalf("null navigation: %v", err)
	}
	_, err = f.query(f.cfg("/api", "", nil), `{"path":"/github/repos/acme/web/issues","rows":".[] | .x"}`, frame.Limits{})
	if !errors.Is(err, plugin.ErrUpstreamAuth) {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidation(t *testing.T) {
	s := New(Options{})
	cases := map[string]string{
		``:                                     "baseURL is required",
		`{}`:                                   "baseURL is required",
		`{"baseURL":"api.example.com"}`:        "absolute URL",
		`{"baseURL":"http://api.example.com"}`: "must use https",
		`{"baseURL":"ftp://api.example.com","allowInsecureHTTP":true}`:                                                       "must use https",
		`{"baseURL":"https://u:p@api.example.com"}`:                                                                          "user info",
		`{"baseURL":"https://api.example.com/?a=1"}`:                                                                         "user info, a query",
		`{"baseURL":"https://api.example.com","timeout":"1ms"}`:                                                              "timeout",
		`{"baseURL":"https://api.example.com","maxResponseBytes":10}`:                                                        "maxResponseBytes",
		`{"baseURL":"https://api.example.com","headers":[{"name":"Cookie","value":"a"}]}`:                                    "may not be set",
		`{"baseURL":"https://api.example.com","auth":{"type":"bearer"}}`:                                                     "needs secureJsonData.token",
		`{"baseURL":"https://api.example.com","auth":{"type":"basic"}}`:                                                      "username",
		`{"baseURL":"https://api.example.com","auth":{"type":"apiKey"}}`:                                                     "1 to 4 keys",
		`{"baseURL":"https://api.example.com","auth":{"type":"apiKey","keys":[{"in":"cookie","name":"a","secret":"s"}]}}`:    "header or query",
		`{"baseURL":"https://api.example.com","auth":{"type":"apiKey","keys":[{"in":"header","name":"Host","secret":"s"}]}}`: "cannot carry a key",
		`{"baseURL":"https://api.example.com","auth":{"type":"awsSigV4"}}`:                                                   "planned",
		`{"baseURL":"https://api.example.com","auth":{"type":"kerberos"}}`:                                                   "not supported",
		`{"baseURL":"https://api.example.com","auth":{"type":"none","username":"x"}}`:                                        "takes no",
		`{"baseURL":"https://api.example.com","extra":1}`:                                                                    "unknown field",
	}
	for js, want := range cases {
		err := s.Validate(context.Background(), plugin.SourceConfig{UID: "x", JSONData: json.RawMessage(js)})
		if !errors.Is(err, plugin.ErrInvalidConfig) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", js, err, want)
		}
	}
	ok := plugin.SourceConfig{UID: "x", JSONData: json.RawMessage(`{"baseURL":"https://api.example.com/v1/","auth":{"type":"bearer"}}`), SecureJSONData: plugin.SecureValues{"token": "t"}}
	if err := s.Validate(context.Background(), ok); err != nil {
		t.Fatal(err)
	}
	if err := s.Health(context.Background(), ok); err != nil {
		t.Fatal(err)
	}
	if m := s.Meta(); m.ID != "http" || len(m.Capabilities) == 0 {
		t.Fatalf("meta = %+v", m)
	}
}

func TestNextLinkParsing(t *testing.T) {
	cases := map[string]string{
		`<https://a/x?page=2>; rel="next", <https://a/x?page=9>; rel="last"`: "https://a/x?page=2",
		`<https://a/x?page=9>; rel="last", <https://a/x?page=2>; rel=next`:   "https://a/x?page=2",
		`<https://a/p>; rel="prev next"`:                                     "https://a/p",
		`<https://a/p>; REL="Next"`:                                          "https://a/p",
		`<https://a/x>; rel="next"; results="false"; cursor="1"`:             "",
		`<https://a/x>; rel="next"; results="true"`:                          "https://a/x",
		`<https://a/x,y>; rel="next"`:                                        "https://a/x,y",
		`<https://a/x>; rel="last"`:                                          "",
		`garbage`:                                                            "",
		`<unterminated; rel="next"`:                                          "",
	}
	for in, want := range cases {
		if got := nextLink([]string{in}); got != want {
			t.Errorf("nextLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePathKeepsBase(t *testing.T) {
	base, _ := url.Parse("https://api.example.com/v1")
	for in, want := range map[string]string{"/a/b": "/v1/a/b", "/a/./b/../c/": "/v1/a/c/", "/": "/v1/", "/%41": "/v1/A"} {
		got, err := resolvePath(base, in)
		if err != nil || got != want {
			t.Errorf("resolvePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// BenchmarkPaginatedQuery measures NFR-PERF-006 AC3: 10 pages × 1 000 rows against a
// local upstream, excluding upstream latency.
func BenchmarkPaginatedQuery(b *testing.B) {
	page := `[` + strings.TrimSuffix(strings.Repeat(`{"id":1,"name":"host-a","cpu":0.5,"up":true},`, 1000), ",") + `]`
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, page)
	})
	f := newFixture(b, h)
	cfg := f.cfg("", "", nil)
	body := json.RawMessage(`{"path":"/x/p","pagination":{"type":"page","size":1000,"maxPages":10}}`)
	b.ResetTimer()
	for b.Loop() {
		res, err := f.src.Query(context.Background(), plugin.Query{RefID: "A", Source: cfg, Body: body})
		if err != nil || res.Frames[0].Rows() != 10_000 {
			b.Fatalf("%v", err)
		}
	}
}
