package bdd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/mockapi"
	"github.com/yesoreyeram/dv/internal/netguard"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/builtin"
	"github.com/yesoreyeram/dv/internal/query"
	"github.com/yesoreyeram/dv/internal/server"
)

// TestConnectorFeatures executes features/connectors end to end: config file with secret
// references → config.Load → query service with the real http source and SSRF guard →
// POST /api/v1/query → a fake vendor API (FR-040, FR-071…FR-073, FR-079, NFR-SEC-005).
func TestConnectorFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "connectors",
		ScenarioInitializer: initializeConnectorScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features/connectors"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("connector feature suite failed")
	}
}

type upstreamRequest struct {
	header http.Header
}

type connectorWorld struct {
	upstream *httptest.Server
	mu       sync.Mutex
	received []upstreamRequest
	allowed  []string
	env      map[string]string
	sources  []map[string]any
	resp     *httptest.ResponseRecorder
	startErr error
}

func (w *connectorWorld) record(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		w.received = append(w.received, upstreamRequest{header: r.Header.Clone()})
		w.mu.Unlock()
		h.ServeHTTP(rw, r)
	})
}

func (w *connectorWorld) addConnection(uid string, auth map[string]any, secrets map[string]any) {
	ds := map[string]any{
		"uid": uid, "type": "http", "name": "Connection " + uid,
		"jsonData": map[string]any{"baseURL": w.upstream.URL + "/api", "allowInsecureHTTP": true, "auth": auth},
	}
	if len(secrets) > 0 {
		ds["secureJsonData"] = secrets
	}
	w.sources = append(w.sources, ds)
}

// start loads the config file through config.Load (so FR-079 references are parsed and
// resolved exactly as in production) and builds the server.
func (w *connectorWorld) start() (*server.Server, error) {
	file, err := json.Marshal(map[string]any{
		"httpAddr": "127.0.0.1:8080", "queryApiEnabled": true, "datasources": w.sources, "outboundAllowedCidrs": w.allowed,
	})
	if err != nil {
		return nil, err
	}
	env := map[string]string{"DV_CONFIG_FILE": "dv.json"}
	for k, v := range w.env {
		env[k] = v
	}
	src := config.Source{
		Getenv: func(k string) string { return env[k] },
		ReadFile: func(p string) ([]byte, error) {
			if p == "dv.json" {
				return file, nil
			}
			return nil, errors.New("no such file")
		},
	}
	cfg, err := config.Load(src)
	if err != nil {
		return nil, err
	}
	allowed, err := netguard.ParseAllowed(cfg.OutboundAllowedCIDRs)
	if err != nil {
		return nil, err
	}
	roots, err := fsroot.Open(nil)
	if err != nil {
		return nil, err
	}
	limits := frame.Limits{MaxRows: cfg.QueryMaxRows, MaxBytes: cfg.QueryMaxBytes}
	reg := plugin.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Roots: roots, FileMaxBytes: cfg.FileMaxBytes, QueryDefaults: limits, Outbound: netguard.Policy{Allowed: allowed}}); err != nil {
		return nil, err
	}
	reg.Freeze()
	var instances []query.Instance
	for _, ds := range cfg.DataSources {
		sec, err := ds.ResolveSecrets(src)
		if err != nil {
			return nil, err
		}
		instances = append(instances, query.Instance{UID: ds.UID, Type: ds.Type, Name: ds.Name, JSONData: ds.JSONData, SecureJSONData: sec})
	}
	svc, err := query.New(context.Background(), query.Options{
		Sources: reg, Instances: instances, Limits: limits, Timeout: cfg.QueryTimeout.Std(),
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		return nil, err
	}
	return server.New(server.Options{Config: cfg, Plugins: reg, Query: svc})
}

func (w *connectorWorld) runQuery(uid, body string, from, to string) error {
	s, err := w.start()
	if err != nil {
		return err
	}
	req := map[string]any{"queries": []map[string]any{{"refId": "A", "datasource": map[string]string{"uid": uid}, "query": json.RawMessage(body)}}}
	if from != "" {
		req["from"], req["to"] = from, to
	}
	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("query body is not JSON: %w", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader(string(b)))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Content-Type", "application/json")
	w.resp = httptest.NewRecorder()
	s.Handler().ServeHTTP(w.resp, r)
	return nil
}

type wireResult struct {
	Status int             `json:"status"`
	Frames []wireFrameJSON `json:"frames"`
	Error  *query.Error    `json:"error"`
}

