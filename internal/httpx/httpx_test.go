package httpx

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func ok(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }

func TestChainOrder(t *testing.T) {
	var order []string
	mk := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(http.HandlerFunc(ok), mk("a"), mk("b"), mk("c"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Join(order, "") != "abc" {
		t.Fatalf("order = %v", order)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(ok))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Content-Security-Policy":      ContentSecurityPolicy,
	}
	for k, v := range want {
		if got := rr.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if rr.Header().Get("Permissions-Policy") == "" {
		t.Error("Permissions-Policy missing")
	}
	if rr.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must only be sent over TLS")
	}
	for _, bad := range []string{"unsafe-inline", "unsafe-eval", "*"} {
		if strings.Contains(ContentSecurityPolicy, bad) {
			t.Errorf("CSP contains %q", bad)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Header().Get("Strict-Transport-Security"), "max-age=") {
		t.Error("HSTS missing over TLS")
	}
	if !strings.HasSuffix(rr.Header().Get("Content-Security-Policy"), "upgrade-insecure-requests") {
		t.Error("CSP should upgrade insecure requests over TLS")
	}
}

func TestMaxBody(t *testing.T) {
	h := MaxBody(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			return
		}
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("toolong")))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("nil body code = %d", rr.Code)
	}
}

func TestRequestID(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rr.Header().Get(RequestIDHeader); len(got) != 32 || got != seen {
		t.Fatalf("generated id = %q, ctx = %q", got, seen)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "client-abc.123")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get(RequestIDHeader) != "client-abc.123" {
		t.Fatal("valid client id not kept")
	}

	for _, bad := range []string{"bad id", "x\ny", strings.Repeat("a", 65), "<script>"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(RequestIDHeader, bad)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if got := rr.Header().Get(RequestIDHeader); got == bad || !ValidRequestID(got) {
			t.Errorf("bad id %q not replaced: %q", bad, got)
		}
	}
	if RequestIDFrom(t.Context()) != "" || TraceIDFrom(t.Context()) != "" {
		t.Error("empty context should yield empty ids")
	}
}

