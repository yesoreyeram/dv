package httpsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

// Limits on settings and query models (FR-040 AC1/AC2, FR-071 AC4, FR-072, FR-073 AC6).
const (
	maxHeaders          = 32
	maxParams           = 64
	maxNameBytes        = 128
	maxValueBytes       = 4096
	maxBodyBytes        = 64 << 10
	maxAPIKeys          = 4
	maxColumns          = 1024
	defaultMaxPages     = 10
	hardMaxPages        = 100
	defaultTimeout      = 30 * time.Second
	minTimeout          = time.Second
	maxTimeout          = 5 * time.Minute
	defaultMaxResponse  = 16 << 20
	minMaxResponse      = 1 << 10
	maxMaxResponse      = 256 << 20
	defaultOffsetLimit  = 100
	maxOffsetLimit      = 10_000
	defaultRowsSelector = `if type == "array" then .[] else . end`
)

// NameValue is one header or parameter.
type NameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// APIKey is one key of the apiKey auth method (FR-071 AC4).
type APIKey struct {
	In     string `json:"in"` // header | query
	Name   string `json:"name"`
	Prefix string `json:"prefix,omitempty"`
	Secret string `json:"secret"` // name of the secureJsonData entry
}

// Auth selects the authentication method (FR-071).
type Auth struct {
	Type     string   `json:"type"`
	Username string   `json:"username,omitempty"`
	Keys     []APIKey `json:"keys,omitempty"`
}

// Settings is the instance configuration in SourceConfig.JSONData (FR-040 AC1).
type Settings struct {
	BaseURL           string      `json:"baseURL"`
	AllowInsecureHTTP bool        `json:"allowInsecureHTTP,omitempty"`
	Auth              Auth        `json:"auth"`
	Headers           []NameValue `json:"headers,omitempty"`
	Timeout           string      `json:"timeout,omitempty"`
	MaxResponseBytes  int64       `json:"maxResponseBytes,omitempty"`

	base    *url.URL
	timeout time.Duration
}

// Column is one FR-073 AC6 column.
type Column struct {
	Name     string          `json:"name"`
	Selector string          `json:"selector"`
	Type     frame.FieldType `json:"type,omitempty"`
}

