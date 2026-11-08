package bdd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
			return fmt.Errorf("check failed (%v):\n%s", w.err, w.output)
		}
		return nil
	})
	sc.Step(`^the toolchain check fails$`, func() error {
		if w.err == nil {
			return fmt.Errorf("check passed but should have failed:\n%s", w.output)
		}
		return nil
	})
	sc.Step(`^the toolchain check output mentions "([^"]*)"$`, func(text string) error {
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
	sc.Step(`^"([^"]*)" builds the web assets with "([^"]*)"$`, func(file, image string) error {
		return w.match(file, `(?m)^FROM (node:\S+) AS web$`, image, 1)
	})
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
