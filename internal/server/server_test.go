package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/api"
	"github.com/yesoreyeram/dv/internal/buildinfo"
	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/health"
)

func newTestServer(t *testing.T, mutate func(*config.Config)) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.HTTPAddr = "127.0.0.1:0"
	if mutate != nil {
		mutate(&cfg)
	}
	s, err := New(Options{Config: cfg, Build: buildinfo.Info{Version: "1.0.0", Commit: "c0ffee"}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, target, nil))
	return rr
}

func TestEndpoints(t *testing.T) {
	s := newTestServer(t, nil)
	h := s.Handler()

	rr := do(t, h, http.MethodGet, "/healthz")
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("healthz: %d %s", rr.Code, rr.Body.String())
	}

	rr = do(t, h, http.MethodGet, "/api/v1/version")
	var info buildinfo.Info
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil || info.Version != "1.0.0" || info.Commit != "c0ffee" {
		t.Fatalf("version: %v %+v", err, info)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("version must not be cached")
	}

	rr = do(t, h, http.MethodGet, "/api/openapi.json")
	if rr.Code != 200 || rr.Body.String() != string(api.OpenAPI) {
		t.Fatalf("openapi: %d", rr.Code)
	}

	for _, p := range []string{"/api/v1/nope", "/nope"} {
		rr = do(t, h, http.MethodGet, p)
		if rr.Code != 404 || !strings.Contains(rr.Body.String(), `"not_found"`) {
			t.Fatalf("%s: %d %s", p, rr.Code, rr.Body.String())
		}
	}

	rr = do(t, h, http.MethodPost, "/api/v1/version")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST version: %d", rr.Code)
	}
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing on 405")
	}
}