// Pagination configures FR-072.
type Pagination struct {
	Type     string `json:"type,omitempty"`
	MaxPages int    `json:"maxPages,omitempty"`
	// offset
	OffsetParam string `json:"offsetParam,omitempty"`
	LimitParam  string `json:"limitParam,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	// page
	PageParam string `json:"pageParam,omitempty"`
	SizeParam string `json:"sizeParam,omitempty"`
	Size      int    `json:"size,omitempty"`
	// offset and page
	Start *int `json:"start,omitempty"`
	// cursor
	CursorSelector string `json:"cursorSelector,omitempty"`
	CursorParam    string `json:"cursorParam,omitempty"`
	CursorOnly     bool   `json:"cursorOnly,omitempty"`
	CursorBodyPath string `json:"cursorBodyPath,omitempty"`
	// nextUrl
	NextURLSelector string `json:"nextUrlSelector,omitempty"`
}

// QueryModel is the plugin-specific query in Query.Body (FR-040 AC2).
type QueryModel struct {
	Method     string                     `json:"method,omitempty"`
	Path       string                     `json:"path"`
	Params     []NameValue                `json:"params,omitempty"`
	Headers    []NameValue                `json:"headers,omitempty"`
	Body       json.RawMessage            `json:"body,omitempty"`
	Pagination Pagination                 `json:"pagination,omitempty"`
	Rows       string                     `json:"rows,omitempty"`
	Columns    []Column                   `json:"columns,omitempty"`
	Flatten    bool                       `json:"flatten,omitempty"`
	FieldTypes map[string]frame.FieldType `json:"fieldTypes,omitempty"`
}

var tokenRE = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")

// forbiddenHeaders may never be set by a query (FR-040 AC4); also refused in settings
// except where noted.
var forbiddenHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true, "host": true,
	"content-length": true, "transfer-encoding": true, "connection": true, "upgrade": true,
	"te": true, "forwarded": true, "keep-alive": true, "proxy-connection": true, "trailer": true,
	"expect": true,
}

func headerForbidden(name string) bool {
	l := strings.ToLower(name)
	return forbiddenHeaders[l] || strings.HasPrefix(l, "x-forwarded-") || strings.HasPrefix(l, "proxy-")
}

func invalidQuery(format string, a ...any) error {
	return fmt.Errorf("%w: %s", plugin.ErrInvalidQuery, fmt.Sprintf(format, a...))
}

func invalidConfig(format string, a ...any) error {
	return fmt.Errorf("%w: %s", plugin.ErrInvalidConfig, fmt.Sprintf(format, a...))
}

func strictDecode(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after JSON object")
	}
	return nil
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func checkValue(kind, name, v string) error {
	if len(v) > maxValueBytes {
		return fmt.Errorf("%s %q value is longer than %d bytes", kind, name, maxValueBytes)
	}
	if strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("%s %q value contains a control character", kind, name)
	}
	return nil
}

// parseSettings decodes and validates jsonData and checks that the secrets the auth
// method needs are provisioned (FR-079 AC4).
func parseSettings(cfg plugin.SourceConfig) (*Settings, error) {
	var st Settings
	if len(bytes.TrimSpace(cfg.JSONData)) == 0 {
		return nil, invalidConfig("jsonData.baseURL is required")
	}
	if err := strictDecode(cfg.JSONData, &st); err != nil {
		return nil, invalidConfig("jsonData: %v", err)
	}
	u, err := url.Parse(st.BaseURL)
	switch {
	case st.BaseURL == "":
		return nil, invalidConfig("jsonData.baseURL is required")
	case err != nil || !u.IsAbs() || u.Host == "":
		return nil, invalidConfig("jsonData.baseURL must be an absolute URL")
	case u.Scheme != "https" && (u.Scheme != "http" || !st.AllowInsecureHTTP):
		return nil, invalidConfig("jsonData.baseURL must use https (set allowInsecureHTTP to allow http)")
	case u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "":
		return nil, invalidConfig("jsonData.baseURL must not contain user info, a query or a fragment")
	case hasControl(st.BaseURL) || strings.Contains(st.BaseURL, `\`):
		return nil, invalidConfig("jsonData.baseURL contains a control character or backslash")
	}
	bp := path.Clean("/" + u.Path)
	u.Path, u.RawPath = bp, ""
	st.base = u

	st.timeout = defaultTimeout
	if st.Timeout != "" {
		d, err := time.ParseDuration(st.Timeout)
		if err != nil || d < minTimeout || d > maxTimeout {
			return nil, invalidConfig("jsonData.timeout must be a duration between 1s and 5m")
		}
		st.timeout = d
	}
	if st.MaxResponseBytes == 0 {
		st.MaxResponseBytes = defaultMaxResponse
	}
	if st.MaxResponseBytes < minMaxResponse || st.MaxResponseBytes > maxMaxResponse {
		return nil, invalidConfig("jsonData.maxResponseBytes must be between 1 KiB and 256 MiB")
	}
	if len(st.Headers) > maxHeaders {
		return nil, invalidConfig("jsonData.headers has more than %d entries", maxHeaders)
	}
	for _, h := range st.Headers {
		if !tokenRE.MatchString(h.Name) {
			return nil, invalidConfig("jsonData.headers: %q is not a valid header name", h.Name)
		}
		if headerForbidden(h.Name) {
			return nil, invalidConfig("jsonData.headers: %q may not be set; use auth for credentials", h.Name)
		}
		if err := checkValue("header", h.Name, h.Value); err != nil {
			return nil, invalidConfig("jsonData.headers: %v", err)
		}
	}
	if err := st.checkAuth(cfg.SecureJSONData); err != nil {
		return nil, err
	}
	return &st, nil
}

func (st *Settings) checkAuth(sec plugin.SecureValues) error {
	need := func(key string) error {
		if v, ok := sec.Get(key); !ok || v == "" {
			return invalidConfig("auth %s needs secureJsonData.%s", st.Auth.Type, key)
		}
		return nil
	}
	a := st.Auth
	switch a.Type {
	case "", "none":
		st.Auth.Type = "none"
		if a.Username != "" || len(a.Keys) > 0 {
			return invalidConfig("auth none takes no username or keys")
		}
	case "basic":
		if len(a.Keys) > 0 {
			return invalidConfig("auth basic takes no keys")
		}
		if a.Username == "" {
			if err := need("username"); err != nil {
				return invalidConfig("auth basic needs jsonData.auth.username or secureJsonData.username")
			}
		}
		if strings.Contains(a.Username, ":") || hasControl(a.Username) {
			return invalidConfig("auth basic username must not contain ':' or control characters")
		}
		return need("password")
	case "bearer":
		if a.Username != "" || len(a.Keys) > 0 {
			return invalidConfig("auth bearer takes no username or keys")
		}
		if err := need("token"); err != nil {
			return err
		}
		if t, _ := sec.Get("token"); hasControl(t) {
			return invalidConfig("secureJsonData.token contains a control character")
		}
	case "apiKey":
		if a.Username != "" {
			return invalidConfig("auth apiKey takes no username")
		}
		if len(a.Keys) == 0 || len(a.Keys) > maxAPIKeys {
			return invalidConfig("auth apiKey needs 1 to %d keys", maxAPIKeys)
		}
		seen := map[string]bool{}
		for _, k := range a.Keys {
			if k.In != "header" && k.In != "query" {
				return invalidConfig("auth apiKey key %q: in must be header or query", k.Name)
			}
			id := k.In + ":" + strings.ToLower(k.Name)
			if seen[id] {
				return invalidConfig("auth apiKey key %q is listed twice", k.Name)
			}
			seen[id] = true
			if k.In == "header" && (!tokenRE.MatchString(k.Name) || headerForbidden(k.Name) && !strings.EqualFold(k.Name, "Authorization")) {
				return invalidConfig("auth apiKey: %q cannot carry a key", k.Name)
			}
			if k.In == "query" && (k.Name == "" || len(k.Name) > maxNameBytes || hasControl(k.Name)) {
				return invalidConfig("auth apiKey: invalid query parameter name")
			}
			if hasControl(k.Prefix) || len(k.Prefix) > 64 {
				return invalidConfig("auth apiKey key %q: invalid prefix", k.Name)
			}
			if k.Secret == "" {
				return invalidConfig("auth apiKey key %q needs a secret name", k.Name)
			}
			if err := need(k.Secret); err != nil {
				return err
			}
			if v, _ := sec.Get(k.Secret); hasControl(v) {
				return invalidConfig("secureJsonData.%s contains a control character", k.Secret)
			}
		}
	case "oauth2ClientCredentials", "jwtBearer", "awsSigV4", "azureAd", "workloadIdentityFederation", "mtls":
		return invalidConfig("auth %s is planned (FR-071) and not available in this build", a.Type)
	default:
		return invalidConfig("auth type %q is not supported", a.Type)
	}
	return nil
}

// authOwned returns the header names (lower case) and query parameters the auth method
// sets, which a query may not set (FR-040 AC4).
func (st *Settings) authOwned() (headers, params map[string]bool) {
	headers, params = map[string]bool{"authorization": true}, map[string]bool{}
	for _, k := range st.Auth.Keys {
		if k.In == "header" {
			headers[strings.ToLower(k.Name)] = true
		} else {
			params[k.Name] = true
		}
	}
	return headers, params
}

// parseQuery decodes and validates the query body against the instance settings.
func parseQuery(raw json.RawMessage, st *Settings) (*QueryModel, error) {
	var m QueryModel
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, invalidQuery("query body is required")
	}
	if err := strictDecode(raw, &m); err != nil {
		return nil, invalidQuery("%v", err)
	}
	switch m.Method {
	case "":
		m.Method = http.MethodGet
	case http.MethodGet, http.MethodPost:
	default:
		return nil, invalidQuery("method must be GET or POST")
	}
	if len(m.Body) > 0 {
		if m.Method != http.MethodPost {
			return nil, invalidQuery("body is only allowed with method POST")
		}
		if len(m.Body) > maxBodyBytes {
			return nil, invalidQuery("body is larger than %d bytes", maxBodyBytes)
		}
	}
	ownedH, ownedP := st.authOwned()
	if len(m.Headers) > maxHeaders {
		return nil, invalidQuery("headers has more than %d entries", maxHeaders)
	}
	for _, h := range m.Headers {
		if !tokenRE.MatchString(h.Name) {
			return nil, invalidQuery("%q is not a valid header name", h.Name)
		}
		if headerForbidden(h.Name) || ownedH[strings.ToLower(h.Name)] {
			return nil, invalidQuery("header %q may not be set by a query", h.Name)
		}
		if err := checkValue("header", h.Name, h.Value); err != nil {
			return nil, invalidQuery("%v", err)
		}
	}
	if len(m.Params) > maxParams {
		return nil, invalidQuery("params has more than %d entries", maxParams)
	}
	for _, p := range m.Params {
		if p.Name == "" || len(p.Name) > maxNameBytes || hasControl(p.Name) {
			return nil, invalidQuery("invalid parameter name")
		}
		if ownedP[p.Name] {
			return nil, invalidQuery("parameter %q may not be set by a query", p.Name)
		}
		if err := checkValue("parameter", p.Name, p.Value); err != nil {
			return nil, invalidQuery("%v", err)
		}
	}
	if err := m.Pagination.validate(m.Method, ownedP); err != nil {
		return nil, err
	}
	if len(m.Columns) > maxColumns {
		return nil, invalidQuery("columns has more than %d entries", maxColumns)
	}
	names := map[string]bool{}
	for i, c := range m.Columns {
		if c.Name == "" || len(c.Name) > 256 {
			return nil, invalidQuery("columns[%d].name must be 1-256 bytes", i)
		}
		if names[c.Name] {
			return nil, invalidQuery("column %q is listed twice", c.Name)
		}
		names[c.Name] = true
		if c.Type != "" && !c.Type.Valid() {
			return nil, invalidQuery("column %q has unsupported type %q", c.Name, c.Type)
		}
	}
	for name, t := range m.FieldTypes {
		if !t.Valid() {
			return nil, invalidQuery("fieldTypes: %q has unsupported type %q", name, t)
		}
	}
	return &m, nil
}

func (p *Pagination) validate(method string, ownedParams map[string]bool) error {
	if p.MaxPages == 0 {
		p.MaxPages = defaultMaxPages
	}
	if p.MaxPages < 1 || p.MaxPages > hardMaxPages {
		return invalidQuery("pagination.maxPages must be between 1 and %d", hardMaxPages)
	}
	param := func(field, v, def string) (string, error) {
		if v == "" {
			v = def
		}
		if v == "" || len(v) > maxNameBytes || hasControl(v) {
			return "", invalidQuery("pagination.%s is required", field)
		}
		if ownedParams[v] {
			return "", invalidQuery("pagination.%s %q is set by the connection's auth", field, v)
		}
		return v, nil
	}
	var err error
	switch p.Type {
	case "", "none":
		p.Type = "none"
	case "offset":
		if p.OffsetParam, err = param("offsetParam", p.OffsetParam, "offset"); err != nil {
			return err
		}
		if p.LimitParam, err = param("limitParam", p.LimitParam, "limit"); err != nil {
			return err
		}
		if p.Limit == 0 {
			p.Limit = defaultOffsetLimit
		}
		if p.Limit < 1 || p.Limit > maxOffsetLimit {
			return invalidQuery("pagination.limit must be between 1 and %d", maxOffsetLimit)
		}
		if p.Start == nil {
			zero := 0
			p.Start = &zero
		}
	case "page":
		if p.PageParam, err = param("pageParam", p.PageParam, "page"); err != nil {
			return err
		}
		if p.SizeParam != "" {
			if p.SizeParam, err = param("sizeParam", p.SizeParam, ""); err != nil {
				return err
			}
		}
		if p.Size < 0 || p.Size > maxOffsetLimit || (p.SizeParam != "" && p.Size == 0) {
			return invalidQuery("pagination.size must be between 1 and %d when sizeParam is set", maxOffsetLimit)
		}
		if p.Start == nil {
			one := 1
			p.Start = &one
		}
	case "cursor":
		if p.CursorSelector == "" {
			return invalidQuery("pagination.cursorSelector is required")
		}
		if p.CursorBodyPath != "" {
			if method != http.MethodPost {
				return invalidQuery("pagination.cursorBodyPath needs method POST")
			}
			for _, seg := range strings.Split(p.CursorBodyPath, ".") {
				if seg == "" || len(seg) > maxNameBytes {
					return invalidQuery("pagination.cursorBodyPath must be dot-separated keys")
				}
			}
		} else if p.CursorParam, err = param("cursorParam", p.CursorParam, ""); err != nil {
			return err
		}
	case "nextUrl":
		if p.NextURLSelector == "" {
			return invalidQuery("pagination.nextUrlSelector is required")
		}
	case "linkHeader":
	default:
		return invalidQuery("pagination.type %q is not supported (none, offset, page, cursor, nextUrl, linkHeader)", p.Type)
	}
	return nil
}

// resolvePath joins the query path to the base path (FR-040 AC2).
func resolvePath(base *url.URL, p string) (string, error) {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return "", invalidQuery("path must be a relative path starting with '/' under the connection's base URL")
	}
	if strings.ContainsAny(p, `\?#`) || hasControl(p) {
		return "", invalidQuery("path must not contain '?', '#', a backslash or a control character; use params")
	}
	dec, err := url.PathUnescape(p)
	if err != nil || hasControl(dec) || strings.Contains(dec, `\`) {
		return "", invalidQuery("path has an invalid escape or control character")
	}
	full := path.Clean(base.Path + "/" + dec)
	if !within(base.Path, full) {
		return "", invalidQuery("path resolves outside the base path of the connection")
	}
	if strings.HasSuffix(dec, "/") && !strings.HasSuffix(full, "/") {
		full += "/"
	}
	return full, nil
}

// within reports whether p is basePath or below it.
func within(basePath, p string) bool {
	basePath = strings.TrimSuffix(basePath, "/")
	return basePath == "" || p == basePath || strings.HasPrefix(p, basePath+"/")
}

// macro expands ${__from}, ${__to} with :s and :iso suffixes (FR-040 AC3).
var macroRE = regexp.MustCompile(`\$\{__(from|to)(?::(s|iso))?\}`)

func expandMacros(s string, tr plugin.TimeRange) (string, error) {
	if !strings.Contains(s, "${__") {
		return s, nil
	}
	var err error
	out := macroRE.ReplaceAllStringFunc(s, func(m string) string {
		if tr.From.IsZero() && tr.To.IsZero() {
			err = invalidQuery("time macros need a time range (from/to) in the request")
			return m
		}
		sub := macroRE.FindStringSubmatch(m)
		t := tr.From
		if sub[1] == "to" {
			t = tr.To
		}
		switch sub[2] {
		case "s":
			return strconv.FormatInt(t.Unix(), 10)
		case "iso":
			return t.UTC().Format(time.RFC3339)
		}
		return strconv.FormatInt(t.UnixMilli(), 10)
	})
	return out, err
}
