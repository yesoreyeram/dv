// Package cipolicy checks GitHub Actions workflow files against the CI supply-chain
// policy (NFR-SEC-015): downloads are verified against a committed SHA-256, Go tools are
// pinned to an exact version, and action references name a tag or a commit.
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

	downloadCmd = regexp.MustCompile(`(?:^|[\s;&|(])(curl|wget)\s`)
	// A download whose output goes through a pipe into a shell or an extractor.
	pipedDownload = regexp.MustCompile(`(?:^|[\s;&(])(?:curl|wget)\s[^|\n]*\|\s*(?:sudo\s+)?(?:sh|bash|zsh|dash|tar|python3?|perl|unzip|gunzip|xz)\b`)
	hashCheck     = regexp.MustCompile(`\b(?:sha256sum|shasum\s+-a\s*256)\b[^\n]*(?:\s-c\b|--check\b)`)
	hashLiteral   = regexp.MustCompile(`\b[0-9a-f]{64}\b`)

	goTool    = regexp.MustCompile(`\bgo\s+(?:run|install)\s+((?:-\S+\s+)*)(\S+)`)
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
		// Block content must be indented deeper than the `run` key itself; a sibling key
		// such as `env:` (same column as `run`) ends the block.
		indent := strings.Index(line, "run:")
		rest := strings.TrimSpace(m[2])
		var script string
		if strings.HasPrefix(rest, "|") || strings.HasPrefix(rest, ">") {
			var body []string
			j := i + 1
			for ; j < len(lines); j++ {
				l := lines[j]
				if strings.TrimSpace(l) == "" {
					body = append(body, "")
					continue
				}
				if leading(l) <= indent {
					break
				}
				body = append(body, l)
			}
			script = strings.Join(body, "\n")
			out = append(out, CheckScript(name, i+1, script)...)
			i = j - 1
			continue
		}
		script = unquote(rest)
		out = append(out, CheckScript(name, i+1, script)...)
	}
	return out
}

// CheckScript checks the shell script of a single `run:` step that starts at line.
func CheckScript(name string, line int, script string) []Finding {
	// Join shell line continuations so a command split over lines is seen whole.
	joined := strings.ReplaceAll(script, "\\\n", " ")
	var out []Finding
	add := func(rule, msg string) {
		out = append(out, Finding{File: name, Line: line, Rule: rule, Message: msg})
	}
	if pipedDownload.MatchString(joined) {
		add(RuleDownloadPiped, "a download is piped straight into a shell or extractor; save it to a file and verify its SHA-256 first")
	}
	if downloadCmd.MatchString(joined) && (!hashCheck.MatchString(joined) || !hashLiteral.MatchString(joined)) {
		add(RuleDownloadUnverified, "the step downloads a file but does not check it against a committed SHA-256 (sha256sum -c)")
	}
	for _, m := range goTool.FindAllStringSubmatch(joined, -1) {
		target := unquote(m[2])
		if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "$") {
			continue // local package or a path inside the checkout
		}
		at := strings.LastIndex(target, "@")
		if at < 0 {
			continue // resolved through this module's go.mod/go.sum
		}
		if v := target[at+1:]; !exactSemv.MatchString(v) {
			add(RuleGoToolUnpinned, fmt.Sprintf("%s is not pinned to an exact version (use @vX.Y.Z)", target))
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
