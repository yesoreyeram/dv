package bdd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// toolchainFiles are the declarations scripts/check-toolchain.sh reads (NFR-MAINT-007).
var toolchainFiles = []string{
	"mise.toml",
	"go.mod",
	"package.json",
	"deploy/Dockerfile",
	".github/workflows/ci.yml",
	"scripts/check-toolchain.sh",
}

type toolchainWorld struct {
	dir    string
	output string
	err    error
}

func initializeToolchainScenario(sc *godog.ScenarioContext) {
	w := &toolchainWorld{}
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if w.dir != "" {
			_ = os.RemoveAll(w.dir)
		}
		return ctx, err
	})

	sc.Step(`^a copy of the repository's toolchain declarations$`, func() error {
		dir, err := os.MkdirTemp("", "dv-toolchain-*")
		if err != nil {
			return err
		}
		w.dir = dir
		for _, f := range toolchainFiles {
			b, err := os.ReadFile(filepath.Join("..", "..", f))
			if err != nil {
				return err
			}
			dst := filepath.Join(dir, f)
			if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
				return err
			}
			mode := os.FileMode(0o600)
			if strings.HasSuffix(f, ".sh") {
				mode = 0o700
			}
			if err := os.WriteFile(dst, b, mode); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^in "([^"]*)" the text '([^']*)' is replaced with '([^']*)'$`, func(file, from, to string) error {
		vars, err := w.pins()
		if err != nil {
			return err
		}
		from, to = expand(from, vars), expand(to, vars)
		p := filepath.Join(w.dir, file)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Contains(b, []byte(from)) {
			return fmt.Errorf("%s does not contain %q, so the scenario would test nothing", file, from)
		}
		return os.WriteFile(p, bytes.ReplaceAll(b, []byte(from), []byte(to)), 0o600)
	})

	sc.Step(`^the toolchain check runs$`, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "bash", filepath.Join(w.dir, "scripts", "check-toolchain.sh"))
		cmd.Dir = w.dir
		out, err := cmd.CombinedOutput()
		w.output, w.err = string(out), err
		return nil
	})

	sc.Step(`^the toolchain check passes$`, func() error {
		if w.err != nil {
			return fmt.Errorf("check failed (%w):\n%s", w.err, w.output)
		}
		return nil
	})
	sc.Step(`^the toolchain check fails$`, func() error {
		if w.err == nil {
			return fmt.Errorf("check passed but should have failed:\n%s", w.output)
		}
		return nil
	})
	sc.Step(`^every Go, pnpm and golangci-lint pin is bumped by one patch version$`, func() error {
		vars, err := w.pins()
		if err != nil {
			return err
		}
		edits := []struct{ file, from, to string }{
			{"mise.toml", `go = "{go}"`, `go = "` + nextPatch(vars["go"]) + `"`},
			{"go.mod", "go {go}\n", "go " + nextPatch(vars["go"]) + "\n"},
			{"mise.toml", `pnpm = "{pnpm}"`, `pnpm = "` + nextPatch(vars["pnpm"]) + `"`},
			{"package.json", `"pnpm@{pnpm}"`, `"pnpm@` + nextPatch(vars["pnpm"]) + `"`},
			{"deploy/Dockerfile", "pnpm@{pnpm}", "pnpm@" + nextPatch(vars["pnpm"])},
			{"mise.toml", `golangci-lint = "{golangci-lint}"`, `golangci-lint = "` + nextPatch(vars["golangci-lint"]) + `"`},
			{".github/workflows/ci.yml", "golangci-lint@v{golangci-lint}", "golangci-lint@v" + nextPatch(vars["golangci-lint"])},
		}
		for _, e := range edits {
			p := filepath.Join(w.dir, e.file)
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			from := expand(e.from, vars)
			if !bytes.Contains(b, []byte(from)) {
				return fmt.Errorf("%s does not contain %q", e.file, from)
			}
			if err := os.WriteFile(p, bytes.ReplaceAll(b, []byte(from), []byte(e.to)), 0o600); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the toolchain check output mentions "([^"]*)"$`, func(text string) error {
		vars, err := w.pins()
		if err != nil {
			return err
		}
		text = expand(text, vars)
		if !strings.Contains(w.output, text) {
			return fmt.Errorf("output does not mention %q:\n%s", text, w.output)
		}
		return nil
	})

	sc.Step(`^"([^"]*)" pins Node major (\d+)$`, func(file string, major int) error {
		return w.match(file, `(?m)^node = "(\d+)\.`, fmt.Sprint(major), 1)
	})
	sc.Step(`^every "node-version" in "([^"]*)" is (\d+)$`, func(file string, major int) error {
		return w.match(file, `(?m)^\s*node-version: (\S+)$`, fmt.Sprint(major), 1)
	})
	sc.Step(`^"([^"]*)" builds the web assets with an LTS Node image not older than the pin$`, func(file string) error {
		vars, err := w.pins()
		if err != nil {
			return err
		}
		img, _ := strconv.Atoi(vars["docker.node"])
		pin, _ := strconv.Atoi(vars["node.major"])
		if img%2 != 0 || img < pin {
			return fmt.Errorf("%s builds with node:%s-alpine; want an even major >= %d", file, vars["docker.node"], pin)
		}
		return nil
	})
}

