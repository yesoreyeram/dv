package bdd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/builtin"
	"github.com/yesoreyeram/dv/internal/query"
	"github.com/yesoreyeram/dv/internal/server"
)

// TestQueryFeatures executes features/query against the real server, built-in plugins
// and query service (FR-016, FR-017, NFR-SEC-016).
func TestQueryFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "query",
		ScenarioInitializer: initializeQueryScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features/query"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("query feature suite failed")
	}
}

type queryWorld struct {
	base      string            // temp dir holding the roots
	roots     map[string]string // root name -> dir
	instances []query.Instance
	cfg       config.Config
	srv       *server.Server
	close     func()
	resp      *httptest.ResponseRecorder
	headers   map[string]string // extra request headers; "Host" overrides the loopback default

	env     map[string]string
	files   map[string]string
	loaded  config.Config
	loadErr error
}

func (w *queryWorld) server() (*server.Server, error) {
	if w.srv != nil {
		return w.srv, nil
	}
	roots, err := fsroot.Open(w.roots)
	if err != nil {
		return nil, err
	}
	w.close = func() { _ = roots.Close() }
	limits := frame.Limits{MaxRows: w.cfg.QueryMaxRows, MaxBytes: w.cfg.QueryMaxBytes}
	reg := plugin.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Roots: roots, FileMaxBytes: w.cfg.FileMaxBytes, QueryDefaults: limits}); err != nil {
		return nil, err
	}
	reg.Freeze()
	svc, err := query.New(context.Background(), query.Options{
		Sources: reg, Instances: w.instances, Limits: limits, Timeout: w.cfg.QueryTimeout.Std(),
	})
	if err != nil {
		return nil, err
	}
	s, err := server.New(server.Options{Config: w.cfg, Plugins: reg, Query: svc})
	if err != nil {
		return nil, err
	}
	w.srv = s
	return s, nil
}

