package cipolicy

import (
	"os"
	"path/filepath"
	"reflect"
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
	U, P := RuleDownloadUnverified, RuleDownloadPiped
	cases := []struct {
		name   string
		script string
		want   []string
	}{
		// The verifying helper is the only accepted way to download (NFR-SEC-015 AC1).
		{"helper", "scripts/fetch-verified.sh https://x/t.tgz " + goodHash + " t.tgz\ntar -xzf t.tgz", nil},
		{"helper ./ and quotes", "./scripts/fetch-verified.sh \"https://x/${V}/t.tgz\" '" + goodHash + "' t.tgz && tar -xzf t.tgz", nil},
		{"helper continuation", "scripts/fetch-verified.sh \\\n  https://x/t.tgz \\\n  " + goodHash + " \\\n  t.tgz", nil},
		{"helper in if is fine", "if true; then scripts/fetch-verified.sh https://x/t " + goodHash + " t; fi", nil},
		{"helper variable hash", "scripts/fetch-verified.sh https://x/t \"$SUM\" t", []string{U}},
		{"helper short hash", "scripts/fetch-verified.sh https://x/t abc123 t", []string{U}},
		{"helper uppercase hash", "scripts/fetch-verified.sh https://x/t " + strings.ToUpper(goodHash) + " t", []string{U}},
		{"helper missing args", "scripts/fetch-verified.sh https://x/t", []string{U}},
		{"helper extra args", "scripts/fetch-verified.sh https://x/t " + goodHash + " t extra", []string{U}},
		{"helper http", "scripts/fetch-verified.sh http://x/t " + goodHash + " t", []string{U}},
		// Every other downloader is refused, however it is verified or wrapped.
		{"curl with manual check", "curl -sSfLo t.tgz https://x/t.tgz\necho \"" + goodHash + "  t.tgz\" | sha256sum -c -\ntar -xzf t.tgz", []string{U}},
		{"wget", "wget -qO t.tgz https://x/t.tgz", []string{U}},
		{"curl path", "/usr/bin/curl -fLo t https://x/t", []string{U}},
		{"command curl", "command curl -fLo t https://x/t", []string{U}},
		{"aria2c", "aria2c https://x/t", []string{U}},
		{"gh release download", "gh release download v1 -R o/r -p t", []string{U}},
		{"curl in heredoc", "cat <<'END'\ncurl -fLo tool https://x/tool\nEND\n./tool", []string{U}},
		{"curl in if false", "if false; then\n  curl -fLo t https://x/t\nfi", []string{U}},
		{"two downloads", "curl -fLo a https://x/a\nwget https://x/b", []string{U, U}},
		{"pipe to tar", "curl -sSfL https://x/t.tgz | tar -xz t", []string{P, U}},
		{"pipe to bash", "curl -fsSL https://x/i.sh | bash", []string{P, U}},
		{"pipe to sudo sh", "wget -qO- https://x/i.sh | sudo sh", []string{P, U}},
		{"continuation pipe", "curl -sSfL \\\n  https://x/t.tgz \\\n  | tar -xz", []string{P, U}},
		// Shell comments are not commands.
		{"commented-out download", "# curl -fsSL https://x/i.sh | bash", nil},
		{"trailing comment", "echo hi # curl https://x", nil},
		{"hash in quotes is not a comment", "echo 'a #curl'", []string{U}},
		{"word curl in text", "echo curly && ./curlish && make curl-free", nil},
		// Go tools.
		{"go run latest", "go run golang.org/x/vuln/cmd/govulncheck@latest ./...", []string{RuleGoToolUnpinned}},
		{"go install branch", "go install github.com/x/y@main", []string{RuleGoToolUnpinned}},
		{"go install minor only", "go install github.com/x/y@v1.2", []string{RuleGoToolUnpinned}},
		{"go install flags", "go install -v -trimpath github.com/x/y@latest", []string{RuleGoToolUnpinned}},
		// Flags with separate values must not hide the package (Codex review on PR #14).
		{"go run -tags value", "go run -tags netgo golang.org/x/vuln/cmd/govulncheck@latest ./...", []string{RuleGoToolUnpinned}},
		{"go run quoted ldflags", "go run -ldflags \"-s -w\" -mod mod golang.org/x/vuln/cmd/govulncheck@latest ./...", []string{RuleGoToolUnpinned}},
		{"go install -C and two packages", "go install -C /tmp github.com/x/a@v1.2.3 github.com/x/b@latest", []string{RuleGoToolUnpinned}},
		{"go install empty version", "go install github.com/x/a@", []string{RuleGoToolUnpinned}},
		{"go run -tags pinned", "go run -tags netgo golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...", nil},
		{"go -C before run", "go -C . run golang.org/x/vuln/cmd/govulncheck@latest ./...", []string{RuleGoToolUnpinned}},
		{"go -C= before install", "go -C=tools install github.com/example/tool@main", []string{RuleGoToolUnpinned}},
		{"go -C quoted var before run", `go -C "$GITHUB_WORKSPACE" run x.org/y/tool@latest`, []string{RuleGoToolUnpinned}},
		{"go -C pinned", "go -C tools run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...", nil},
		{"go test -run is not go run", "go test -run TestX example.com/a/b@latest", nil},
		{"go -C dir test", "go -C run test ./...", nil},
		{"bare go", "go", nil},
		{"go -C quoted dir with space", `go -C "tools dir" run golang.org/x/vuln/cmd/govulncheck@latest ./...`, []string{RuleGoToolUnpinned}},
		{"go -C single-quoted separator", `go -C 'a;b' run golang.org/x/vuln/cmd/govulncheck@latest ./...`, []string{RuleGoToolUnpinned}},
		{"go -C escaped space", `go -C a\ b install github.com/example/tool@main`, []string{RuleGoToolUnpinned}},
		{"go run in command substitution", `v=$(go run golang.org/x/vuln/cmd/govulncheck@latest)`, []string{RuleGoToolUnpinned}},
		{"go run in backticks", "v=`go run golang.org/x/vuln/cmd/govulncheck@latest`", []string{RuleGoToolUnpinned}},
		{"quoted package", `go run "golang.org/x/vuln/cmd/govulncheck@latest"`, []string{RuleGoToolUnpinned}},
		{"go -C quoted dir pinned", `go -C "tools dir" run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...`, nil},
		{"echo mentioning go run is a separate word list", `echo "go run x.org/y/z@latest"`, nil},
		{"cargo is not go", "cargo run x.org/y/z@latest", nil},
		{"go run ldflags with @", "go run -ldflags=-X=main.by=ci@example.com ./cmd/dv", nil},
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

// YAML folding (Codex review on PR #14): what Actions executes is checked.
func TestCheckWorkflowFoldedScalars(t *testing.T) {
	cases := []struct {
		name, yaml string
		want       []string
	}{
		{"folded block", "jobs:\n  a:\n    steps:\n      - run: >-\n          go run\n          golang.org/x/vuln/cmd/govulncheck@latest ./...\n", []string{RuleGoToolUnpinned}},
		{"folded keep", "jobs:\n  a:\n    steps:\n      - run: >+\n          go install\n          github.com/x/y@main\n\n", []string{RuleGoToolUnpinned}},
		{"plain multi-line", "jobs:\n  a:\n    steps:\n      - run: go run\n          golang.org/x/vuln/cmd/govulncheck@latest\n        env:\n          A: b\n", []string{RuleGoToolUnpinned}},
		{"quoted multi-line", "jobs:\n  a:\n    steps:\n      - run: 'go install\n          github.com/x/y@main'\n", []string{RuleGoToolUnpinned}},
		{"literal keeps lines", "jobs:\n  a:\n    steps:\n      - run: |\n          go run ./cmd/dv\n          echo golang.org/x/vuln/cmd/govulncheck@latest\n", nil},
		{"folded both readings dedupe", "jobs:\n  a:\n    steps:\n      - run: >\n          go run golang.org/x/vuln/cmd/govulncheck@latest\n", []string{RuleGoToolUnpinned}},
		{"plain pinned with comment", "jobs:\n  a:\n    steps:\n      - run: go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 # pinned\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rules(CheckWorkflow("w.yml", []byte(tc.yaml)))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
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

func TestShellCommands(t *testing.T) {
	cases := []struct {
		in   string
		want [][]string
	}{
		{`a b; c`, [][]string{{"a", "b"}, {"c"}}},
		{`a "b c" 'd;e' f\ g`, [][]string{{"a", "b c", "d;e", "f g"}}},
		{`a "x\"y" "" b`, [][]string{{"a", `x"y`, "", "b"}}},
		{"a | b && c\nd", [][]string{{"a"}, {"b"}, {"c"}, {"d"}}},
		{"x=$(go run p) `q`", [][]string{{"x=$"}, {"go", "run", "p"}, {"q"}}},
		{`"unterminated`, [][]string{{"unterminated"}}},
		{`trailing\`, [][]string{{"trailing"}}},
	}
	for _, tc := range cases {
		got := shellCommands(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("shellCommands(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
