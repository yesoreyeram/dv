package cipolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fetchServer serves body at /tool over HTTPS for tests of scripts/fetch-verified.sh. It
// returns the server and a CA bundle path that curl trusts via CURL_CA_BUNDLE.
func fetchServer(t testing.TB, body string) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tool" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, ca
}

// runFetch runs the helper in dir and returns its combined output and error.
func runFetch(script, dir, ca string, args ...string) (string, error) {
	cmd := exec.Command("bash", append([]string{script}, args...)...) // #nosec G204 -- test helper
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CURL_CA_BUNDLE="+ca)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestFetchVerified(t *testing.T) {
	script, err := filepath.Abs("../../scripts/fetch-verified.sh")
	if err != nil {
		t.Fatal(err)
	}
	const body = "release bytes"
	srv, ca := fetchServer(t, body)
	other := sum("something else")

	cases := []struct {
		name    string
		args    func(dir string) []string
		wantOK  bool
		wantMsg string
	}{
		{"match", func(string) []string { return []string{srv.URL + "/tool", sum(body), "tool"} }, true, "fetch-verified: tool"},
		{"mismatch", func(string) []string { return []string{srv.URL + "/tool", other, "tool"} }, false, "SHA-256 mismatch"},
		{"malformed hash", func(string) []string { return []string{srv.URL + "/tool", "abc", "tool"} }, false, "64-character"},
		{"uppercase hash", func(string) []string { return []string{srv.URL + "/tool", strings.ToUpper(sum(body)), "tool"} }, false, "64-character"},
		{"plain http", func(string) []string {
			return []string{strings.Replace(srv.URL, "https://", "http://", 1) + "/tool", sum(body), "tool"}
		}, false, "non-HTTPS"},
		{"not found", func(string) []string { return []string{srv.URL + "/missing", sum(body), "tool"} }, false, "404"},
		{"usage", func(string) []string { return []string{srv.URL + "/tool"} }, false, "usage"},
		{"existing output", func(dir string) []string {
			_ = os.WriteFile(filepath.Join(dir, "tool"), []byte("old"), 0o600)
			return []string{srv.URL + "/tool", sum(body), "tool"}
		}, false, "refusing to overwrite"},
		{"directory output", func(string) []string { return []string{srv.URL + "/tool", sum(body), "out/"} }, false, "file path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, err := runFetch(script, dir, ca, tc.args(dir)...)
			if (err == nil) != tc.wantOK || !strings.Contains(out, tc.wantMsg) {
				t.Fatalf("err=%v, output %q; want ok=%v and %q", err, out, tc.wantOK, tc.wantMsg)
			}
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".fetch-verified.") {
					t.Fatalf("temporary file left behind: %s", e.Name())
				}
			}
			got, readErr := os.ReadFile(filepath.Join(dir, "tool"))
			switch {
			case tc.wantOK && string(got) != body:
				t.Fatalf("tool = %q, %v; want the served content", got, readErr)
			case !tc.wantOK && tc.name != "existing output" && readErr == nil:
				t.Fatalf("tool was written although the fetch failed: %q", got)
			case tc.name == "existing output" && string(got) != "old":
				t.Fatalf("existing file was changed: %q", got)
			}
		})
	}
}