type wireFrameJSON struct {
	Schema struct {
		Name string `json:"name"`
		Meta struct {
			Truncated bool           `json:"truncated"`
			Notices   []frame.Notice `json:"notices"`
		} `json:"meta"`
		Fields []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	} `json:"schema"`
	Data struct {
		Values [][]any `json:"values"`
	} `json:"data"`
}

func (w *connectorWorld) result() (wireResult, error) {
	var resp struct {
		Results map[string]wireResult `json:"results"`
	}
	if w.resp == nil {
		return wireResult{}, errors.New("no query was run")
	}
	if err := json.Unmarshal(w.resp.Body.Bytes(), &resp); err != nil {
		return wireResult{}, fmt.Errorf("%w: %s", err, w.resp.Body.String())
	}
	r, ok := resp.Results["A"]
	if !ok {
		return wireResult{}, fmt.Errorf("no result A in %s", w.resp.Body.String())
	}
	return r, nil
}

func (w *connectorWorld) frame() (wireFrameJSON, error) {
	r, err := w.result()
	if err != nil {
		return wireFrameJSON{}, err
	}
	if len(r.Frames) != 1 {
		return wireFrameJSON{}, fmt.Errorf("want 1 frame, got %d: %s", len(r.Frames), w.resp.Body.String())
	}
	return r.Frames[0], nil
}

func (w *connectorWorld) countWith(header, value string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, r := range w.received {
		if header == "" || r.header.Get(header) == value {
			n++
		}
	}
	return n
}

