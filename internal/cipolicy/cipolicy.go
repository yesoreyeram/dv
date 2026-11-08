// Package cipolicy checks GitHub Actions workflow files against the CI supply-chain
// policy (NFR-SEC-015): files are downloaded only through scripts/fetch-verified.sh with
// a committed SHA-256, Go tools are pinned to an exact version, and action references
// name a tag or a commit.
//
// The checker is deliberately line based instead of a full YAML parser: it only needs
// `run:` scripts and `uses:` values, works on the exact text a reviewer sees, and adds
// no dependency. It errs on the side of reporting.
package cipolicy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Rule identifiers, stable so tests and docs can refer to them.
const (
	RuleDownloadUnverified = "download-unverified"
	RuleDownloadPiped      = "download-piped"
	RuleGoToolUnpinned     = "go-tool-unpinned"
	RuleActionUnpinned     = "action-unpinned"
)

// Finding is one policy violation.
type Finding struct {
	File    string
	Line    int // 1-based line of the `run:` or `uses:` key
	Rule    string
	Message string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s", f.File, f.Line, f.Rule, f.Message)
}

var (
	runKey  = regexp.MustCompile(`^(\s*)(?:-\s+)?run:\s*(.*)$`)
	usesKey = regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(.*)$`)

	// Any downloader other than scripts/fetch-verified.sh (NFR-SEC-015 AC1). Matched as a
	// word anywhere outside comments, so paths (/usr/bin/curl), `command curl` and text in
	// heredocs are all caught; a false positive is preferred over a missed download.
	downloadCmd = regexp.MustCompile(`(?:^|[^\w.-])(curl|wget|aria2c)(?:$|[^\w.-])|\bgh\s+release\s+download\b`)
	// A download whose output goes through a pipe into a shell or an extractor.
	pipedDownload = regexp.MustCompile(`(?:^|[^\w.-])(?:curl|wget)\s[^|\n]*\|\s*(?:sudo\s+)?(?:sh|bash|zsh|dash|tar|python3?|perl|unzip|gunzip|xz)\b`)
	// A call of the verifying helper and its arguments (up to the end of the command).
	helperCall = regexp.MustCompile(`(?:^|[\s;&|(])(?:\./)?scripts/fetch-verified\.sh\b([^;&|\n]*)`)
	httpsArg   = regexp.MustCompile(`^https://\S+$`)
	sha256Arg  = regexp.MustCompile(`^[0-9a-f]{64}$`)

	// A `go run`/`go install` command and all its arguments (up to the end of the command).
	// Flags are not parsed: every module/path@version argument is checked instead, so a
	// value-taking flag (-tags netgo, -C dir) cannot hide the package (NFR-SEC-015 AC2).
	goTool    = regexp.MustCompile(`\bgo\s+(?:run|install)\b([^;&|\n]*)`)
	exactSemv = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

	actionTag = regexp.MustCompile(`^v\d+(?:\.\d+){0,2}$`)
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	imageDig  = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
)

// CheckWorkflow checks one workflow file's contents. name is used in findings only.
func CheckWorkflow(name string, src []byte) []Finding {
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	var out []Finding
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := usesKey.FindStringSubmatch(line); m != nil {
			if f, bad := checkUses(unquote(stripComment(m[1]))); bad {
				out = append(out, Finding{File: name, Line: i + 1, Rule: RuleActionUnpinned, Message: f})
			}
			continue
		}
		m := runKey.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// Continuation lines must be indented deeper than the `run` key itself; a sibling
		// key such as `env:` (same column as `run`) ends the value.
		indent := strings.Index(line, "run:")
		rest := strings.TrimSpace(m[2])
		keyLine := i + 1
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) != "" && leading(l) <= indent {
				break
			}
			body = append(body, l)
		}
		i = j - 1
		// Each reading Actions may execute is checked, and a finding from any of them
		// counts (NFR-SEC-015 AC5): literal lines for `|`, lines joined by spaces for a
		// folded `>` block and for multi-line plain or quoted scalars.
		var readings []string
		switch {
		case strings.HasPrefix(rest, "|"):
			readings = []string{strings.Join(body, "\n")}
		case strings.HasPrefix(rest, ">"):
			readings = []string{strings.Join(body, "\n"), foldLines(body)}
		default:
			readings = []string{unquote(strings.TrimSpace(stripComment(rest) + " " + foldLines(body)))}
		}
		out = append(out, dedupe(readings, func(script string) []Finding { return CheckScript(name, keyLine, script) })...)
	}
	return out
}