func (w *queryWorld) do(method, path, ctype, body string) error {
	s, err := w.server()
	if err != nil {
		return err
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// httptest defaults Host to example.com; a browser on a loopback bind sends the bind
	// address (NFR-SEC-017).
	req.Host = "127.0.0.1:8080"
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range w.headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	w.resp = httptest.NewRecorder()
	s.Handler().ServeHTTP(w.resp, req)
	return nil
}

func (w *queryWorld) send(queries []map[string]any) error {
	b, err := json.Marshal(map[string]any{"queries": queries})
	if err != nil {
		return err
	}
	return w.do(http.MethodPost, "/api/v1/query", "application/json", string(b))
}

func fileQuery(ref, uid string, body any) map[string]any {
	return map[string]any{"refId": ref, "datasource": map[string]string{"uid": uid}, "query": body}
}

func (w *queryWorld) result(ref string) (query.Result, error) {
	var resp struct {
		Results map[string]struct {
			Status     int               `json:"status"`
			DurationMs *int64            `json:"durationMs"`
			Frames     []json.RawMessage `json:"frames"`
			Error      *query.Error      `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.resp.Body.Bytes(), &resp); err != nil {
		return query.Result{}, fmt.Errorf("%w: %s", err, w.resp.Body.String())
	}
	r, ok := resp.Results[ref]
	if !ok {
		return query.Result{}, fmt.Errorf("no result %q in %s", ref, w.resp.Body.String())
	}
	if r.DurationMs == nil {
		return query.Result{}, fmt.Errorf("result %q has no durationMs (FR-016 AC10): %s", ref, w.resp.Body.String())
	}
	out := query.Result{Status: r.Status, DurationMs: *r.DurationMs, Error: r.Error}
	for _, raw := range r.Frames {
		var f frame.Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			return out, fmt.Errorf("frame: %w", err)
		}
		out.Frames = append(out.Frames, &f)
	}
	return out, nil
}

func (w *queryWorld) frame(ref string, i int) (*frame.Frame, error) {
	r, err := w.result(ref)
	if err != nil {
		return nil, err
	}
	if i >= len(r.Frames) {
		return nil, fmt.Errorf("result %s has %d frames", ref, len(r.Frames))
	}
	return r.Frames[i], nil
}

func (w *queryWorld) writeFile(root, name, content string) error {
	dir, ok := w.roots[root]
	if !ok {
		return fmt.Errorf("unknown root %q", root)
	}
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o600)
}

func initializeQueryScenario(sc *godog.ScenarioContext) {
	w := &queryWorld{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		base, err := os.MkdirTemp("", "dv-query-bdd-")
		if err != nil {
			return ctx, err
		}
		cfg := config.Default()
		cfg.HTTPAddr = "127.0.0.1:0"
		*w = queryWorld{base: base, roots: map[string]string{}, cfg: cfg, env: map[string]string{}, files: map[string]string{}}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if w.close != nil {
			w.close()
		}
		return ctx, os.RemoveAll(w.base)
	})

	// --- query.feature ---
	sc.Step(`^a file root "([^"]*)" containing "([^"]*)":$`, func(root, name string, doc *godog.DocString) error {
		dir := filepath.Join(w.base, root)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		w.roots[root] = dir
		return w.writeFile(root, name, doc.Content+"\n")
	})
	sc.Step(`^a provisioned data source "([^"]*)" of type "([^"]*)" named "([^"]*)" with root "([^"]*)"$`, func(uid, typ, name, root string) error {
		w.instances = append(w.instances, query.Instance{UID: uid, Type: typ, Name: name, JSONData: json.RawMessage(fmt.Sprintf(`{"root":%q}`, root))})
		return nil
	})
	sc.Step(`^the query API is enabled$`, func() error { w.cfg.QueryAPIEnabled = true; return nil })
	sc.Step(`^the query API is enabled with the remote opt-in$`, func() error {
		w.cfg.QueryAPIEnabled, w.cfg.QueryAPIAllowRemote = true, true
		return nil
	})
	sc.Step(`^the request header "([^"]*)" is "([^"]*)"$`, func(k, v string) error {
		if w.headers == nil {
			w.headers = map[string]string{}
		}
		w.headers[k] = v
		return nil
	})
	sc.Step(`^the file "([^"]*)" in root "([^"]*)" contains "([^"]*)"$`, w.writeFileStep)
	sc.Step(`^the file size limit is (\d+) bytes$`, func(n int) error { w.cfg.FileMaxBytes = int64(n); return nil })
	sc.Step(`^the host query row limit is (\d+)$`, func(n int) error { w.cfg.QueryMaxRows = n; return nil })
	sc.Step(`^I query "([^"]*)" with refId "([^"]*)" and path "([^"]*)"$`, func(uid, ref, path string) error {
		return w.send([]map[string]any{fileQuery(ref, uid, map[string]string{"path": path})})
	})
	sc.Step(`^I query "([^"]*)" with refId "([^"]*)" and path "([^"]*)" and maxRows (\d+)$`, func(uid, ref, path string, n int) error {
		q := fileQuery(ref, uid, map[string]string{"path": path})
		q["maxRows"] = n
		return w.send([]map[string]any{q})
	})
	sc.Step(`^I query "([^"]*)" with refId "([^"]*)" and the query body (.+)$`, func(uid, ref, body string) error {
		return w.send([]map[string]any{fileQuery(ref, uid, json.RawMessage(body))})
	})
	sc.Step(`^I send the queries:$`, func(t *godog.Table) error {
		var qs []map[string]any
		for _, row := range t.Rows[1:] {
			qs = append(qs, fileQuery(row.Cells[0].Value, row.Cells[1].Value, map[string]string{"path": row.Cells[2].Value}))
		}
		return w.send(qs)
	})
	sc.Step(`^I send (\d+) queries to "([^"]*)" for path "([^"]*)"$`, func(n int, uid, path string) error {
		qs := make([]map[string]any, n)
		for i := range qs {
			qs[i] = fileQuery(fmt.Sprintf("Q%d", i), uid, map[string]string{"path": path})
		}
		return w.send(qs)
	})
	sc.Step(`^I POST to "([^"]*)" with content type "([^"]*)" and body:$`, func(path, ctype string, doc *godog.DocString) error {
		return w.do(http.MethodPost, path, ctype, doc.Content)
	})
	sc.Step(`^I send a (GET|POST) request to "([^"]*)"$`, func(method, path string) error {
		ctype := ""
		if method == http.MethodPost {
			ctype = "application/json"
		}
		return w.do(method, path, ctype, "{}")
	})
	sc.Step(`^the response status should be (\d+)$`, func(code int) error {
		if w.resp.Code != code {
			return fmt.Errorf("status = %d, want %d (body %s)", w.resp.Code, code, w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^result "([^"]*)" has status (\d+) and (\d+) frames? with (\d+) rows$`, func(ref string, status, frames, rows int) error {
		r, err := w.result(ref)
		if err != nil {
			return err
		}
		if r.Status != status || len(r.Frames) != frames || r.Error != nil {
			return fmt.Errorf("result %s: status %d, %d frames, error %+v; body %s", ref, r.Status, len(r.Frames), r.Error, w.resp.Body.String())
		}
		if got := r.Frames[0].Rows(); got != rows {
			return fmt.Errorf("result %s: %d rows, want %d", ref, got, rows)
		}
		return nil
	})
	sc.Step(`^result "([^"]*)" has status (\d+) and error code "([^"]*)"$`, func(ref string, status int, code string) error {
		r, err := w.result(ref)
		if err != nil {
			return err
		}
		if r.Status != status || r.Error == nil || r.Error.Code != code || r.Error.Message == "" || r.Frames != nil {
			return fmt.Errorf("result %s: %+v %+v; body %s", ref, r, r.Error, w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^result "([^"]*)" reports a durationMs of at least (\d+)$`, func(ref string, min int64) error {
		r, err := w.result(ref)
		if err != nil {
			return err
		}
		if r.DurationMs < min {
			return fmt.Errorf("result %s durationMs = %d, want >= %d", ref, r.DurationMs, min)
		}
		return nil
	})
	sc.Step(`^result "([^"]*)" frame (\d+) meta "executedQuery" is "([^"]*)"$`, func(ref string, i int, want string) error {
		f, err := w.frame(ref, i)
		if err != nil {
			return err
		}
		if f.Meta.ExecutedQuery != want {
			return fmt.Errorf("executedQuery = %q, want %q", f.Meta.ExecutedQuery, want)
		}
		return nil
	})
	sc.Step(`^a file "([^"]*)" in root "([^"]*)" with (\d+) rows$`, func(name, root string, n int) error {
		var b strings.Builder
		b.WriteString("id,label\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "%d,item-%04d\n", i, i)
		}
		return w.writeFile(root, name, b.String())
	})
	sc.Step(`^the host query byte limit is (\d+) bytes$`, func(n int) error { w.cfg.QueryMaxBytes = int64(n); return nil })
	sc.Step(`^result "([^"]*)" frame (\d+) has a notice mentioning "([^"]*)"$`, func(ref string, i int, text string) error {
		f, err := w.frame(ref, i)
		if err != nil {
			return err
		}
		for _, n := range f.Meta.Notices {
			if strings.Contains(n.Text, text) {
				return nil
			}
		}
		return fmt.Errorf("no notice mentioning %q: %+v", text, f.Meta.Notices)
	})
	sc.Step(`^the frames of the response hold at most (\d+) bytes$`, func(limit int64) error {
		var resp struct {
			Results map[string]json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal(w.resp.Body.Bytes(), &resp); err != nil {
			return err
		}
		var total int64
		for ref := range resp.Results {
			r, err := w.result(ref)
			if err != nil {
				return err
			}
			for _, f := range r.Frames {
				total += f.EstimatedBytes()
			}
		}
		if total == 0 || total > limit {
			return fmt.Errorf("frames hold %d bytes, want 1..%d", total, limit)
		}
		return nil
	})
	sc.Step(`^result "([^"]*)" frame (\d+) is marked truncated$`, func(ref string, i int) error {
		f, err := w.frame(ref, i)
		if err != nil {
			return err
		}
		if !f.Meta.Truncated {
			return fmt.Errorf("frame not truncated: %s", w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^the response body does not contain the root directory$`, func() error {
		if strings.Contains(w.resp.Body.String(), w.base) {
			return fmt.Errorf("body reveals %s: %s", w.base, w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^the response body does not contain "([^"]*)"$`, func(s string) error {
		if strings.Contains(w.resp.Body.String(), s) {
			return fmt.Errorf("body contains %q: %s", s, w.resp.Body.String())
		}
		return nil
	})
	sc.Step(`^the JSON field "([^"]*)" should be "([^"]*)"$`, func(field, want string) error {
		got, err := jsonField(w.resp.Body.Bytes(), field)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s = %q, want %q (body %s)", field, got, want, w.resp.Body.String())
		}
		return nil
	})

	// --- gate.feature ---
	sc.Step(`^the environment is empty$`, func() error { return nil })
	sc.Step(`^the environment variable "([^"]*)" is "([^"]*)"$`, func(k, v string) error { w.env[k] = v; return nil })
	sc.Step(`^the config file contains:$`, func(doc *godog.DocString) error {
		w.env["DV_CONFIG_FILE"] = "dv.json"
		w.files["dv.json"] = doc.Content
		return nil
	})
	sc.Step(`^the configuration is loaded$`, func() error {
		w.loaded, w.loadErr = config.Load(config.Source{
			Getenv: func(k string) string { return w.env[k] },
			ReadFile: func(p string) ([]byte, error) {
				if v, ok := w.files[p]; ok {
					return []byte(v), nil
				}
				return nil, os.ErrNotExist
			},
		})
		return nil
	})
	sc.Step(`^the configuration is valid$`, func() error { return w.loadErr })
	sc.Step(`^the configuration is rejected mentioning "([^"]*)"$`, func(s string) error {
		if w.loadErr == nil {
			return fmt.Errorf("configuration accepted, want an error mentioning %q", s)
		}
		if !strings.Contains(w.loadErr.Error(), s) {
			return fmt.Errorf("error does not mention %q: %w", s, w.loadErr)
		}
		return nil
	})
	sc.Step(`^the query API is off$`, func() error {
		if w.loaded.QueryAPIEnabled || w.loaded.QueryAPIAllowRemote {
			return fmt.Errorf("query API on by default: %+v", w.loaded)
		}
		return nil
	})
}

func (w *queryWorld) writeFileStep(name, root, content string) error {
	return w.writeFile(root, name, content)
}
