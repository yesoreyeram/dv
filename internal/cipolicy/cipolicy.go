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
	// Drop shell comments first: a commented-out command never downloads, binds a hash
	// or verifies anything. Then join line continuations so a command split over lines
	// is seen whole.
	joined := strings.ReplaceAll(stripShellComments(script), "\\\n", " ")
	var out []Finding
	add := func(rule, msg string) {
		out = append(out, Finding{File: name, Line: line, Rule: rule, Message: msg})
	}
	if pipedDownload.MatchString(joined) {
		add(RuleDownloadPiped, "a download is piped straight into a shell or extractor; save it to a file and verify its SHA-256 first")
	}
	for _, msg := range unverifiedDownloads(joined) {
		add(RuleDownloadUnverified, msg)
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

// segmentSep splits a script into commands; pipes stay inside one command so that
// `echo "<sha>  file" | sha256sum -c -` is a single verification command.
var segmentSep = regexp.MustCompile(`\n|;|&&|\|\|`)

// segment is one command of a script with the list operators around it ("" for a line
// break, `;` or the start/end of the script).
type segment struct {
	text, before, after string
}

func splitSegments(script string) []segment {
	idx := segmentSep.FindAllStringIndex(script, -1)
	op := func(k int) string {
		if k < 0 || k >= len(idx) {
			return ""
		}
		if o := script[idx[k][0]:idx[k][1]]; o == "&&" || o == "||" {
			return o
		}
		return ""
	}
	var out []segment
	prev := 0
	for k := 0; k <= len(idx); k++ {
		end := len(script)
		if k < len(idx) {
			end = idx[k][0]
		}
		out = append(out, segment{text: script[prev:end], before: op(k - 1), after: op(k)})
		if k < len(idx) {
			prev = idx[k][1]
		}
	}
	return out
}

// checkCmd is the only accepted verification form (NFR-SEC-015 AC1): the hash line is
// piped straight into the checksum tool reading stdin, as one standalone command, so the
// hash cannot be separated from the check (a manifest file can be swapped or ignored).
var checkCmd = regexp.MustCompile(`^\s*(?:echo|printf)\s([^|<>` + "`" + `$]*)\|\s*(?:sha256sum|shasum\s+-a\s*256)\s+(?:-c|--check)(?:\s+-)?\s*$`)

// verifies reports whether seg is a standalone checksum command whose piped input binds
// file to a committed SHA-256.
func verifies(seg segment, file string) bool {
	if seg.before != "" || seg.after == "||" {
		return false // conditional: `x && check`, `x || check`, `check || true`
	}
	m := checkCmd.FindStringSubmatch(seg.text)
	if m == nil {
		return false
	}
	bind := regexp.MustCompile(`\b[0-9a-f]{64}['"]?\s+['"]?\*?` + regexp.QuoteMeta(file) + `(?:$|[\s"'\\])`)
	return bind.MatchString(m[1])
}

// unverifiedDownloads applies NFR-SEC-015 AC1 per downloaded file: after the download,
// a standalone `echo "<sha256>  <file>" | sha256sum -c -` must come before any other
// command that mentions the file. A piped download has no output file, so it is
// reported here as well as by RuleDownloadPiped.
func unverifiedDownloads(script string) []string {
	segs := splitSegments(script)
	var out []string
	for i, seg := range segs {
		if !downloadCmd.MatchString(seg.text) {
			continue
		}
		file := downloadTarget(seg.text)
		if file == "" {
			out = append(out, "cannot tell which output file a download writes; save it with -o <file> and verify that file's SHA-256 (sha256sum -c)")
			continue
		}
		mention := regexp.MustCompile(`(?:^|[\s"'/=])` + regexp.QuoteMeta(file) + `(?:$|[\s"';|&)\\])`)
		verified := false
		for _, later := range segs[i+1:] {
			if verifies(later, file) {
				verified = true
				break
			}
			if mention.MatchString(later.text) {
				break // used (extracted, run, moved, put in a manifest) before it was verified
			}
		}
		if !verified {
			out = append(out, fmt.Sprintf("downloaded file %q is not verified before it is used; add the standalone command: echo \"<sha256>  %s\" | sha256sum -c -", file, file))
		}
	}
	return out
}

// downloadTarget returns the file a curl/wget command writes, or "" when it writes to
// stdout or cannot be determined.
func downloadTarget(seg string) string {
	f := strings.Fields(seg)
	tool, start := "", -1
	for i, t := range f {
		if t == "curl" || t == "wget" {
			tool, start = t, i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var url, out string
	remote := tool == "wget" // wget saves under the URL's name unless told otherwise
	for i := start + 1; i < len(f); i++ {
		t := unquote(f[i])
		next := func() string {
			if i+1 < len(f) {
				i++
				return unquote(f[i])
			}
			return ""
		}
		switch {
		case t == ">" || t == ">>":
			out = next()
		case strings.HasPrefix(t, ">"):
			out = strings.TrimLeft(t, ">")
		case t == "--output" || t == "--output-document":
			out = next()
		case strings.HasPrefix(t, "--output=") || strings.HasPrefix(t, "--output-document="):
			out = t[strings.Index(t, "=")+1:]
		case t == "--remote-name" || t == "--remote-name-all":
			remote = true
		case strings.Contains(t, "://"):
			if url == "" {
				url = t
			}
		case strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") && len(t) > 1:
			flags := t[1:]
			// Short-option clusters: the value flag is last (-sSfLo file), or its value
			// is attached (-ofile, -O-).
			if tool == "curl" {
				if k := strings.IndexByte(flags, 'o'); k >= 0 {
					if k == len(flags)-1 {
						out = next()
					} else {
						out = flags[k+1:]
					}
				} else if strings.ContainsRune(flags, 'O') {
					remote = true
				}
			} else if k := strings.IndexByte(flags, 'O'); k >= 0 {
				if k == len(flags)-1 {
					out = next()
				} else {
					out = flags[k+1:]
				}
			}
		}
	}
	if out == "-" {
		out = ""
		remote = false
	}
	if out == "" && remote && url != "" {
		u := url
		if q := strings.IndexAny(u, "?#"); q >= 0 {
			u = u[:q]
		}
		out = u[strings.LastIndex(u, "/")+1:]
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
