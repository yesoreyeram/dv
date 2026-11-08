package cipolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodHash = "a65b5253807a68ac0cafa4414031fd740aeb55f54fb7e55f386acb52e6a840eb"

func rules(fs []Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func TestCheckScript(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   []string
	}{
		{"verified download", "curl -sSfLo t.tgz https://x/t.tgz\necho \"" + goodHash + "  t.tgz\" | sha256sum -c -\ntar -xzf t.tgz", nil},
		{"verified with --check", "wget -qO t.tgz https://x/t.tgz\necho '" + goodHash + "  t.tgz' | sha256sum --check", nil},
		{"verified with shasum", "curl -fLo t.tgz https://x\necho '" + goodHash + "  t.tgz' | shasum -a 256 -c", nil},
		{"no check", "curl -sSfLo t.tgz https://x/t.tgz\ntar -xzf t.tgz", []string{RuleDownloadUnverified}},
		{"check without committed hash", "curl -fLO https://x/t.tgz\ncurl -fLO https://x/sums.txt\nsha256sum -c sums.txt", []string{RuleDownloadUnverified, RuleDownloadUnverified}},
		{"hash literal but no check", "curl -fLo t https://x\n# " + goodHash, []string{RuleDownloadUnverified}},
		{"pipe to tar", "curl -sSfL https://x/t.tgz | tar -xz t", []string{RuleDownloadPiped, RuleDownloadUnverified}},
		{"pipe to bash", "curl -fsSL https://x/i.sh | bash", []string{RuleDownloadPiped, RuleDownloadUnverified}},
		{"pipe to sudo sh", "wget -qO- https://x/i.sh | sudo sh", []string{RuleDownloadPiped, RuleDownloadUnverified}},
		{"continuation", "curl -sSfL \\\n  https://x/t.tgz \\\n  | tar -xz", []string{RuleDownloadPiped, RuleDownloadUnverified}},
		{"pipe after verify still flagged", "curl -fL https://x | tar -xz\necho '" + goodHash + "  t' | sha256sum -c", []string{RuleDownloadPiped, RuleDownloadUnverified}},
		// Per-file verification (Codex review on PR #14).
		{"second download unverified", "curl -sSfLo a.tgz https://x/a.tgz\ncurl -sSfLo b.tgz https://x/b.tgz\necho '" + goodHash + "  a.tgz' | sha256sum -c -\ntar -xzf b.tgz", []string{RuleDownloadUnverified}},
		{"used before verified", "curl -sSfLo a.tgz https://x/a.tgz && tar -xzf a.tgz && echo '" + goodHash + "  a.tgz' | sha256sum -c -", []string{RuleDownloadUnverified}},
		{"check before binding", "curl -sSfLo a.tgz https://x/a.tgz; sha256sum -c sums; echo '" + goodHash + "  a.tgz' > sums", []string{RuleDownloadUnverified}},
		{"hash for another file", "curl -sSfL https://x/tool > tool\necho '" + goodHash + "  other' | sha256sum -c -", []string{RuleDownloadUnverified}},
		{"stdout only", "curl -sSf https://x/tool", []string{RuleDownloadUnverified}},
		{"both verified via sums file", "curl -fLo a.tgz https://x/a.tgz\nwget -q https://x/dl/b.zip?raw=1\nprintf '%s  %s\\n' " + goodHash + " a.tgz > sums\necho '" + goodHash + " *b.zip' >> sums\nsha256sum -c sums\ntar -xzf a.tgz && unzip b.zip", nil},
		{"curl -O remote name", "curl -fsSLO https://x/v1/t.tgz\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"curl --remote-name", "curl -fsSL --remote-name https://x/t.tgz\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"curl --output=", "curl -fsSL --output=t.tgz https://x/z\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"curl --output", "curl -fsSL --output t.tgz https://x/z\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"curl attached -o", "curl -fsSLot.tgz https://x/z\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"curl redirect attached", "curl -fsSL https://x/z >t.tgz\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"wget -O- redirected", "wget -qO- https://x/z > t.tgz\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"wget --output-document=", "wget -q --output-document=t.tgz https://x/z\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		{"wget attached -O", "wget -qOt.tgz https://x/z\necho '" + goodHash + "  t.tgz' | sha256sum -c -", nil},
		// Shell comments never verify (Codex review on PR #14).
		{"commented-out check", "curl -fLo tool https://x/tool\n# echo '" + goodHash + "  tool' | sha256sum -c -\n./tool", []string{RuleDownloadUnverified}},
		{"trailing comment check", "curl -fLo tool https://x/tool # echo '" + goodHash + "  tool' | sha256sum -c -\n./tool", []string{RuleDownloadUnverified}},
		{"commented-out download", "# curl -fsSL https://x/i.sh | bash", nil},
		{"hash inside quotes is not a comment", "curl -fLo t https://x/t#frag\necho \"" + goodHash + "  t\" | sha256sum -c - # verified\n./t", nil},
		{"dollar hash kept", "curl -fLo t https://x/t\necho \"$#\" '#x' \"a\\\"#b\" && echo '" + goodHash + "  t' | sha256sum -c -", nil},
		{"dangling -o", "curl -fsSL https://x/z -o", []string{RuleDownloadUnverified}},
		{"word curl in text", "echo curly && ./curlish", nil},
		{"go run latest", "go run golang.org/x/vuln/cmd/govulncheck@latest ./...", []string{RuleGoToolUnpinned}},
		{"go install branch", "go install github.com/x/y@main", []string{RuleGoToolUnpinned}},
		{"go install minor only", "go install github.com/x/y@v1.2", []string{RuleGoToolUnpinned}},
		{"go install flags", "go install -v -trimpath github.com/x/y@latest", []string{RuleGoToolUnpinned}},
		{"go run pinned", "go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...", nil},
		{"go run prerelease", "go run example.com/t@v0.0.0-20240101000000-abcdefabcdef", nil},
		{"go run local", "go run ./cmd/dv-automerge", nil},
		{"go run from go.mod", "go run github.com/cucumber/godog/cmd/godog", nil},
		{"go test is not a tool", "go test -race ./...", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rules(CheckScript("w.yml", 1, tc.script))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckUses(t *testing.T) {
	cases := map[string]bool{ // ref -> bad
		"actions/checkout@v4":     false,
		"actions/checkout@v4.2":   false,
		"actions/checkout@v4.2.2": false,
		"actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683": false,
		"./.github/actions/x":                false,
		"docker://alpine@sha256:" + goodHash: false,
		"actions/checkout@main":              true,
		"actions/checkout@master":            true,
		"actions/checkout":                   true,
		"actions/checkout@11bd71":            true,
		"docker://alpine:3.20":               true,
		"":                                   true,
	}
	for ref, bad := range cases {
		if _, got := checkUses(ref); got != bad {
			t.Errorf("checkUses(%q) bad=%v, want %v", ref, got, bad)
		}
	}
}

func TestCheckWorkflowBlocksAndLines(t *testing.T) {
	src := `name: x
on: [push]
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      # - uses: actions/checkout@main   (commented out, ignored)
      - uses: actions/checkout@main # trailing comment
      - uses: "actions/setup-go@v5"
      - name: one-liner
        run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
      - name: block
        run: |
          echo start

          curl -sSfL https://x/t.tgz | tar -xz
        env:
          A: curl https://not-a-script-line
      - run: >-
          go install github.com/x/y@v1.0.0
      - run: 'curl -fsSL https://x/i.sh | sh'
`
	got := CheckWorkflow("ci.yml", []byte(src))
	want := []struct {
		line int
		rule string
	}{
		{8, RuleActionUnpinned},
		{11, RuleGoToolUnpinned},
		{13, RuleDownloadPiped},
		{13, RuleDownloadUnverified},
		{21, RuleDownloadPiped},
		{21, RuleDownloadUnverified},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d findings %v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].Line != w.line || got[i].Rule != w.rule || got[i].File != "ci.yml" {
			t.Errorf("finding %d = %s, want line %d %s", i, got[i], w.line, w.rule)
		}
	}
	if s := got[0].String(); !strings.HasPrefix(s, "ci.yml:8: [action-unpinned] ") {
		t.Errorf("String() = %q", s)
	}
}

func TestCheckWorkflowCRLF(t *testing.T) {
	src := "steps:\r\n  - run: |\r\n      curl -fL https://x | sh\r\n"
	if got := rules(CheckWorkflow("w.yml", []byte(src))); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestCheckDir(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("steps:\n  - uses: a/b@main\n"), 0o600))
	must(os.WriteFile(filepath.Join(dir, "a.yml"), []byte("steps:\n  - run: go install x.y/z@latest\n"), 0o600))
	must(os.WriteFile(filepath.Join(dir, "README.md"), []byte("- uses: a/b@main\n"), 0o600))
	must(os.Mkdir(filepath.Join(dir, "sub.yml"), 0o700))
	got, n, err := CheckDir(dir)
	must(err)
	if n != 2 || len(got) != 2 || got[0].File != "a.yml" || got[1].File != "b.yaml" {
		t.Fatalf("n=%d findings=%v", n, got)
	}
	if _, _, err := CheckDir(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

// TestRepositoryWorkflows is the enforcement point (NFR-SEC-015 AC5): it fails CI when a
// workflow in this repository breaks the policy.
func TestRepositoryWorkflows(t *testing.T) {
	got, n, err := CheckDir(filepath.Join("..", "..", ".github", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no workflow files found")
	}
	for _, f := range got {
		t.Error(f)
	}
}
