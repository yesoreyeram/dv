package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugin/plugintest"
	"github.com/yesoreyeram/dv/internal/query"
)

func newQueryServer(t *testing.T, enabled bool) *Server {
	t.Helper()
	reg := plugin.NewRegistry()
	if err := reg.RegisterSource(plugintest.NewSource("stub")); err != nil {
		t.Fatal(err)
	}
	reg.Freeze()
	svc, err := query.New(context.Background(), query.Options{
		Sources: reg, Instances: []query.Instance{{UID: "s1", Type: "stub", Name: "Stub"}},
		Limits: frame.Limits{MaxRows: 10, MaxBytes: 1 << 20}, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HTTPAddr, cfg.QueryAPIEnabled, cfg.MaxBodyBytes = "127.0.0.1:0", enabled, 1<<10
	s, err := New(Options{Config: cfg, Plugins: reg, Query: svc})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func post(t *testing.T, s *Server, ctype, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func errCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatalf("%v: %s", err, rr.Body)
	}
	return e.Error.Code
}

const okBody = `{"queries":[{"refId":"A","datasource":{"uid":"s1"},"query":{}}]}`

func TestQueryEndpointsDisabledByDefault(t *testing.T) {
	for _, s := range []*Server{newQueryServer(t, false), newTestServer(t, nil)} {
		if rr := post(t, s, "application/json", okBody); rr.Code != 404 || errCode(t, rr) != "not_found" {
			t.Fatalf("query: %d %s", rr.Code, rr.Body)
		}
		if rr := do(t, s.Handler(), http.MethodGet, "/api/v1/datasources"); rr.Code != 404 {
			t.Fatalf("datasources: %d", rr.Code)
		}
	}
}

func TestQueryEnabledWithoutServiceIs404(t *testing.T) {
	cfg := config.Default()
	cfg.HTTPAddr, cfg.QueryAPIEnabled = "127.0.0.1:0", true
	s, err := New(Options{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if rr := post(t, s, "application/json", okBody); rr.Code != 404 {
		t.Fatal(rr.Code)
	}
}

func TestQueryEndpoint(t *testing.T) {
	s := newQueryServer(t, true)
	rr := post(t, s, "application/json; charset=utf-8", okBody)
	if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	var resp query.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.Results["A"].Status != 200 {
		t.Fatalf("%v %s", err, rr.Body)
	}
	rr = do(t, s.Handler(), http.MethodGet, "/api/v1/datasources")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `{"datasources":[{"uid":"s1","type":"stub","name":"Stub"}]}`) {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
}

func TestQueryRequestErrors(t *testing.T) {
	s := newQueryServer(t, true)
	cases := []struct {
		ctype, body string
		status      int
		code        string
	}{
		{"", okBody, 415, "unsupported_media_type"},
		{"text/plain", okBody, 415, "unsupported_media_type"},
		{"application/json;;", okBody, 415, "unsupported_media_type"},
		{"application/json", `not json`, 400, "invalid_request"},
		{"application/json", `{"queries":[],"x":1}`, 400, "invalid_request"},
		{"application/json", okBody + ` {}`, 400, "invalid_request"},
		{"application/json", okBody + ` ]`, 400, "invalid_request"},
		{"application/json", `{"queries":[]}`, 400, "invalid_request"},
		{"application/json", `{"queries":[{"refId":"A","datasource":{"uid":"s1"},"query":{"pad":"` + strings.Repeat("x", 2048) + `"}}]}`, 413, "invalid_request"},
	}
	for _, c := range cases {
		rr := post(t, s, c.ctype, c.body)
		if rr.Code != c.status || errCode(t, rr) != c.code {
			t.Errorf("%q %.40q: %d %s", c.ctype, c.body, rr.Code, rr.Body)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestDecodeQueryRequestReadError(t *testing.T) {
	if _, status, err := decodeQueryRequest(failingReader{}); status != 400 || err == nil || strings.Contains(err.Error(), "boom") {
		t.Fatal(status, err)
	}
}
