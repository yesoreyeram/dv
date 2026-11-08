package health

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmptyRegistryIsReady(t *testing.T) {
	rep := New(time.Second, nil).Check(context.Background())
	if !rep.Ready || rep.Status != "ready" || len(rep.Checks) != 0 {
		t.Fatalf("got %+v", rep)
	}
}

func TestAggregation(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return nil })
	r.Register("b", func(context.Context) error { return errors.New("secret host 10.0.0.1") })
	rep := r.Check(context.Background())
	if rep.Ready || rep.Status != "not_ready" {
		t.Fatalf("got %+v", rep)
	}
	if rep.Checks["a"] != Pass || rep.Checks["b"] != Fail {
		t.Fatalf("checks = %v", rep.Checks)
	}
}

func TestTimeout(t *testing.T) {
	r := New(20*time.Millisecond, nil)
	r.Register("slow", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return nil
	})
	start := time.Now()
	rep := r.Check(context.Background())
	if rep.Checks["slow"] != Fail {
		t.Fatalf("slow check should fail: %v", rep.Checks)
	}
	if time.Since(start) > time.Second {
		t.Fatal("timeout not enforced")
	}
}

func TestIgnoringContextStillTimesOut(t *testing.T) {
	r := New(20*time.Millisecond, nil)
	release := make(chan struct{})
	defer close(release)
	r.Register("stuck", func(context.Context) error { <-release; return nil })
	if rep := r.Check(context.Background()); rep.Checks["stuck"] != Fail {
		t.Fatalf("got %v", rep.Checks)
	}
}

func TestPanickingCheckFails(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("boom", func(context.Context) error { panic("kaboom") })
	if rep := r.Check(context.Background()); rep.Checks["boom"] != Fail {
		t.Fatalf("got %v", rep.Checks)
	}
}

func TestDraining(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return nil })
	r.SetDraining()
	if !r.Draining() {
		t.Fatal("draining not set")
	}
	if rep := r.Check(context.Background()); rep.Ready || rep.Status != "not_ready" {
		t.Fatalf("got %+v", rep)
	}
}

func TestRegisterReplaces(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return errors.New("x") })
	r.Register("a", func(context.Context) error { return nil })
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("got %+v", rep)
	}
}

// fakeClock lets cache-expiry tests run without sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClocked(timeout time.Duration) (*Registry, *fakeClock) {
	r := New(timeout, nil)
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	r.now = clk.now
	return r, clk
}

func TestConcurrentProbesShareOneEvaluation(t *testing.T) {
	r := New(time.Second, nil)
	var calls atomic.Int32
	gate := make(chan struct{})
	r.Register("a", func(context.Context) error { calls.Add(1); <-gate; return nil })

	const n = 64
	var wg sync.WaitGroup
	reps := make([]Report, n)
	for i := range n {
		wg.Add(1)
		go func(i int) { defer wg.Done(); reps[i] = r.Check(context.Background()) }(i)
	}
	// Let every goroutine join the in-flight evaluation before it completes.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("check ran %d times, want 1", got)
	}
	for i, rep := range reps {
		if !rep.Ready || rep.Checks["a"] != Pass {
			t.Fatalf("report %d = %+v", i, rep)
		}
	}
}

func TestResultIsCachedForTTL(t *testing.T) {
	r, clk := newClocked(time.Second)
	var calls atomic.Int32
	r.Register("a", func(context.Context) error { calls.Add(1); return nil })
	for range 5 {
		r.Check(context.Background())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls within TTL = %d, want 1", got)
	}
	clk.advance(DefaultCacheTTL)
	r.Check(context.Background()) // stale result; refresh runs in the background
	// Wait for the refresh to finish and store its result, not just to start: advancing
	// the clock while it is still running would stamp the result with the later time.
	eventually(t, func() bool { return calls.Load() == 2 && r.idle() })
	// Past TTL + timeout nothing stale is served: the caller waits and the check runs.
	clk.advance(DefaultCacheTTL + time.Second)
	r.Check(context.Background())
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls after stale bound = %d, want 3", got)
	}
}