// CheckScript checks the shell script of a single `run:` step that starts at line.
func CheckScript(name string, line int, script string) []Finding {
	// Drop shell comments first: a commented-out command never downloads anything. Then
	// join line continuations so a command split over lines is seen whole.
	joined := strings.ReplaceAll(stripShellComments(script), "\\\n", " ")
	var out []Finding
	add := func(rule, msg string) {
		out = append(out, Finding{File: name, Line: line, Rule: rule, Message: msg})
	}
	if pipedDownload.MatchString(joined) {
		add(RuleDownloadPiped, "a download is piped straight into a shell or extractor; use scripts/fetch-verified.sh <https-url> <sha256> <output>")
	}
	for _, m := range downloadCmd.FindAllStringSubmatch(joined, -1) {
		tool := m[1]
		if tool == "" {
			tool = "gh release download"
		}
		add(RuleDownloadUnverified, tool+" is not allowed in workflows; download with scripts/fetch-verified.sh <https-url> <sha256> <output>, which verifies before writing")
	}
	for _, m := range helperCall.FindAllStringSubmatch(joined, -1) {
		args := strings.Fields(m[1])
		for i := range args {
			args[i] = unquote(args[i])
		}
		if len(args) != 3 || !httpsArg.MatchString(args[0]) || !sha256Arg.MatchString(args[1]) {
			add(RuleDownloadUnverified, fmt.Sprintf("scripts/fetch-verified.sh needs <https-url> <sha256> <output> with the SHA-256 as a committed 64-hex literal, got %q", strings.TrimSpace(m[1])))
		}
	}
	for _, m := range goTool.FindAllStringSubmatch(joined, -1) {
		for _, tok := range strings.Fields(m[1]) {
			target := unquote(strings.Trim(tok, `"'`))
			if strings.HasPrefix(target, "-") || strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "$") {
				continue // a flag, a local package or a path inside the checkout
			}
			at := strings.LastIndex(target, "@")
			if at < 0 || !strings.Contains(target[:at], "/") {
				continue // no version: resolved through this module's go.mod/go.sum
			}
			if v := target[at+1:]; !exactSemv.MatchString(v) {
				add(RuleGoToolUnpinned, fmt.Sprintf("%s is not pinned to an exact version (use @vX.Y.Z)", target))
			}
		}
	}
	return out
}

func checkUses(ref string) (string, bool) {
	switch {
	case ref == "":
		return "empty uses:", true
	case strings.HasPrefix(ref, "./"):
		return "", false
	case strings.HasPrefix(ref, "docker://"):
		if imageDig.MatchString(ref) {
			return "", false
		}
		return ref + " has no @sha256: digest", true
	}
	at := strings.LastIndex(ref, "@")
	if at < 0 {
		return ref + " has no version; pin a tag (@vN) or a commit SHA", true
	}
	v := ref[at+1:]
	if actionTag.MatchString(v) || commitSHA.MatchString(v) {
		return "", false
	}
	return ref + " does not name a version tag or a commit SHA", true
}

// CheckDir checks every *.yml / *.yaml file directly inside dir, in name order.
func CheckDir(dir string) ([]Finding, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	var names []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.Type().IsRegular() && (ext == ".yml" || ext == ".yaml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []Finding
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n)) // #nosec G304 -- dir is a repo path chosen by the caller
		if err != nil {
			return nil, 0, err
		}
		out = append(out, CheckWorkflow(n, b)...)
	}
	return out, len(names), nil
}

// stripShellComments removes `#` comments from each line of a shell script. A `#`
// starts a comment only at the start of a word (line start or after whitespace) and
// outside single or double quotes, as in POSIX sh; `$#`, `${#v}` and URL fragments stay.
func stripShellComments(script string) string {
	lines := strings.Split(script, "\n")
	for i, l := range lines {
		var quote byte
		for j := 0; j < len(l); j++ {
			c := l[j]
			switch {
			case quote != 0:
				if c == '\\' && quote == '"' && j+1 < len(l) {
					j++
				} else if c == quote {
					quote = 0
				}
			case c == '\\':
				j++
			case c == '\'' || c == '"':
				quote = c
			case c == '#' && (j == 0 || l[j-1] == ' ' || l[j-1] == '\t'):
				lines[i] = strings.TrimRight(l[:j], " \t")
				j = len(l)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// foldLines joins the non-blank lines of a YAML scalar with single spaces, as YAML
// folding does for lines at the same indentation. It is used as an extra reading, so
// over-joining (more-indented lines YAML would keep) can only add findings.
func foldLines(lines []string) string {
	var parts []string
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// dedupe runs check on every reading and returns each distinct finding once.
func dedupe(readings []string, check func(string) []Finding) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, r := range readings {
		for _, f := range check(r) {
			if k := f.Rule + "\x00" + f.Message; !seen[k] {
				seen[k] = true
				out = append(out, f)
			}
		}
	}
	return out
}

func leading(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

func stripComment(s string) string {
	if i := strings.Index(s, " #"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
