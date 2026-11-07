// Package bdd executes the API Gherkin specifications in features/api with godog
// against an in-process server (ADR-0006).
package bdd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/buildinfo"
	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/health"
	"github.com/yesoreyeram/dv/internal/httpx"
	"github.com/yesoreyeram/dv/internal/server"
)

func TestAPIFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "api",
		ScenarioInitializer: initializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features/api"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("API feature suite failed")
	}
}

type world struct {
	cfg       config.Config
	build     buildinfo.Info
	health    *health.Registry
	withPanic bool
	srv       *server.Server

	resp      *httptest.ResponseRecorder
	responses []*httptest.ResponseRecorder

	checkCalls map[string]*atomic.Int32 // invocations per readiness check
	release    chan struct{}            // unblocks stuck readiness checks after a scenario
}

func newWorld() *world {
	cfg := config.Default()
	return &world{
		cfg: cfg, build: buildinfo.Get(), health: health.New(cfg.ReadinessCheckTimeout.Std(), nil),
		checkCalls: map[string]*atomic.Int32{},
	}
}

func (w *world) server() (*server.Server, error) {
	if w.srv != nil {
		return w.srv, nil
	}
	opts := server.Options{Config: w.cfg, Build: w.build, Health: w.health}
	if w.withPanic {
		opts.ExtraRoutes = func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/v1/test/panic", func(http.ResponseWriter, *http.Request) {
				panic("simulated failure")
			})
		}
	}
	s, err := server.New(opts)
	if err != nil {
		return nil, err
	}
	w.srv = s
	return s, nil
}

func (w *world) get(path string, header map[string]string) (*httptest.ResponseRecorder, error) {
	return w.getFrom(path, "", header)
}