// idle reports whether no evaluation is in flight (test-only view of the registry).
func (r *Registry) idle() bool {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	return r.inflight == nil
}

// eventually polls cond for up to two seconds.
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(time.Millisecond)
	}
}

// checkWithin runs Check and fails the test if it takes longer than d, which is how
// these tests tell a stale answer from one that waited for the checks.
func checkWithin(t *testing.T, r *Registry, d time.Duration) Report {
	t.Helper()
	got := make(chan Report, 1)
	go func() { got <- r.Check(context.Background()) }()
	select {
	case rep := <-got:
		return rep
	case <-time.After(d):
		t.Fatalf("Check blocked for more than %s", d)
		return Report{}
	}
}

// FR-002 AC7: after the TTL, probes get the previous result at once while a single
// refresh runs; the refreshed result replaces it when it completes.
func TestExpiredResultIsServedWhileRefreshing(t *testing.T) {
	r, clk := newClocked(time.Second)
	var calls atomic.Int32
	var failing atomic.Bool
	gate := make(chan struct{})
	r.Register("db", func(context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		<-gate // the refresh is slow...
		failing.Store(true)
		return errors.New("down") // ...and finds the dependency down
	})
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("first probe: %+v", rep)
	}

	clk.advance(DefaultCacheTTL)
	for i := range 20 {
		if rep := checkWithin(t, r, 500*time.Millisecond); !rep.Ready || rep.Checks["db"] != Pass {
			t.Fatalf("probe %d during refresh: %+v", i, rep)
		}
	}
	eventually(t, func() bool { return calls.Load() == 2 })
	time.Sleep(10 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Fatalf("probes during one refresh started %d check runs, want 2", got)
	}

	close(gate)
	eventually(t, failing.Load)
	eventually(t, func() bool { return !r.Check(context.Background()).Ready })
	if rep := r.Check(context.Background()); rep.Checks["db"] != Fail {
		t.Fatalf("refreshed result not served: %+v", rep)
	}
}

// FR-002 AC7: a result older than TTL + check timeout is never served.
func TestResultOlderThanStaleBoundIsNotServed(t *testing.T) {
	const timeout = 200 * time.Millisecond
	r, clk := newClocked(timeout)
	var down atomic.Bool
	r.Register("db", func(context.Context) error {
		if down.Load() {
			return errors.New("down")
		}
		return nil
	})
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("first probe: %+v", rep)
	}
	down.Store(true)
	clk.advance(DefaultCacheTTL + timeout) // exactly at the bound: no longer usable
	if rep := r.Check(context.Background()); rep.Ready || rep.Checks["db"] != Fail {
		t.Fatalf("result older than the stale bound was served: %+v", rep)
	}
}

func TestStaleResultStillHonoursDraining(t *testing.T) {
	r, clk := newClocked(time.Second)
	r.Register("a", func(context.Context) error { return nil })
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("got %+v", rep)
	}
	clk.advance(DefaultCacheTTL)
	r.SetDraining()
	if rep := checkWithin(t, r, 500*time.Millisecond); rep.Ready || rep.Status != "not_ready" {
		t.Fatalf("stale result ignored draining: %+v", rep)
	}
}

func TestRegisterInvalidatesStaleResult(t *testing.T) {
	r, clk := newClocked(time.Second)
	r.Register("a", func(context.Context) error { return nil })
	r.Check(context.Background())
	clk.advance(DefaultCacheTTL)
	r.Register("b", func(context.Context) error { return errors.New("down") })
	if rep := r.Check(context.Background()); rep.Ready || rep.Checks["b"] != Fail {
		t.Fatalf("invalidated result served stale: %+v", rep)
	}
}

func TestZeroTTLDisablesCaching(t *testing.T) {
	r := New(time.Second, nil)
	r.SetCacheTTL(0)
	var calls atomic.Int32
	r.Register("a", func(context.Context) error { calls.Add(1); return nil })
	for range 3 {
		r.Check(context.Background())
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
	r.SetCacheTTL(-time.Second) // treated as zero
	r.Check(context.Background())
	if got := calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4", got)
	}
}

