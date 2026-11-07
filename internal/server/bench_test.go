package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/health"
	"github.com/yesoreyeram/dv/internal/loadtest"
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
