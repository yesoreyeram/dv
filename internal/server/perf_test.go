package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/health"
	"github.com/yesoreyeram/dv/internal/loadtest"
)

// NFR-PERF-001 targets: 500 rps for at least 2 s, p99 < 10 ms on one core.
const (
	perfRate     = 500
	perfDuration = 2 * time.Second
	perfP99      = 10 * time.Millisecond
)

// serveTCP runs s on a loopback listener through the real http.Server (timeouts and
// all) and returns its base URL. The server is shut down when the test ends.
func serveTCP(t testing.TB, s *Server) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return "http://" + ln.Addr().String()
}

// singleCore pins the process to one P for the duration of the test, as NFR-PERF-001
// specifies. The load generator shares that core, which makes the measurement
// conservative rather than optimistic.
func singleCore(t testing.TB) {
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
}

func probe(t *testing.T, url string) loadtest.Result {
	t.Helper()
	client := loadtest.NewClient(5 * time.Second)
	defer client.CloseIdleConnections()
	res, err := loadtest.Run(context.Background(), loadtest.Options{Client: client, URL: url, Rate: perfRate, Duration: perfDuration})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %s", url, res)
	if res.Errors != 0 || res.Status[http.StatusOK] != res.Total() {
		t.Fatalf("not every probe returned 200: %s (first error %v)", res, res.FirstErr)
	}
	if want := int(perfRate * perfDuration.Seconds()); res.Total() != want {
		t.Fatalf("issued %d probes, want %d", res.Total(), want)
	}
	return res
}

// TestProbeLatencyUnderLoad verifies NFR-PERF-001 AC1 for both probes through the full
// middleware chain over real TCP.
func TestProbeLatencyUnderLoad(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			s := newTestServer(t, nil)
			s.Health().Register("plugins", func(context.Context) error { return nil })
			base := serveTCP(t, s)
			singleCore(t)
			res := probe(t, base+path)
			if p99 := res.Quantile(0.99); p99 >= perfP99 {
				t.Fatalf("p99 = %s, want < %s (%s)", p99, perfP99, res)
			}
		})
	}
}

// TestSlowReadinessCheckUnderLoad verifies NFR-PERF-001 AC2 (B-046): with a 50 ms
// check and the default 1 s cache, the check runs at most once per cache window and
// only the probes that arrive during a refresh wait for it.
func TestSlowReadinessCheckUnderLoad(t *testing.T) {
	const checkTime = 50 * time.Millisecond
	s := newTestServer(t, nil)
	ttl := s.cfg.ReadinessCacheTTL.Std()
	if ttl != time.Second {
		t.Fatalf("test assumes the default 1 s readiness cache, got %s", ttl)
	}
	var calls atomic.Int32
	s.Health().Register("plugins", func(ctx context.Context) error {
		calls.Add(1)
		select {
		case <-time.After(checkTime):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	base := serveTCP(t, s)
	singleCore(t)
	res := probe(t, base+"/readyz")

	maxCalls := int32(perfDuration/ttl) + 1
	if got := calls.Load(); got > maxCalls || got < 1 {
		t.Fatalf("check ran %d times, want 1..%d", got, maxCalls)
	}
	if p50 := res.Quantile(0.5); p50 >= perfP99 {
		t.Fatalf("p50 = %s, want < %s", p50, perfP99)
	}
	if p99 := res.Quantile(0.99); p99 >= checkTime+perfP99 {
		t.Fatalf("p99 = %s, want < %s", p99, checkTime+perfP99)
	}
}

// Benchmarks (NFR-PERF-001 AC3). Run with `make bench`.

func benchServer(b *testing.B, check health.CheckFunc) http.Handler {
	b.Helper()
	cfg := config.Default()
	cfg.HTTPAddr = "127.0.0.1:0"
	s, err := New(Options{Config: cfg})
	if err != nil {
		b.Fatal(err)
	}
	if check != nil {
		s.Health().Register("plugins", check)
	}
	return s.Handler()
}

func benchHandler(b *testing.B, h http.Handler, path string) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
			if rr.Code != http.StatusOK {
				b.Errorf("%s: status %d", path, rr.Code)
				return
			}
		}
	})
}

func BenchmarkHealthz(b *testing.B) {
	benchHandler(b, benchServer(b, nil), "/healthz")
}

func BenchmarkReadyz(b *testing.B) {
	benchHandler(b, benchServer(b, func(context.Context) error { return nil }), "/readyz")
}

// BenchmarkReadyzSlowCheck is the probe-flood case (B-046): a 20 ms check behind the
// shared, cached evaluation.
func BenchmarkReadyzSlowCheck(b *testing.B) {
	benchHandler(b, benchServer(b, func(ctx context.Context) error {
		select {
		case <-time.After(20 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}), "/readyz")
}

// BenchmarkProbeTCP measures a probe round trip over loopback TCP with keep-alive.
func BenchmarkProbeTCP(b *testing.B) {
	cfg := config.Default()
	cfg.HTTPAddr = "127.0.0.1:0"
	s, err := New(Options{Config: cfg})
	if err != nil {
		b.Fatal(err)
	}
	url := serveTCP(b, s) + "/healthz"
	client := loadtest.NewClient(5 * time.Second)
	defer client.CloseIdleConnections()
	b.ReportAllocs()
	for b.Loop() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}