func (w *world) getFrom(path, remote string, header map[string]string) (*httptest.ResponseRecorder, error) {
	s, err := w.server()
	if err != nil {
		return nil, err
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if remote != "" {
		req.RemoteAddr = remote
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr, nil
}

func initializeScenario(sc *godog.ScenarioContext) {
	w := newWorld()
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*w = *newWorld()
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if w.release != nil {
			close(w.release)
		}
		return ctx, nil
	})

	sc.Step(`^the dv server is running$`, func() error { return nil })
	sc.Step(`^the dv server is running with version "([^"]*)" and commit "([^"]*)"$`, func(v, c string) error {
		w.build.Version, w.build.Commit = v, c
		return nil
	})
	sc.Step(`^the dv server is running with a rate limit of (\d+) requests per second and burst (\d+)$`, func(rps, burst int) error {
		w.cfg.RateLimitRPS, w.cfg.RateLimitBurst = float64(rps), burst
		return nil
	})
	sc.Step(`^the dv server is running with a route that panics$`, func() error { w.withPanic = true; return nil })
	sc.Step(`^the readiness check "([^"]*)" passes$`, func(name string) error {
		w.health.Register(name, func(context.Context) error { return nil })
		return nil
	})
	sc.Step(`^the readiness check "([^"]*)" fails with "([^"]*)"$`, func(name, msg string) error {
		w.health.Register(name, func(context.Context) error { return errors.New(msg) })
		return nil
	})
	sc.Step(`^the readiness check "([^"]*)" passes and counts its calls$`, func(name string) error {
		n := &atomic.Int32{}
		w.checkCalls[name] = n
		w.health.Register(name, func(context.Context) error {
			n.Add(1)
			time.Sleep(20 * time.Millisecond) // long enough for a burst of probes to overlap
			return nil
		})
		return nil
	})
	sc.Step(`^the readiness check "([^"]*)" blocks forever ignoring cancellation$`, func(name string) error {
		// Short timeout and TTL keep the scenario fast; the server applies the TTL.
		w.cfg.ReadinessCheckTimeout = config.Duration(50 * time.Millisecond)
		w.cfg.ReadinessCacheTTL = config.Duration(50 * time.Millisecond)
		w.health = health.New(w.cfg.ReadinessCheckTimeout.Std(), nil)
		w.release = make(chan struct{})
		n := &atomic.Int32{}
		w.checkCalls[name] = n
		release := w.release
		w.health.Register(name, func(context.Context) error { n.Add(1); <-release; return nil })
		return nil
	})
	sc.Step(`^I send (\d+) concurrent GET requests to "([^"]*)"$`, func(n int, path string) error {
		if _, err := w.server(); err != nil {
			return err
		}
		w.responses = make([]*httptest.ResponseRecorder, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func(i int) { defer wg.Done(); w.responses[i], errs[i] = w.get(path, nil) }(i)
		}
		wg.Wait()
		return errors.Join(errs...)
	})
	sc.Step(`^I wait for the readiness cache to expire$`, func() error {
		time.Sleep(w.cfg.ReadinessCacheTTL.Std() + 20*time.Millisecond)
		return nil
	})
	sc.Step(`^every response status should be (\d+)$`, func(code int) error {
		for i, rr := range w.responses {
			if rr.Code != code {
				return fmt.Errorf("response %d status = %d, want %d", i, rr.Code, code)
			}
		}
		return nil
	})
	checkCount := func(name string) (int, error) {
		n, ok := w.checkCalls[name]
		if !ok {
			return 0, fmt.Errorf("readiness check %q is not counted", name)
		}
		return int(n.Load()), nil
	}
	sc.Step(`^the readiness check "([^"]*)" should have run at most (\d+) times?$`, func(name string, max int) error {
		got, err := checkCount(name)
		if err == nil && got > max {
			err = fmt.Errorf("readiness check %q ran %d times, want at most %d", name, got, max)
		}
		return err
	})
	sc.Step(`^the readiness check "([^"]*)" should have been started (\d+) times?$`, func(name string, want int) error {
		got, err := checkCount(name)
		if err == nil && got != want {
			err = fmt.Errorf("readiness check %q started %d times, want %d", name, got, want)
		}
		return err
	})
	sc.Step(`^the server has started shutting down$`, func() error { w.health.SetDraining(); return nil })

	sc.Step(`^I send a GET request to "([^"]*)"$`, func(path string) (err error) {
		w.resp, err = w.get(path, nil)
		return err
	})
	sc.Step(`^I send a GET request to "([^"]*)" with header "([^"]*)" set to "([^"]*)"$`, func(path, k, v string) (err error) {
		w.resp, err = w.get(path, map[string]string{k: v})
		return err
	})
	sc.Step(`^I send a GET request to "([^"]*)" from peer address "([^"]*)"$`, func(path, remote string) (err error) {
		w.resp, err = w.getFrom(path, remote, nil)
		return err
	})
	sc.Step(`^I send (\d+) GET requests to "([^"]*)" in quick succession$`, func(n int, path string) error {
		w.responses = nil
		for range n {
			rr, err := w.get(path, nil)
			if err != nil {
				return err
			}
			w.responses = append(w.responses, rr)
		}
		return nil
	})

	sc.Step(`^the response status should be (\d+)$`, func(code int) error {
		if w.resp.Code != code {
			return fmt.Errorf("status = %d, want %d (body %s)", w.resp.Code, code, w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^the JSON field "([^"]*)" should be "([^"]*)"$`, func(field, want string) error {
		got, err := jsonField(w.resp.Body.Bytes(), field)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s = %q, want %q", field, got, want)
		}
		return nil
	})
	sc.Step(`^the JSON field "([^"]*)" should start with "([^"]*)"$`, func(field, prefix string) error {
		got, err := jsonField(w.resp.Body.Bytes(), field)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(got, prefix) {
			return fmt.Errorf("%s = %q, want prefix %q", field, got, prefix)
		}
		return nil
	})
	sc.Step(`^the response body should not contain "([^"]*)"$`, func(s string) error {
		if strings.Contains(w.resp.Body.String(), s) {
			return fmt.Errorf("body contains %q: %s", s, w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^the response header "([^"]*)" should be "([^"]*)"$`, func(h, want string) error {
		if got := w.resp.Header().Get(h); got != want {
			return fmt.Errorf("%s = %q, want %q", h, got, want)
		}
		return nil
	})
	sc.Step(`^the response header "([^"]*)" should not be "([^"]*)"$`, func(h, bad string) error {
		if got := w.resp.Header().Get(h); got == bad {
			return fmt.Errorf("%s = %q", h, got)
		}
		return nil
	})
	sc.Step(`^the response header "([^"]*)" should contain "([^"]*)"$`, func(h, sub string) error {
		if got := w.resp.Header().Get(h); !strings.Contains(got, sub) {
			return fmt.Errorf("%s = %q, want to contain %q", h, got, sub)
		}
		return nil
	})
	sc.Step(`^the response header "([^"]*)" should not contain "([^"]*)"$`, func(h, sub string) error {
		if got := w.resp.Header().Get(h); strings.Contains(got, sub) {
			return fmt.Errorf("%s = %q contains %q", h, got, sub)
		}
		return nil
	})
	sc.Step(`^the response header "([^"]*)" should be a valid request ID$`, func(h string) error {
		if got := w.resp.Header().Get(h); !httpx.ValidRequestID(got) {
			return fmt.Errorf("%s = %q is not a valid request ID", h, got)
		}
		return nil
	})
	sc.Step(`^at least (\d+) responses? should have status (\d+)$`, func(n, code int) error {
		count := 0
		for _, r := range w.responses {
			if r.Code == code {
				count++
			}
		}
		if count < n {
			return fmt.Errorf("%d responses with status %d, want >= %d", count, code, n)
		}
		return nil
	})
	sc.Step(`^every 429 response should have a "([^"]*)" header$`, func(h string) error {
		for i, r := range w.responses {
			if r.Code == http.StatusTooManyRequests && r.Header().Get(h) == "" {
				return fmt.Errorf("response %d lacks %s", i, h)
			}
		}
		return nil
	})
}

// jsonField resolves a dotted path ("error.code") in a JSON object and returns its
// string form.
func jsonField(body []byte, path string) (string, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("body is not JSON: %w (%s)", err, body)
	}
	for _, part := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%s: not an object at %q", path, part)
		}
		if v, ok = m[part]; !ok {
			return "", fmt.Errorf("%s: field %q missing in %s", path, part, body)
		}
	}
	switch t := v.(type) {
	case string:
		return t, nil
	default:
		b, _ := json.Marshal(t)
		return string(b), nil
	}
}