func initializeConnectorScenario(sc *godog.ScenarioContext) {
	w := &connectorWorld{env: map[string]string{}}
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if w.upstream != nil {
			w.upstream.Close()
		}
		return ctx, err
	})
	sc.Step(`^a fake upstream API$`, func() error {
		w.upstream = httptest.NewServer(w.record(mockapi.Handler(mockapi.DefaultTokens)))
		return nil
	})
	sc.Step(`^outbound connections to the fake upstream are allowed$`, func() error {
		w.allowed = []string{"127.0.0.1/32"}
		return nil
	})
	sc.Step(`^outbound connections to the fake upstream are not allowed$`, func() error {
		w.allowed = nil
		return nil
	})
	sc.Step(`^an http connection "([^"]*)" to the fake upstream with bearer token from env "([^"]*)" set to "([^"]*)"$`, func(uid, env, val string) error {
		w.env[env] = val
		w.addConnection(uid, map[string]any{"type": "bearer"}, map[string]any{"token": map[string]string{"env": env}})
		return nil
	})
	sc.Step(`^an http connection "([^"]*)" to the fake upstream with API-key headers "([^"]*)" and "([^"]*)"$`, func(uid, h1, h2 string) error {
		w.env["DV_TEST_API_KEY"], w.env["DV_TEST_APP_KEY"] = mockapi.DefaultTokens.Datadog, "app-key"
		w.addConnection(uid,
			map[string]any{"type": "apiKey", "keys": []map[string]string{{"in": "header", "name": h1, "secret": "apiKey"}, {"in": "header", "name": h2, "secret": "appKey"}}},
			map[string]any{"apiKey": map[string]string{"env": "DV_TEST_API_KEY"}, "appKey": map[string]string{"env": "DV_TEST_APP_KEY"}})
		return nil
	})
	sc.Step(`^an http connection "([^"]*)" to the fake upstream with no authentication$`, func(uid string) error {
		w.addConnection(uid, map[string]any{"type": "none"}, nil)
		return nil
	})
	sc.Step(`^I run the http query on "([^"]*)":$`, func(uid string, doc *godog.DocString) error {
		return w.runQuery(uid, doc.Content, "", "")
	})
	sc.Step(`^I run the http query on "([^"]*)" from "([^"]*)" to "([^"]*)":$`, func(uid, from, to string, doc *godog.DocString) error {
		return w.runQuery(uid, doc.Content, from, to)
	})
	sc.Step(`^the query result status is (\d+)$`, func(want int) error {
		r, err := w.result()
		if err != nil {
			return err
		}
		if r.Status != want {
			return fmt.Errorf("status %d, want %d: %s", r.Status, want, w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^the result frame has (\d+) rows$`, func(want int) error {
		f, err := w.frame()
		if err != nil {
			return err
		}
		got := 0
		if len(f.Data.Values) > 0 {
			got = len(f.Data.Values[0])
		}
		if got != want {
			return fmt.Errorf("rows = %d, want %d", got, want)
		}
		return nil
	})
	sc.Step(`^the result frame fields are "([^"]*)"$`, func(want string) error {
		f, err := w.frame()
		if err != nil {
			return err
		}
		var names []string
		for _, fl := range f.Schema.Fields {
			names = append(names, fl.Name)
		}
		if got := strings.Join(names, ", "); got != want {
			return fmt.Errorf("fields = %q, want %q", got, want)
		}
		return nil
	})
	fieldIndex := func(f wireFrameJSON, name string) (int, error) {
		for i, fl := range f.Schema.Fields {
			if fl.Name == name {
				return i, nil
			}
		}
		return 0, fmt.Errorf("no field %q", name)
	}
	sc.Step(`^the result frame field "([^"]*)" has values "([^"]*)"$`, func(name, want string) error {
		f, err := w.frame()
		if err != nil {
			return err
		}
		i, err := fieldIndex(f, name)
		if err != nil {
			return err
		}
		var parts []string
		for _, v := range f.Data.Values[i] {
			if n, ok := v.(float64); ok && f.Schema.Fields[i].Type == "time" {
				parts = append(parts, time.UnixMilli(int64(n)).UTC().Format(time.RFC3339))
				continue
			}
			parts = append(parts, fmt.Sprint(v))
		}
		if got := strings.Join(parts, ", "); got != want {
			return fmt.Errorf("%s = %q, want %q", name, got, want)
		}
		return nil
	})
	sc.Step(`^the result frame field "([^"]*)" has type "([^"]*)"$`, func(name, want string) error {
		f, err := w.frame()
		if err != nil {
			return err
		}
		i, err := fieldIndex(f, name)
		if err != nil {
			return err
		}
		if got := f.Schema.Fields[i].Type; got != want {
			return fmt.Errorf("%s has type %q, want %q", name, got, want)
		}
		return nil
	})
	sc.Step(`^the result frame is truncated with a notice mentioning "([^"]*)"$`, func(want string) error {
		f, err := w.frame()
		if err != nil {
			return err
		}
		if !f.Schema.Meta.Truncated {
			return errors.New("frame is not truncated")
		}
		for _, n := range f.Schema.Meta.Notices {
			if strings.Contains(n.Text, want) {
				return nil
			}
		}
		return fmt.Errorf("no notice mentions %q: %+v", want, f.Schema.Meta.Notices)
	})
	sc.Step(`^the fake upstream received (\d+) requests with "([^"]*)" set to "([^"]*)"$`, func(n int, h, v string) error {
		if got := w.countWith(h, v); got != n || w.countWith("", "") != n {
			return fmt.Errorf("%d of %d requests had %s=%q, want %d of %d", got, w.countWith("", ""), h, v, n, n)
		}
		return nil
	})
	sc.Step(`^the fake upstream received (\d+) requests$`, func(n int) error {
		if got := w.countWith("", ""); got != n {
			return fmt.Errorf("upstream received %d requests, want %d", got, n)
		}
		return nil
	})
	sc.Step(`^the response body does not contain "([^"]*)"$`, func(s string) error {
		if strings.Contains(w.resp.Body.String(), s) {
			return fmt.Errorf("response contains %q", s)
		}
		return nil
	})
	sc.Step(`^the query error code is "([^"]*)"$`, func(want string) error {
		r, err := w.result()
		if err != nil {
			return err
		}
		if r.Error == nil || r.Error.Code != want {
			return fmt.Errorf("error = %+v, want code %q", r.Error, want)
		}
		return nil
	})
	sc.Step(`^the query error mentions "([^"]*)"$`, func(want string) error {
		r, err := w.result()
		if err != nil {
			return err
		}
		if r.Error == nil || !strings.Contains(r.Error.Message, want) {
			return fmt.Errorf("error = %+v, want a message mentioning %q", r.Error, want)
		}
		return nil
	})
	sc.Step(`^the server starts with an http connection whose secureJsonData token is the plain string "([^"]*)"$`, func(secret string) error {
		w.sources = append(w.sources, map[string]any{
			"uid": "plain", "type": "http", "name": "Plain",
			"jsonData":       map[string]any{"baseURL": "https://api.example.com", "auth": map[string]string{"type": "bearer"}},
			"secureJsonData": map[string]any{"token": secret},
		})
		_, w.startErr = w.start()
		return nil
	})
	sc.Step(`^start-up fails with an error mentioning "([^"]*)"$`, func(want string) error {
		if w.startErr == nil || !strings.Contains(w.startErr.Error(), want) {
			return fmt.Errorf("start-up error = %w, want one mentioning %q", w.startErr, want)
		}
		return nil
	})
	sc.Step(`^the start-up error does not contain "([^"]*)"$`, func(s string) error {
		if w.startErr != nil && strings.Contains(w.startErr.Error(), s) {
			return fmt.Errorf("start-up error reveals %q: %w", s, w.startErr)
		}
		return nil
	})
}