func TestRegisterInvalidatesCache(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return nil })
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("got %+v", rep)
	}
	r.Register("b", func(context.Context) error { return errors.New("down") })
	if rep := r.Check(context.Background()); rep.Ready || rep.Checks["b"] != Fail {
		t.Fatalf("stale cached result returned: %+v", rep)
	}
}

func TestDrainingBypassesCache(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return nil })
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("got %+v", rep)
	}
	r.SetDraining()
	rep := r.Check(context.Background())
	if rep.Ready || rep.Status != "not_ready" || rep.Checks["a"] != Pass {
		t.Fatalf("draining not applied to cached result: %+v", rep)
	}
}

func TestReturnedReportsDoNotShareState(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return nil })
	first := r.Check(context.Background())
	first.Checks["a"] = Fail
	if second := r.Check(context.Background()); second.Checks["a"] != Pass {
		t.Fatalf("mutating one report changed the cache: %+v", second)
	}
}

func TestStuckCheckIsNotRestarted(t *testing.T) {
	r, clk := newClocked(20 * time.Millisecond)
	var starts atomic.Int32
	release := make(chan struct{})
	r.Register("mount", func(context.Context) error { starts.Add(1); <-release; return nil })

	for range 5 {
		if rep := r.Check(context.Background()); rep.Checks["mount"] != Fail {
			t.Fatalf("got %+v", rep)
		}
		clk.advance(DefaultCacheTTL)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("stuck check started %d times, want 1", got)
	}

	// Once it returns, the next evaluation runs it again and it can pass.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		clk.advance(DefaultCacheTTL)
		if rep := r.Check(context.Background()); rep.Ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("check never recovered after being released")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 2: the run that recovered. A stale-served probe may also have started one
	// more background refresh by now (FR-002 AC7), which is fine once it is free.
	if got := starts.Load(); got < 2 || got > 3 {
		t.Fatalf("starts after recovery = %d, want 2 or 3", got)
	}
}

func TestCallerCancellationReturnsFailWithoutPoisoningOthers(t *testing.T) {
	r := New(time.Second, nil)
	gate := make(chan struct{})
	r.Register("a", func(context.Context) error { <-gate; return nil })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep := r.Check(ctx)
	if rep.Ready || rep.Status != "not_ready" || rep.Checks["a"] != Fail {
		t.Fatalf("cancelled caller got %+v", rep)
	}
	// The shared evaluation was detached from the cancelled caller and still passes.
	close(gate)
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("second caller got %+v", rep)
	}
}

// A check that has returned must never be reported as "still running" by the next
// evaluation. The running flag used to be cleared after the result was handed back,
// so back-to-back probes could see a finished check as stuck and flap to 503.
func TestFinishedCheckIsNeverReportedAsRunning(t *testing.T) {
	r := New(time.Second, nil)
	r.SetCacheTTL(0)
	var calls atomic.Int32
	r.Register("a", func(context.Context) error { calls.Add(1); return nil })
	_, entries := r.snapshot()
	e := entries[0]
	// Busy goroutines keep every P occupied so the check goroutine is often preempted
	// right after it hands back its result, which is the window this test guards.
	stop := make(chan struct{})
	var busy sync.WaitGroup
	for range 2 * runtime.GOMAXPROCS(0) {
		busy.Add(1)
		go func() {
			defer busy.Done()
			for {
				select {
				case <-stop:
					return
				default:
					runtime.Gosched()
				}
			}
		}()
	}
	defer func() { close(stop); busy.Wait() }()
	const n = 20_000
	for i := range n {
		if rep := r.Check(context.Background()); !rep.Ready {
			t.Fatalf("probe %d: %+v after %d check runs", i, rep, calls.Load())
		}
		// The result reaching the caller happens after the flag is cleared.
		if e.running.Load() {
			t.Fatalf("probe %d: check still marked running after its result was returned", i)
		}
	}
	if got := calls.Load(); got != n {
		t.Fatalf("check ran %d times, want %d", got, n)
	}
}