func TestParseTraceparent(t *testing.T) {
	const tid = "4bf92f3577b34da6a3ce929d0e0e4736"
	tests := map[string]string{
		"00-" + tid + "-00f067aa0ba902b7-01":                      tid,
		"01-" + tid + "-00f067aa0ba902b7-01-ext":                  tid,
		"00-" + tid + "-00f067aa0ba902b7-01-ext":                  "",
		"ff-" + tid + "-00f067aa0ba902b7-01":                      "",
		"00-" + strings.ToUpper(tid) + "-00f067aa0ba902b7-01":     "",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01": "",
		"00-" + tid + "-0000000000000000-01":                      "",
		"00-" + tid + "-00f067aa0ba902b7":                         "",
		"0-" + tid + "-00f067aa0ba902b7-01":                       "",
		"":                                                        "",
	}
	for in, want := range tests {
		if got := ParseTraceparent(in); got != want {
			t.Errorf("ParseTraceparent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTraceContextAndContextHandler(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(ContextHandler{slog.NewJSONHandler(&buf, nil)}).With("svc", "dv").WithGroup("g")
	h := RequestID(TraceContext(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		log.InfoContext(r.Context(), "hello", "k", "v")
	})))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set(RequestIDHeader, "rid-1")
	h.ServeHTTP(httptest.NewRecorder(), req)
	out := buf.String()
	for _, want := range []string{`"request_id":"rid-1"`, `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`, `"svc":"dv"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log %s missing %s", out, want)
		}
	}
}

func TestAccessLog(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusAccepted) // ignored, first status wins
		_, _ = io.WriteString(w, "hey")
	})
	mux.HandleFunc("GET /implicit", ok)
	mux.HandleFunc("GET /empty", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	h := AccessLog(log)(mux)

	cases := []struct {
		path, route string
		status      int
	}{
		{"/items/7?token=secret", "GET /items/{id}", 201},
		{"/implicit", "GET /implicit", 200},
		{"/empty", "GET /empty", 200},
		{"/fail", "GET /fail", 502},
	}
	for _, c := range cases {
		buf.Reset()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, c.path, nil))
		var rec map[string]any
		if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
			t.Fatalf("%s: %v (%s)", c.path, err, buf.String())
		}
		if rec["route"] != c.route || int(rec["status"].(float64)) != c.status {
			t.Errorf("%s: got %v", c.path, rec)
		}
		if strings.Contains(buf.String(), "secret") {
			t.Errorf("query string leaked into logs: %s", buf.String())
		}
		if c.status >= 500 && rec["level"] != "ERROR" {
			t.Errorf("5xx should log at ERROR: %v", rec["level"])
		}
	}
	buf.Reset()
	AccessLog(log)(http.HandlerFunc(ok)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !strings.Contains(buf.String(), `"route":"unmatched"`) {
		t.Errorf("unmatched route not labelled: %s", buf.String())
	}
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if sr.Unwrap() == nil {
		t.Fatal("unwrap returned nil")
	}
}

func TestRecoverer(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	h := RequestID(Recoverer(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("db password=hunter2") })))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d", rr.Code)
	}
	var body ErrorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "internal" || body.Error.RequestID == "" {
		t.Fatalf("body = %+v", body)
	}
	if strings.Contains(rr.Body.String(), "hunter2") || strings.Contains(rr.Body.String(), "goroutine") {
		t.Fatal("panic details leaked to client")
	}
	if !strings.Contains(buf.String(), "stack") {
		t.Fatal("stack not logged server-side")
	}

	abort := Recoverer(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("expected ErrAbortHandler re-panic, got %v", err)
		}
	}()
	abort.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestWriteJSONEncodeFailure(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, http.StatusOK, map[string]any{"bad": math.Inf(1)})
	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "internal") {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache-control missing")
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func TestRateLimiterTokenBucket(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := NewRateLimiter(2, 2, 10, clk.Now)
	for i := range 2 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d should pass", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 || wait > 500*time.Millisecond {
		t.Fatalf("third request: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("other client should be independent")
	}
	clk.Add(500 * time.Millisecond)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("token should refill")
	}
}

func TestRateLimiterBoundedTable(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := NewRateLimiter(1, 1, 2, clk.Now)
	l.Allow("a")
	l.Allow("b")
	// Table full of active clients: newcomers share one overflow bucket (burst 1).
	if ok, _ := l.Allow("c"); !ok {
		t.Fatal("first newcomer should get the overflow token")
	}
	if ok, wait := l.Allow("d"); ok || wait <= 0 {
		t.Fatalf("second newcomer must share the drained overflow bucket: ok=%v wait=%v", ok, wait)
	}
	if l.Len() != 2 {
		t.Fatalf("overflow must not grow the table: len = %d", l.Len())
	}
	clk.Add(4 * time.Minute)
	if ok, _ := l.Allow("c"); !ok {
		t.Fatal("idle clients should be evicted")
	}
	if l.Len() != 1 {
		t.Fatalf("len = %d", l.Len())
	}
	if NewRateLimiter(1, 1, 1, nil).now == nil {
		t.Fatal("default clock not set")
	}
}

func TestRateLimiterSweepFullTable(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := NewRateLimiter(1, 1, 2, clk.Now)
	l.Allow("a")
	clk.Add(2 * time.Minute)
	l.Allow("b")
	clk.Add(90 * time.Second) // a idle 3m30s, b idle 1m30s; periodic sweep removes a
	if ok, _ := l.Allow("c"); !ok {
		t.Fatal("sweep should free a slot")
	}
}

func TestRateLimiterFullTableSweepIsThrottled(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := NewRateLimiter(1000, 1000, 2, clk.Now)
	l.Allow("a")
	l.Allow("b")
	clk.Add(10 * time.Millisecond)
	l.Allow("c") // full, but a sweep ran <1s ago: straight to overflow
	swept := l.lastSweep
	for i := range 50 {
		clk.Add(time.Millisecond)
		l.Allow(fmt.Sprintf("n%d", i))
	}
	if !l.lastSweep.Equal(swept) {
		t.Fatal("newcomers on a full table must not sweep more than once per interval")
	}
	clk.Add(sweepMinInterval)
	l.Allow("z")
	if l.lastSweep.Equal(swept) {
		t.Fatal("sweep should run again after the interval")
	}
}

func TestRateLimiterFullTableIsNotOSquared(t *testing.T) {
	// 20k entries, 20k newcomers: with a per-miss O(n) sweep this is ~4e8 map visits.
	const n = 20_000
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := NewRateLimiter(1, 1, n, clk.Now)
	for i := range n {
		l.Allow(fmt.Sprintf("a%d", i))
	}
	start := time.Now()
	for i := range n {
		l.Allow(fmt.Sprintf("b%d", i))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("newcomers on a full table took %v", d)
	}
}

func TestClientKey(t *testing.T) {
	cases := map[string]string{
		"192.0.2.1:1234":             "192.0.2.1",
		"[2001:db8:1:2:aaaa::1]:443": "2001:db8:1:2::/64",
		"[2001:db8:1:2:bbbb::9]:443": "2001:db8:1:2::/64",
		"[2001:db8:1:3::1]:443":      "2001:db8:1:3::/64",
		"[::ffff:192.0.2.7]:80":      "192.0.2.7",
		"[fe80::1%eth0]:80":          "fe80::/64",
		"garbage":                    "garbage",
		"[not-an-ip]:80":             "not-an-ip",
	}
	for remote, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		if got := ClientKey(req); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestRateLimitMiddlewareSharesIPv6Slash64(t *testing.T) {
	l := NewRateLimiter(1, 1, 10, nil)
	h := l.Middleware(http.HandlerFunc(ok))
	codes := make([]int, 0, 2)
	for _, remote := range []string{"[2001:db8::1]:1", "[2001:db8::2]:1"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		codes = append(codes, rr.Code)
	}
	if codes[0] != 200 || codes[1] != http.StatusTooManyRequests {
		t.Fatalf("addresses in one /64 must share a bucket: %v", codes)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	l := NewRateLimiter(1, 1, 10, nil)
	h := RequestID(l.Middleware(http.HandlerFunc(ok)))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("first = %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") != "1" {
		t.Fatalf("second = %d retry=%q", rr.Code, rr.Header().Get("Retry-After"))
	}
	if !strings.Contains(rr.Body.String(), "rate_limited") {
		t.Fatal(rr.Body.String())
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8::1]:443"
	if ClientIP(req) != "2001:db8::1" {
		t.Fatal(ClientIP(req))
	}
	req.RemoteAddr = "garbage"
	if ClientIP(req) != "garbage" {
		t.Fatal(ClientIP(req))
	}
}
