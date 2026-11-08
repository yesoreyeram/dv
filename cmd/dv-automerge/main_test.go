package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestConfigFrom(t *testing.T) {
	ok := map[string]string{"GITHUB_REPOSITORY": "o/r", "DV_AUTOMERGE_AUTHORS": "a,b"}
	r, err := configFrom(env(ok))
	if err != nil || r.API != "https://api.github.com" || r.Policy.DefaultBranch != "main" || len(r.Policy.Authors) != 2 || r.DryRun {
		t.Fatalf("defaults: %+v %v", r, err)
	}
	for name, m := range map[string]map[string]string{
		"GITHUB_REPOSITORY":    {"DV_AUTOMERGE_AUTHORS": "a"},
		"DV_AUTOMERGE_AUTHORS": {"GITHUB_REPOSITORY": "o/r", "DV_AUTOMERGE_AUTHORS": " , "},
		"DV_AUTOMERGE_METHOD":  {"GITHUB_REPOSITORY": "o/r", "DV_AUTOMERGE_AUTHORS": "a", "DV_AUTOMERGE_METHOD": "octopus"},
	} {
		if _, err := configFrom(env(m)); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: want error naming it, got %v", name, err)
		}
	}
	full := map[string]string{
		"GITHUB_REPOSITORY": "o/r", "DV_AUTOMERGE_AUTHORS": "a", "GITHUB_API_URL": "http://x",
		"DV_AUTOMERGE_DEFAULT_BRANCH": "trunk", "DV_AUTOMERGE_METHOD": "merge", "DV_AUTOMERGE_DRY_RUN": "true",
	}
	r, err = configFrom(env(full))
	if err != nil || r.API != "http://x" || r.Policy.DefaultBranch != "trunk" || r.MergeMethod != "merge" || !r.DryRun {
		t.Fatalf("overrides: %+v %v", r, err)
	}
}

func TestRunWithEventFile(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	p := filepath.Join(dir, "event.json")
	// A push run with no pull requests: nothing to do, no network.
	if err := os.WriteFile(p, []byte(`{"workflow_run":{"conclusion":"success","event":"push","pull_requests":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := map[string]string{"GITHUB_REPOSITORY": "o/r", "DV_AUTOMERGE_AUTHORS": "a", "GITHUB_EVENT_PATH": p}
	if err := run(log, env(base)); err != nil {
		t.Fatal(err)
	}
	base["GITHUB_EVENT_PATH"] = filepath.Join(dir, "missing.json")
	if err := run(log, env(base)); err == nil {
		t.Fatal("want open error")
	}
	if err := os.WriteFile(p, []byte(`[`), 0o600); err != nil {
		t.Fatal(err)
	}
	base["GITHUB_EVENT_PATH"] = p
	if err := run(log, env(base)); err == nil {
		t.Fatal("want decode error")
	}
	if err := run(log, env(map[string]string{})); err == nil {
		t.Fatal("want config error")
	}
}