func TestReadiness(t *testing.T) {
	s := newTestServer(t, nil)
	s.Health().Register("db", func(context.Context) error { return nil })
	if rr := do(t, s.Handler(), http.MethodGet, "/readyz"); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"db":"pass"`) {
		t.Fatalf("ready: %d %s", rr.Code, rr.Body.String())
	}
	s.Health().Register("db", func(context.Context) error { return errors.New("dial 10.1.2.3 refused") })
	rr := do(t, s.Handler(), http.MethodGet, "/readyz")
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "10.1.2.3") {
		t.Fatalf("not ready: %d %s", rr.Code, rr.Body.String())
	}
}

func TestRateLimitExemptsProbes(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.RateLimitRPS, c.RateLimitBurst = 0.001, 1 })
	h := s.Handler()
	for range 5 {
		if rr := do(t, h, http.MethodGet, "/healthz"); rr.Code != 200 {
			t.Fatalf("probe throttled: %d", rr.Code)
		}
	}
	if rr := do(t, h, http.MethodGet, "/api/v1/version"); rr.Code != 200 {
		t.Fatalf("first api call: %d", rr.Code)
	}
	rr := do(t, h, http.MethodGet, "/api/v1/version")
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("second api call: %d", rr.Code)
	}
}

func TestRateLimitDisabled(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.RateLimitRPS = 0 })
	for range 100 {
		if rr := do(t, s.Handler(), http.MethodGet, "/api/v1/version"); rr.Code != 200 {
			t.Fatalf("got %d", rr.Code)
		}
	}
}

func TestExtraRoutesAndPanic(t *testing.T) {
	cfg := config.Default()
	s, err := New(Options{Config: cfg, ExtraRoutes: func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/test/panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	}})
	if err != nil {
		t.Fatal(err)
	}
	rr := do(t, s.Handler(), http.MethodGet, "/api/v1/test/panic")
	if rr.Code != 500 || strings.Contains(rr.Body.String(), "boom") {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

// TestOpenAPICoversRoutes enforces FR-004 AC2.
func TestOpenAPICoversRoutes(t *testing.T) {
	var doc struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(api.OpenAPI, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Fatalf("openapi = %q", doc.OpenAPI)
	}
	s := newTestServer(t, nil)
	routes := s.routes()
	for _, rt := range routes {
		if _, ok := doc.Paths[rt.path][strings.ToLower(rt.method)]; !ok {
			t.Errorf("route %s %s missing from openapi.json", rt.method, rt.path)
		}
	}
	n := 0
	for _, ops := range doc.Paths {
		n += len(ops)
	}
	if n != len(routes) {
		t.Errorf("openapi documents %d operations, server registers %d", n, len(routes))
	}
}

func writeWeb(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "assets"), 0o750))
	must(os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>dv</title>"), 0o600))
	must(os.WriteFile(filepath.Join(dir, "assets", "app-abc.js"), []byte("console.log(1)"), 0o600))
	must(os.WriteFile(filepath.Join(dir, "favicon.svg"), []byte("<svg/>"), 0o600))
	return dir
}

func TestStaticSPA(t *testing.T) {
	dir := writeWeb(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, func(c *config.Config) { c.WebDir = dir })
	h := s.Handler()

	tests := []struct {
		path      string
		status    int
		contains  string
		cacheCtrl string
	}{
		{"/", 200, "<title>dv</title>", "no-cache"},
		{"/index.html", 200, "<title>dv</title>", "no-cache"},
		{"/dashboards/abc", 200, "<title>dv</title>", "no-cache"},
		{"/assets", 200, "<title>dv</title>", "no-cache"},
		{"/assets/app-abc.js", 200, "console.log", "public, max-age=31536000, immutable"},
		{"/favicon.svg", 200, "<svg/>", "no-cache"},
		{"/missing.js", 404, "", ""},
		{"/leak.txt", 404, "", ""},
		{"/../../etc/passwd", http.StatusMultipleChoices, "", ""}, // any 3xx: mux redirects to the cleaned path (301 before Go 1.27, 307 after)
	}
	for _, tt := range tests {
		rr := do(t, h, http.MethodGet, tt.path)
		if tt.status == http.StatusMultipleChoices {
			if rr.Code < 300 || rr.Code > 399 || rr.Header().Get("Location") != "/etc/passwd" {
				t.Errorf("%s: got %d Location=%q, want redirect to cleaned path", tt.path, rr.Code, rr.Header().Get("Location"))
			}
		} else if rr.Code != tt.status {
			t.Errorf("%s: status %d, want %d", tt.path, rr.Code, tt.status)
		}
		if !strings.Contains(rr.Body.String(), tt.contains) {
			t.Errorf("%s: body %q", tt.path, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "TOP SECRET") {
			t.Errorf("%s: symlink escape", tt.path)
		}
		if tt.cacheCtrl != "" && rr.Header().Get("Cache-Control") != tt.cacheCtrl {
			t.Errorf("%s: cache-control %q", tt.path, rr.Header().Get("Cache-Control"))
		}
		if rr.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: CSP missing", tt.path)
		}
	}
	if rr := do(t, h, http.MethodPost, "/"); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / = %d", rr.Code)
	}
	if rr := do(t, h, http.MethodGet, "/api/v1/zzz"); rr.Code != 404 || !strings.Contains(rr.Body.String(), "not_found") {
		t.Fatalf("api 404 must stay JSON with web UI enabled: %s", rr.Body.String())
	}
}

func TestStaticIndexRemovedAfterStart(t *testing.T) {
	dir := writeWeb(t)
	s := newTestServer(t, func(c *config.Config) { c.WebDir = dir })
	if err := os.Remove(filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	if rr := do(t, s.Handler(), http.MethodGet, "/"); rr.Code != 500 {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestNewStaticErrors(t *testing.T) {
	if _, err := New(Options{Config: func() config.Config { c := config.Default(); c.WebDir = t.TempDir(); return c }()}); err == nil {
		t.Fatal("expected error for web dir without index.html")
	}
	if _, err := newStaticHandler(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.ShutdownTimeout = config.Duration(2 * time.Second) })
	release := make(chan struct{})
	started := make(chan struct{})
	s2, err := New(Options{Config: s.cfg, Health: s.health, ExtraRoutes: func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/test/slow", func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			_, _ = io.WriteString(w, "done")
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s2.Serve(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	got := make(chan string, 1)
	go func() {
		resp, err := http.Get(base + "/api/v1/test/slow")
		if err != nil {
			got <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		got <- string(b)
	}()
	<-started
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for !s2.Health().Draining() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !s2.Health().Draining() {
		t.Fatal("readiness should flip to draining on shutdown")
	}
	close(release)
	if body := <-got; body != "done" {
		t.Fatalf("in-flight request not drained: %q", body)
	}
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestShutdownTimeoutExceeded(t *testing.T) {
	cfg := config.Default()
	cfg.ShutdownTimeout = config.Duration(50 * time.Millisecond)
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	s, err := New(Options{Config: cfg, ExtraRoutes: func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/test/stuck", func(http.ResponseWriter, *http.Request) { close(started); <-release })
	}})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/api/v1/test/stuck")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	cancel()
	if err := <-served; err == nil || !strings.Contains(err.Error(), "graceful shutdown") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeListenerError(t *testing.T) {
	s := newTestServer(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	if err := s.Serve(context.Background(), ln); err == nil {
		t.Fatal("expected serve error on closed listener")
	}
}

func TestListenAndServe(t *testing.T) {
	s := newTestServer(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.ListenAndServe(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	bad := newTestServer(t, func(c *config.Config) { c.HTTPAddr = "256.0.0.1:1" })
	if err := bad.ListenAndServe(context.Background()); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestNewDefaults(t *testing.T) {
	s, err := New(Options{Config: config.Default()})
	if err != nil {
		t.Fatal(err)
	}
	if s.Health() == nil || s.log == nil {
		t.Fatal("defaults not applied")
	}
	_ = health.Pass
}

// TestRateLimitDoesNotStarveSPA guards against the page document spending the token its
// own assets need (Codex review finding on PR #2).
func TestRateLimitDoesNotStarveSPA(t *testing.T) {
	dir := writeWeb(t)
	s := newTestServer(t, func(c *config.Config) {
		c.WebDir = dir
		c.RateLimitRPS, c.RateLimitBurst = 0.01, 1
	})
	h := s.Handler()
	for _, p := range []string{"/", "/assets/app-abc.js", "/favicon.svg", "/dashboards/x", "/assets/app-abc.js"} {
		if rr := do(t, h, http.MethodGet, p); rr.Code != http.StatusOK {
			t.Fatalf("%s: %d, static/SPA requests must not be rate limited", p, rr.Code)
		}
	}
	if rr := do(t, h, http.MethodGet, "/api/v1/version"); rr.Code != http.StatusOK {
		t.Fatalf("first API call: %d", rr.Code)
	}
	if rr := do(t, h, http.MethodGet, "/api/openapi.json"); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("API burst exhausted, want 429, got %d", rr.Code)
	}
}
