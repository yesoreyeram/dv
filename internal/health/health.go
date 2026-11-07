// Package health implements liveness and readiness reporting (FR-001, FR-002).
package health

import (
	"context"
	"fmt"
	"log/slog"
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

// Report is the aggregated readiness result.
type Report struct {
	Ready  bool              `json:"-"`
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Registry holds named readiness checks. The zero value is not usable; use New.
type Registry struct {
	mu       sync.RWMutex
	checks   map[string]CheckFunc
	timeout  time.Duration
	draining atomic.Bool
	log      *slog.Logger
}

// New returns a registry whose checks each run with the given timeout.
func New(timeout time.Duration, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Registry{checks: map[string]CheckFunc{}, timeout: timeout, log: log}
}

// Register adds or replaces a named check.
func (r *Registry) Register(name string, fn CheckFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks[name] = fn
}

// SetDraining marks the server as shutting down; readiness then always fails
// (NFR-AVAIL-001).
func (r *Registry) SetDraining() { r.draining.Store(true) }

// Draining reports whether SetDraining was called.
func (r *Registry) Draining() bool { return r.draining.Load() }

// Check runs all checks concurrently and aggregates the result.
func (r *Registry) Check(ctx context.Context) Report {
	r.mu.RLock()
	names := make([]string, 0, len(r.checks))
	for n := range r.checks {
		names = append(names, n)
	}
	fns := make([]CheckFunc, len(names))
	sort.Strings(names)
	for i, n := range names {
		fns[i] = r.checks[n]
	}
	r.mu.RUnlock()

	results := make([]string, len(names))
	var wg sync.WaitGroup
	for i := range names {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = r.run(ctx, names[i], fns[i])
		}(i)
	}
	wg.Wait()

	rep := Report{Ready: !r.Draining(), Checks: make(map[string]string, len(names))}
	for i, n := range names {
		rep.Checks[n] = results[i]
		if results[i] != Pass {
			rep.Ready = false
		}
	}
	rep.Status = "ready"
	if !rep.Ready {
		rep.Status = "not_ready"
	}
	return rep
}

func (r *Registry) run(ctx context.Context, name string, fn CheckFunc) string {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- fmt.Errorf("panic: %v", p)
			}
		}()
		done <- fn(ctx)
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
