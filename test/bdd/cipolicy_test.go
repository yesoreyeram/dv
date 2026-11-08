package bdd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/cipolicy"
)

// fetchWorld drives scripts/fetch-verified.sh against a local HTTPS server.
type fetchWorld struct {
	srv  *httptest.Server
	ca   string
	body string
	dir  string
	out  string
	err  error
}

func (f *fetchWorld) close() {
	if f.srv != nil {
		f.srv.Close()
	}
	for _, p := range []string{f.ca, f.dir} {
		if p != "" {
			_ = os.RemoveAll(p)
		}
	}
}

func (f *fetchWorld) fetch(url, hash, out string) error {
	dir, err := os.MkdirTemp("", "dv-fetch-*")
	if err != nil {
		return err
	}
	f.dir = dir
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "fetch-verified.sh"))
	if err != nil {
		return err
	}
	cmd := exec.Command("bash", script, url, hash, out) // #nosec G204 -- test step
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CURL_CA_BUNDLE="+f.ca)
	b, err := cmd.CombinedOutput()
	f.out, f.err = string(b), err
	return nil
}

func (f *fetchWorld) hashFor(which string) (string, error) {
	h := sha256.Sum256([]byte(f.body))
	good := hex.EncodeToString(h[:])
	switch which {
	case "the SHA-256 of the content":
		return good, nil
	case "a different SHA-256":
		o := sha256.Sum256([]byte(f.body + "x"))
		return hex.EncodeToString(o[:]), nil
	case "a malformed SHA-256":
		return good[:20], nil
	}
	return "", fmt.Errorf("unknown hash %q", which)
}

type ciPolicyWorld struct {
	workflow string // a synthetic workflow, or "" when dir is set
	dir      string
	findings []cipolicy.Finding
}

func initializeCIPolicyScenario(sc *godog.ScenarioContext) {
	w := &ciPolicyWorld{}

	f := &fetchWorld{}
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		f.close()
		return ctx, err
	})
	sc.Step(`^an HTTPS server that serves "([^"]*)" at "([^"]*)"$`, func(body, path string) error {
		f.body = body
		f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != path {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(body))
		}))
		ca, err := os.CreateTemp("", "dv-ca-*.pem")
		if err != nil {
			return err
		}
		f.ca = ca.Name()
		if err := pem.Encode(ca, &pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw}); err != nil {
			return err
		}
		return ca.Close()
	})
	sc.Step(`^the helper fetches "([^"]*)" with (the SHA-256 of the content|a different SHA-256|a malformed SHA-256) into "([^"]*)"$`, func(path, which, out string) error {
		hash, err := f.hashFor(which)
		if err != nil {
			return err
		}
		return f.fetch(f.srv.URL+path, hash, out)
	})
	sc.Step(`^the helper fetches the plain-HTTP URL of "([^"]*)" with (the SHA-256 of the content) into "([^"]*)"$`, func(path, which, out string) error {
		hash, err := f.hashFor(which)
		if err != nil {
			return err
		}
		return f.fetch(strings.Replace(f.srv.URL, "https://", "http://", 1)+path, hash, out)
	})
	sc.Step(`^the helper succeeds and "([^"]*)" holds the content$`, func(out string) error {
		if f.err != nil {
			return fmt.Errorf("helper failed (%w): %s", f.err, f.out)
		}
		b, err := os.ReadFile(filepath.Join(f.dir, out))
		if err != nil {
			return fmt.Errorf("read %s: %w", out, err)
		}
		if string(b) != f.body {
			return fmt.Errorf("%s = %q, want %q", out, b, f.body)
		}
		return nil
	})
	sc.Step(`^the helper fails and leaves no "([^"]*)" or temporary file$`, func(out string) error {
		if f.err == nil {
			return fmt.Errorf("helper succeeded but should have failed: %s", f.out)
		}
		entries, err := os.ReadDir(f.dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Name() == out || strings.HasPrefix(e.Name(), ".fetch-verified.") {
				return fmt.Errorf("left behind %s after a failed fetch: %s", e.Name(), f.out)
			}
		}
		return nil
	})

	sc.Step(`^a workflow step that runs:$`, func(doc *godog.DocString) {
		var b strings.Builder
		b.WriteString("jobs:\n  a:\n    steps:\n      - run: |\n")
		for _, l := range strings.Split(doc.Content, "\n") {
			b.WriteString("          " + l + "\n")
		}
		w.workflow = b.String()
	})
	sc.Step(`^a workflow file:$`, func(doc *godog.DocString) {
		w.workflow = doc.Content + "\n"
	})
	sc.Step(`^a workflow step that uses "([^"]*)"$`, func(ref string) {
		w.workflow = "jobs:\n  a:\n    steps:\n      - uses: " + ref + "\n"
	})
	sc.Step(`^the workflows in "([^"]*)"$`, func(dir string) {
		w.dir = filepath.Join("..", "..", filepath.FromSlash(dir))
	})
	sc.Step(`^the CI supply-chain policy checks the workflow$`, func() error {
		if w.dir != "" {
			f, n, err := cipolicy.CheckDir(w.dir)
			if err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("no workflow files in %s", w.dir)
			}
			w.findings = f
			return nil
		}
		w.findings = cipolicy.CheckWorkflow("scenario.yml", []byte(w.workflow))
		return nil
	})
	sc.Step(`^the workflow passes the policy$`, func() error {
		if len(w.findings) != 0 {
			return fmt.Errorf("unexpected findings: %v", w.findings)
		}
		return nil
	})
	reports := func(rule string) error {
		for _, f := range w.findings {
			if f.Rule == rule {
				return nil
			}
		}
		return fmt.Errorf("no %q finding in %v", rule, w.findings)
	}
	sc.Step(`^the policy reports "([^"]*)"$`, reports)
	sc.Step(`^the finding mentions "([^"]*)"$`, func(text string) error {
		for _, f := range w.findings {
			if strings.Contains(f.Message, text) {
				return nil
			}
		}
		return fmt.Errorf("no finding mentions %q: %v", text, w.findings)
	})
	sc.Step(`^the result for the action is "([^"]*)"$`, func(want string) error {
		if want == "pass" {
			if len(w.findings) != 0 {
				return fmt.Errorf("unexpected findings: %v", w.findings)
			}
			return nil
		}
		return reports(want)
	})
}
