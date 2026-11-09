// Package automerge decides whether a pull request may be merged automatically after CI
// and performs that merge through the GitHub REST API (NFR-MAINT-006).
//
// The decision is a pure function ([Decide]) so every rule is unit- and BDD-tested
// without a network. [Runner] gathers the inputs from GitHub and acts on the decision.
package automerge

import (
	"fmt"
	"slices"
	"strings"
)

// HoldLabels stop the bot from merging a pull request (NFR-MAINT-006 AC2).
var HoldLabels = []string{"hold", "do-not-merge"}

// Policy is the static configuration of the bot.
type Policy struct {
	Repo          string   // "owner/name" the bot runs in
	DefaultBranch string   // only PRs targeting this branch merge
	Authors       []string // allow-listed PR authors (GitHub logins)
}

// Run is the CI workflow run that triggered the bot.
type Run struct {
	Conclusion string // "success", "failure", ...
	Event      string // "pull_request", "push", ...
	HeadSHA    string
}

// PullRequest is the subset of a GitHub pull request the decision needs.
type PullRequest struct {
	Number    int
	State     string // "open" or "closed"
	Draft     bool
	Author    string
	HeadRepo  string // "owner/name" of the head branch's repository
	HeadSHA   string
	BaseRef   string
	Labels    []string
	Mergeable *bool // nil while GitHub has not computed it
}

// CheckRun is one check on the head commit.
type CheckRun struct {
	Name       string
	Status     string // "queued", "in_progress", "completed"
	Conclusion string // set when Status is "completed"
}

// Decision is the outcome of [Decide].
type Decision struct {
	Merge  bool
	SHA    string // commit the merge must be pinned to (NFR-MAINT-006 AC3)
	Reason string
}

func skip(format string, a ...any) Decision {
	return Decision{Reason: fmt.Sprintf(format, a...)}
}

// Decide applies every rule of NFR-MAINT-006 AC1/AC2 and returns the first one that
// fails, or a merge decision pinned to the verified head commit.
func Decide(p Policy, run Run, pr PullRequest, checks []CheckRun) Decision {
	switch {
	case run.Conclusion != "success":
		return skip("CI concluded %q, not success", run.Conclusion)
	case run.Event != "pull_request":
		return skip("CI run event is %q, not a pull_request", run.Event)
	case pr.State != "open":
		return skip("pull request is %s, not open", pr.State)
	case pr.Draft:
		return skip("pull request is a draft")
	case !strings.EqualFold(pr.HeadRepo, p.Repo):
		return skip("head repository %q is a fork of %q", pr.HeadRepo, p.Repo)
	case !slices.ContainsFunc(p.Authors, func(a string) bool { return strings.EqualFold(a, pr.Author) }):
		return skip("author %q is not allow-listed", pr.Author)
	case pr.BaseRef != p.DefaultBranch:
		return skip("base %q is not the default branch %q", pr.BaseRef, p.DefaultBranch)
	}
	for _, l := range pr.Labels {
		if slices.ContainsFunc(HoldLabels, func(h string) bool { return strings.EqualFold(h, l) }) {
			return skip("pull request has the label %q", l)
		}
	}
	if run.HeadSHA == "" || pr.HeadSHA != run.HeadSHA {
		return skip("head moved: CI ran on %q but the pull request is at %q", run.HeadSHA, pr.HeadSHA)
	}
	if pr.Mergeable != nil && !*pr.Mergeable {
		return skip("pull request has a merge conflict")
	}
	if len(checks) == 0 {
		return skip("no check runs on %s", pr.HeadSHA)
	}
	for _, c := range checks {
		if c.Status != "completed" {
			return skip("check run %q is %s", c.Name, c.Status)
		}
		switch c.Conclusion {
		case "success", "neutral", "skipped":
		default:
			return skip("check run %q concluded %q", c.Name, c.Conclusion)
		}
	}
	return Decision{Merge: true, SHA: pr.HeadSHA, Reason: "CI green on " + pr.HeadSHA}
}

// ParseAuthors splits a comma-separated allow-list, dropping blanks.
func ParseAuthors(s string) []string {
	var out []string
	for a := range strings.SplitSeq(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}
