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
	"strconv"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/buildinfo"
	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/health"
	"github.com/yesoreyeram/dv/internal/httpx"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugin/plugintest"
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
	plugins   *plugin.Registry
	regErr    error
	srv       *server.Server

	resp      *httptest.ResponseRecorder
	responses []*httptest.ResponseRecorder
}

func newWorld() *world {
	cfg := config.Default()
	return &world{cfg: cfg, build: buildinfo.Get(), health: health.New(cfg.ReadinessCheckTimeout.Std(), nil)}
}

func (w *world) server() (*server.Server, error) {
	if w.srv != nil {
		return w.srv, nil
	}
	opts := server.Options{Config: w.cfg, Build: w.build, Health: w.health, Plugins: w.plugins}
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

// registry returns the scenario's plugin registry, creating an unfrozen one on first use.
func (w *world) registry() *plugin.Registry {
	if w.plugins == nil {
		w.plugins = plugin.NewRegistry()
	}
	return w.plugins
}

func (w *world) get(path string, header map[string]string) (*httptest.ResponseRecorder, error) {
	s, err := w.server()
	if err != nil {
		return nil, err
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
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
	sc.Step(`^the plugin registry contains the source "([^"]*)" and the source "([^"]*)"$`, func(a, b string) error {
		if err := w.registry().RegisterSource(plugintest.NewSource(a)); err != nil {
			return err
		}
		return w.registry().RegisterSource(plugintest.NewSource(b))
	})
	sc.Step(`^the plugin registry contains the source "([^"]*)"$`, func(id string) error {
		return w.registry().RegisterSource(plugintest.NewSource(id))
	})
	sc.Step(`^the plugin registry contains the format "([^"]*)"$`, func(id string) error {
		return w.registry().RegisterFormat(plugintest.NewFormat(id))
	})
	sc.Step(`^the plugin registry is frozen$`, func() error { w.registry().Freeze(); return nil })
	sc.Step(`^the plugin registry is not frozen$`, func() error { w.registry(); return nil })
	sc.Step(`^a source "([^"]*)" is registered$`, func(id string) error {
		w.regErr = w.registry().RegisterSource(plugintest.NewSource(id))
		return nil
	})
	sc.Step(`^the registration fails$`, func() error {
		if w.regErr == nil {
			return errors.New("registration succeeded, want failure")
		}
		return nil
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
	sc.Step(`^the JSON field "([^"]*)" should be an empty list$`, func(field string) error {
		v, err := jsonLookup(w.resp.Body.Bytes(), field)
		if err != nil {
			return err
		}
		if l, ok := v.([]any); !ok || len(l) != 0 {
			return fmt.Errorf("%s = %#v, want []", field, v)
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

// jsonLookup resolves a dotted path ("error.code", "sources.0.id") in a JSON document.
// Numeric segments index into arrays.
func jsonLookup(body []byte, path string) (any, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("body is not JSON: %w (%s)", err, body)
	}
	for _, part := range strings.Split(path, ".") {
		switch node := v.(type) {
		case map[string]any:
			var ok bool
			if v, ok = node[part]; !ok {
				return nil, fmt.Errorf("%s: field %q missing in %s", path, part, body)
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("%s: index %q out of range in %s", path, part, body)
			}
			v = node[i]
		default:
			return nil, fmt.Errorf("%s: cannot descend into %q", path, part)
		}
	}
	return v, nil
}

// jsonField resolves a dotted path and returns its string form.
func jsonField(body []byte, path string) (string, error) {
	v, err := jsonLookup(body, path)
	if err != nil {
		return "", err
	}
	if t, ok := v.(string); ok {
		return t, nil
	}
	b, _ := json.Marshal(v)
	return string(b), nil
}
