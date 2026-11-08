package bdd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/yesoreyeram/dv/internal/automerge"
)

// TestRepoAutomationFeatures executes features/repo against the merge bot's decision
// (NFR-MAINT-006 AC6).
func TestRepoAutomationFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name:                "repo",
		ScenarioInitializer: initializeAutomergeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features/repo"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("repo feature suite failed")
	}
}

type mergeWorld struct {
	policy   automerge.Policy
	run      automerge.Run
	pr       automerge.PullRequest
	checks   []automerge.CheckRun
	decision automerge.Decision
}

func initializeAutomergeScenario(sc *godog.ScenarioContext) {
	w := &mergeWorld{}
	setCheck := func(name, status, conclusion string) {
		for i := range w.checks {
			if w.checks[i].Name == name {
				w.checks[i].Status, w.checks[i].Conclusion = status, conclusion
				return
			}
		}
		w.checks = append(w.checks, automerge.CheckRun{Name: name, Status: status, Conclusion: conclusion})
	}

	sc.Step(`^the repository "([^"]*)" with default branch "([^"]*)"$`, func(repo, branch string) {
		w.policy.Repo, w.policy.DefaultBranch = repo, branch
	})
	sc.Step(`^the allowed authors are "([^"]*)"$`, func(list string) {
		w.policy.Authors = automerge.ParseAuthors(list)
	})
	sc.Step(`^an open pull request #(\d+) by "([^"]*)" from "([^"]*)" into "([^"]*)" at commit "([^"]*)"$`,
		func(n int, author, headRepo, base, sha string) {
			yes := true
			w.pr = automerge.PullRequest{Number: n, State: "open", Author: author, HeadRepo: headRepo, BaseRef: base, HeadSHA: sha, Mergeable: &yes}
		})
	sc.Step(`^the CI run for commit "([^"]*)" concluded "([^"]*)" on a "([^"]*)" event$`, func(sha, concl, event string) {
		w.run = automerge.Run{HeadSHA: sha, Conclusion: concl, Event: event}
	})
	sc.Step(`^every check run on the commit succeeded$`, func() {
		w.checks = []automerge.CheckRun{
			{Name: "go", Status: "completed", Conclusion: "success"},
			{Name: "web", Status: "completed", Conclusion: "success"},
		}
	})
	sc.Step(`^the CI run concluded "([^"]*)"$`, func(c string) { w.run.Conclusion = c })
	sc.Step(`^the CI run was triggered by a "([^"]*)" event$`, func(e string) { w.run.Event = e })
	sc.Step(`^the pull request is a draft$`, func() { w.pr.Draft = true })
	sc.Step(`^the pull request is closed$`, func() { w.pr.State = "closed" })
	sc.Step(`^the pull request comes from the fork "([^"]*)"$`, func(r string) { w.pr.HeadRepo = r })
	sc.Step(`^the pull request is authored by "([^"]*)"$`, func(a string) { w.pr.Author = a })
	sc.Step(`^the pull request targets "([^"]*)"$`, func(b string) { w.pr.BaseRef = b })
	sc.Step(`^the pull request has the label "([^"]*)"$`, func(l string) { w.pr.Labels = append(w.pr.Labels, l) })
	sc.Step(`^a newer commit "([^"]*)" was pushed to the pull request$`, func(sha string) { w.pr.HeadSHA = sha })
	sc.Step(`^the pull request has merge conflicts$`, func() { no := false; w.pr.Mergeable = &no })
	sc.Step(`^GitHub has not computed mergeability yet$`, func() { w.pr.Mergeable = nil })
	sc.Step(`^the check run "([^"]*)" concluded "([^"]*)"$`, func(n, c string) { setCheck(n, "completed", c) })
	sc.Step(`^the check run "([^"]*)" is still "([^"]*)"$`, func(n, s string) { setCheck(n, s, "") })
	sc.Step(`^there are no check runs on the commit$`, func() { w.checks = nil })

	sc.Step(`^the merge bot decides$`, func() {
		w.decision = automerge.Decide(w.policy, w.run, w.pr, w.checks)
	})
	sc.Step(`^the pull request is merged$`, func() error {
		if !w.decision.Merge {
			return fmt.Errorf("expected a merge, got skip: %s", w.decision.Reason)
		}
		return nil
	})
	sc.Step(`^the pull request is not merged$`, func() error {
		if w.decision.Merge {
			return fmt.Errorf("expected a skip, got merge")
		}
		return nil
	})
	sc.Step(`^the merge is pinned to commit "([^"]*)"$`, func(sha string) error {
		if w.decision.SHA != sha {
			return fmt.Errorf("pinned to %q, want %q", w.decision.SHA, sha)
		}
		return nil
	})
	sc.Step(`^the reason mentions "([^"]*)"$`, func(s string) error {
		if !strings.Contains(w.decision.Reason, s) {
			return fmt.Errorf("reason %q does not mention %q", w.decision.Reason, s)
		}
		return nil
	})
}