// pins reads the copied mise.toml into the {name} placeholders the feature uses.
func (w *toolchainWorld) pins() (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(w.dir, "mise.toml"))
	if err != nil {
		return nil, err
	}
	vars := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^([a-z-]+)\s*=\s*"([^"]+)"`).FindAllStringSubmatch(string(b), -1) {
		vars[m[1]] = m[2]
	}
	for _, k := range []string{"go", "node", "pnpm", "golangci-lint"} {
		if vars[k] == "" {
			return nil, fmt.Errorf("mise.toml has no %s pin", k)
		}
	}
	major, err := strconv.Atoi(strings.SplitN(vars["node"], ".", 2)[0])
	if err != nil {
		return nil, fmt.Errorf("node pin %q: %w", vars["node"], err)
	}
	vars["node.major"] = strconv.Itoa(major)
	vars["node.major+1"] = strconv.Itoa(major + 1)
	vars["node.major+2"] = strconv.Itoa(major + 2)
	vars["node.major-2"] = strconv.Itoa(major - 2)

	// Builder images are read from the Dockerfile itself: Dependabot may move them ahead
	// of the pins (NFR-MAINT-007 AC2).
	df, err := os.ReadFile(filepath.Join(w.dir, "deploy", "Dockerfile"))
	if err != nil {
		return nil, err
	}
	gm := regexp.MustCompile(`(?m)^FROM golang:(\d+\.\d+)-alpine`).FindSubmatch(df)
	nm := regexp.MustCompile(`(?m)^FROM node:(\d+)-alpine`).FindSubmatch(df)
	if gm == nil || nm == nil {
		return nil, fmt.Errorf("deploy/Dockerfile: cannot find the golang and node builder images")
	}
	vars["docker.go"] = string(gm[1])
	vars["docker.node"] = string(nm[1])
	dn, _ := strconv.Atoi(vars["docker.node"])
	vars["docker.node+2"] = strconv.Itoa(dn + 2)
	return vars, nil
}

// expand replaces {name} placeholders with values from vars; unknown names are kept so a
// typo makes the scenario's text search fail loudly.
func expand(s string, vars map[string]string) string {
	return regexp.MustCompile(`\{([a-z.+-]+[0-9]*)\}`).ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := vars[m[1:len(m)-1]]; ok {
			return v
		}
		return m
	})
}

// nextPatch returns X.Y.(Z+1) for X.Y.Z.
func nextPatch(v string) string {
	i := strings.LastIndex(v, ".")
	n, err := strconv.Atoi(v[i+1:])
	if i < 0 || err != nil {
		return v + ".1"
	}
	return v[:i+1] + strconv.Itoa(n+1)
}

// match requires at least min matches of re's first group in file, all equal to want.
func (w *toolchainWorld) match(file, re, want string, minimum int) error {
	b, err := os.ReadFile(filepath.Join(w.dir, file))
	if err != nil {
		return err
	}
	m := regexp.MustCompile(re).FindAllStringSubmatch(string(b), -1)
	if len(m) < minimum {
		return fmt.Errorf("%s: %d matches for %s, want at least %d", file, len(m), re, minimum)
	}
	for _, g := range m {
		if g[1] != want {
			return fmt.Errorf("%s: found %q, want %q", file, g[1], want)
		}
	}
	return nil
}
