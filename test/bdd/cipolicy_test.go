package bdd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/cipolicy"
)

type ciPolicyWorld struct {
	workflow string // a synthetic workflow, or "" when dir is set
	dir      string
	findings []cipolicy.Finding
}

func initializeCIPolicyScenario(sc *godog.ScenarioContext) {
	w := &ciPolicyWorld{}

	sc.Step(`^a workflow step that runs:$`, func(doc *godog.DocString) {
		var b strings.Builder
		b.WriteString("jobs:\n  a:\n    steps:\n      - run: |\n")
		for _, l := range strings.Split(doc.Content, "\n") {
			b.WriteString("          " + l + "\n")
		}
		w.workflow = b.String()
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
