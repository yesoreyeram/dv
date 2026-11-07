//go:build perf

// Load tests for NFR-PERF-001 AC1/AC2. They measure latency on one core, so they
// run in isolation (`make perf`, and a dedicated CI step) rather than alongside
// the race-instrumented unit tests, which would compete for that core.

package server

import (
	"context"
	"net/http"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/loadtest"
)

// NFR-PERF-001 targets: 500 rps for at least 2 s, p99 < 10 ms on one core.
const (
	perfRate     = 500
	perfDuration = 2 * time.Second
	perfP99      = 10 * time.Millisecond
)

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

const slowCheckTime = 50 * time.Millisecond

// slowServer serves /readyz over TCP with one readiness check that takes
// slowCheckTime, and counts how often that check runs.
func slowServer(t *testing.T) (base string, ttl time.Duration, calls *atomic.Int32) {
	t.Helper()
	s := newTestServer(t, nil)
	ttl = s.cfg.ReadinessCacheTTL.Std()
	if ttl != time.Second {
		t.Fatalf("test assumes the default 1 s readiness cache, got %s", ttl)
	}
	calls = &atomic.Int32{}
	s.Health().Register("plugins", func(ctx context.Context) error {
		calls.Add(1)
		select {
		case <-time.After(slowCheckTime):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	return serveTCP(t, s), ttl, calls
}

// TestSlowReadinessCheckUnderLoad covers a cold start under load (B-046): with a
// 50 ms check and the default 1 s cache, the check runs at most once per cache window,
// and only the probes that arrive before the first result exists wait for it.
func TestSlowReadinessCheckUnderLoad(t *testing.T) {
	const checkTime = slowCheckTime
	base, ttl, calls := slowServer(t)
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

// TestSlowReadinessCheckWarmUnderLoad verifies NFR-PERF-001 AC2 (B-047): once one
// evaluation has completed, refreshes of a 50 ms check run in the background, so no
// probe waits for it and p99 stays under 10 ms.
func TestSlowReadinessCheckWarmUnderLoad(t *testing.T) {
	base, ttl, calls := slowServer(t)
	warm := loadtest.NewClient(5 * time.Second)
	resp, err := warm.Get(base + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	warm.CloseIdleConnections()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("warm-up probe returned %d", resp.StatusCode)
	}
	singleCore(t)
	res := probe(t, base+"/readyz")

	// The warm-up run plus at most one refresh per cache window.
	maxCalls := int32(perfDuration/ttl) + 2
	if got := calls.Load(); got > maxCalls || got < 2 {
		t.Fatalf("check ran %d times, want 2..%d", got, maxCalls)
	}
	if p99 := res.Quantile(0.99); p99 >= perfP99 {
		t.Fatalf("p99 = %s, want < %s (%s)", p99, perfP99, res)
	}
}
