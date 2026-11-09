package automerge

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func basePolicy() Policy {
	return Policy{Repo: "yesoreyeram/dv", DefaultBranch: "main", Authors: []string{"yesoreyeram", "dependabot[bot]"}}
}

func greenRun() Run { return Run{Conclusion: "success", Event: "pull_request", HeadSHA: "abc"} }

func openPR() PullRequest {
	return PullRequest{Number: 1, State: "open", Author: "dependabot[bot]", HeadRepo: "yesoreyeram/dv", HeadSHA: "abc", BaseRef: "main"}
}

func greenChecks() []CheckRun {
	return []CheckRun{{Name: "go", Status: "completed", Conclusion: "success"}}
}

func TestDecideMergesGreenTrustedPR(t *testing.T) {
	d := Decide(basePolicy(), greenRun(), openPR(), greenChecks())
	if !d.Merge || d.SHA != "abc" {
		t.Fatalf("want merge pinned to abc, got %+v", d)
	}
}

func TestDecideSkips(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Run, *PullRequest, *[]CheckRun)
		reason string
	}{
		{"ci failed", func(r *Run, _ *PullRequest, _ *[]CheckRun) { r.Conclusion = "failure" }, "CI concluded"},
		{"ci cancelled", func(r *Run, _ *PullRequest, _ *[]CheckRun) { r.Conclusion = "cancelled" }, "CI concluded"},
		{"push event", func(r *Run, _ *PullRequest, _ *[]CheckRun) { r.Event = "push" }, "not a pull_request"},
		{"closed", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.State = "closed" }, "not open"},
		{"draft", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.Draft = true }, "draft"},
		{"fork", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.HeadRepo = "attacker/dv" }, "fork"},
		{"deleted fork", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.HeadRepo = "" }, "fork"},
		{"author", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.Author = "mallory" }, "not allow-listed"},
		{"author substring", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.Author = "yesoreyeram-evil" }, "not allow-listed"},
		{"stacked base", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.BaseRef = "feat/parent" }, "default branch"},
		{"hold", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.Labels = []string{"deps", "HOLD"} }, "label"},
		{"head moved", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.HeadSHA = "def" }, "head moved"},
		{"empty run sha", func(r *Run, p *PullRequest, _ *[]CheckRun) { r.HeadSHA = ""; p.HeadSHA = "" }, "head moved"},
		{"conflict", func(_ *Run, p *PullRequest, _ *[]CheckRun) { p.Mergeable = ptr(false) }, "conflict"},
		{"no checks", func(_ *Run, _ *PullRequest, c *[]CheckRun) { *c = nil }, "no check runs"},
		{"pending check", func(_ *Run, _ *PullRequest, c *[]CheckRun) {
			*c = append(*c, CheckRun{Name: "e2e", Status: "in_progress"})
		}, "check run"},
		{"failed check", func(_ *Run, _ *PullRequest, c *[]CheckRun) {
			*c = append(*c, CheckRun{Name: "lint", Status: "completed", Conclusion: "failure"})
		}, "check run"},
		{"action required", func(_ *Run, _ *PullRequest, c *[]CheckRun) {
			*c = append(*c, CheckRun{Name: "x", Status: "completed", Conclusion: "action_required"})
		}, "check run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, p, c := greenRun(), openPR(), greenChecks()
			tt.mutate(&r, &p, &c)
			d := Decide(basePolicy(), r, p, c)
			if d.Merge {
				t.Fatalf("want skip, got merge")
			}
			if !strings.Contains(d.Reason, tt.reason) {
				t.Fatalf("reason %q does not mention %q", d.Reason, tt.reason)
			}
		})
	}
}

func TestDecideAllowsNeutralSkippedAndUnknownMergeability(t *testing.T) {
	c := append(greenChecks(),
		CheckRun{Name: "a", Status: "completed", Conclusion: "neutral"},
		CheckRun{Name: "b", Status: "completed", Conclusion: "skipped"})
	p := openPR()
	p.Mergeable = nil
	p.Author = "YesOreYeRam" // logins are case-insensitive
	if d := Decide(basePolicy(), greenRun(), p, c); !d.Merge {
		t.Fatalf("want merge, got %+v", d)
	}
	p.Mergeable = ptr(true)
	if d := Decide(basePolicy(), greenRun(), p, c); !d.Merge {
		t.Fatalf("want merge, got %+v", d)
	}
}

func TestParseAuthors(t *testing.T) {
	got := ParseAuthors(" a, ,b,,dependabot[bot] ")
	if strings.Join(got, "|") != "a|b|dependabot[bot]" {
		t.Fatalf("got %q", got)
	}
	if len(ParseAuthors("")) != 0 {
		t.Fatal("empty list must stay empty")
	}
}
