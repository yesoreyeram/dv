// Package health implements liveness and readiness reporting (FR-001, FR-002).
package health

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// CheckFunc reports whether a dependency is ready. It must honour ctx cancellation.
type CheckFunc func(ctx context.Context) error

// Status values returned to clients. Error details are never exposed (FR-002 AC4).
const (
	Pass = "pass"
	Fail = "fail"
)

// DefaultCacheTTL is how long a completed readiness evaluation is reused (FR-002 AC5).
const DefaultCacheTTL = time.Second

// Report is the aggregated readiness result.
type Report struct {
	Ready  bool              `json:"-"`
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// entry is one registered check. running guards against starting a check again while
// an earlier invocation that ignored its context is still executing (FR-002 AC6).
type entry struct {
	fn      CheckFunc
	running atomic.Bool
}

// evaluation is one shared run of all checks; concurrent probes wait on done.
type evaluation struct {
	done chan struct{}
	gen  uint64
	rep  Report
}

// Registry holds named readiness checks. The zero value is not usable; use New.
type Registry struct {
	mu       sync.RWMutex
	checks   map[string]*entry
	timeout  time.Duration
	draining atomic.Bool
	log      *slog.Logger

	// cacheMu guards the fields below.
	cacheMu  sync.Mutex
	ttl      time.Duration
	gen      uint64 // bumped by Register and SetCacheTTL so stale results are not reused
	cached   *evaluation
	cachedAt time.Time
	inflight *evaluation
	now      func() time.Time
}

// New returns a registry whose checks each run with the given timeout. Completed
// evaluations are reused for DefaultCacheTTL; see SetCacheTTL.
func New(timeout time.Duration, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Registry{checks: map[string]*entry{}, timeout: timeout, log: log, ttl: DefaultCacheTTL, now: time.Now}
}

// SetCacheTTL sets how long a completed evaluation is reused. Zero disables reuse;
// concurrent probes still share a single in-flight evaluation. Negative values are
// treated as zero.
func (r *Registry) SetCacheTTL(d time.Duration) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.ttl = max(d, 0)
	r.gen++
	r.cached = nil
}

// Register adds or replaces a named check and invalidates any cached result.
func (r *Registry) Register(name string, fn CheckFunc) {
	r.mu.Lock()
	r.checks[name] = &entry{fn: fn}
	r.mu.Unlock()

	r.cacheMu.Lock()
	r.gen++
	r.cached = nil
	r.cacheMu.Unlock()
}

// SetDraining marks the server as shutting down; readiness then always fails
// (NFR-AVAIL-001). It takes effect on the very next Check, cached or not.
func (r *Registry) SetDraining() { r.draining.Store(true) }

// Draining reports whether SetDraining was called.
func (r *Registry) Draining() bool { return r.draining.Load() }

// Check returns the aggregated readiness. Concurrent callers share one evaluation,
// and a completed evaluation is reused for the cache TTL (FR-002 AC5). If ctx ends
// before the shared evaluation completes, every check is reported as failed.
func (r *Registry) Check(ctx context.Context) Report {
	r.cacheMu.Lock()
	if c := r.cached; c != nil && c.gen == r.gen && r.now().Sub(r.cachedAt) < r.ttl {
		r.cacheMu.Unlock()
		return r.finalize(c.rep)
	}
	ev := r.inflight
	if ev == nil {
		ev = &evaluation{done: make(chan struct{}), gen: r.gen}
		r.inflight = ev
		// Detach from the first caller's cancellation (but keep its values for logging)
		// so one impatient client cannot fail the result every other caller shares.
		go r.evaluate(context.WithoutCancel(ctx), ev)
	}
	r.cacheMu.Unlock()

	select {
	case <-ev.done:
		return r.finalize(ev.rep)
	case <-ctx.Done():
		return r.finalize(r.allFailed())
	}
}

func (r *Registry) evaluate(ctx context.Context, ev *evaluation) {
	ev.rep = r.runAll(ctx)
	r.cacheMu.Lock()
	if r.inflight == ev {
		r.inflight = nil
	}
	if ev.gen == r.gen && r.ttl > 0 {
		r.cached, r.cachedAt = ev, r.now()
	}
	r.cacheMu.Unlock()
	close(ev.done)
}

// finalize copies the shared report and applies the live draining state.
func (r *Registry) finalize(shared Report) Report {
	rep := Report{Ready: shared.Ready && !r.Draining(), Checks: maps.Clone(shared.Checks)}
	if rep.Checks == nil {
		rep.Checks = map[string]string{}
	}
	rep.Status = "ready"
	if !rep.Ready {
		rep.Status = "not_ready"
	}
	return rep
}

func (r *Registry) snapshot() ([]string, []*entry) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.checks))
	for n := range r.checks {
		names = append(names, n)
	}
	sort.Strings(names)
	entries := make([]*entry, len(names))
	for i, n := range names {
		entries[i] = r.checks[n]
	}
	return names, entries
}

func (r *Registry) allFailed() Report {
	names, _ := r.snapshot()
	rep := Report{Checks: make(map[string]string, len(names))}
	for _, n := range names {
		rep.Checks[n] = Fail
	}
	return rep
}

func (r *Registry) runAll(ctx context.Context) Report {
	names, entries := r.snapshot()
	results := make([]string, len(names))
	var wg sync.WaitGroup
	for i := range names {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = r.run(ctx, names[i], entries[i])
		}(i)
	}
	wg.Wait()

	rep := Report{Ready: true, Checks: make(map[string]string, len(names))}
	for i, n := range names {
		rep.Checks[n] = results[i]
		if results[i] != Pass {
			rep.Ready = false
		}
	}
	return rep
}

func (r *Registry) run(ctx context.Context, name string, e *entry) string {
	if !e.running.CompareAndSwap(false, true) {
		r.log.WarnContext(ctx, "readiness check still running from an earlier probe", "check", name)
		return Fail
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer e.running.Store(false)
		defer func() {
			if p := recover(); p != nil {
				done <- fmt.Errorf("panic: %v", p)
			}
		}()
		done <- e.fn(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			r.log.WarnContext(ctx, "readiness check failed", "check", name, "error", err)
			return Fail
		}
		return Pass
	case <-ctx.Done():
		r.log.WarnContext(ctx, "readiness check timed out", "check", name, "error", ctx.Err())
		return Fail
	}
}
