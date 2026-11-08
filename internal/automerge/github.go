package automerge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds every GitHub API call (threat model F-09, D).
const DefaultTimeout = 15 * time.Second

// maxBody caps how much of any API response is read.
const maxBody = 4 << 20

// maxCheckPages bounds check-run pagination (100 per page).
const maxCheckPages = 10

// Event is the subset of a `workflow_run` webhook payload the bot needs.
type Event struct {
	WorkflowRun struct {
		Name         string `json:"name"`
		Conclusion   string `json:"conclusion"`
		Event        string `json:"event"`
		HeadSHA      string `json:"head_sha"`
		PullRequests []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	} `json:"workflow_run"`
}

// ParseEvent decodes a `workflow_run` payload (the file at GITHUB_EVENT_PATH).
func ParseEvent(r io.Reader) (Event, error) {
	var ev Event
	if err := json.NewDecoder(io.LimitReader(r, maxBody)).Decode(&ev); err != nil {
		return ev, fmt.Errorf("decode workflow_run event: %w", err)
	}
	return ev, nil
}

// Runner fetches pull-request state from GitHub, applies [Decide] and merges.
type Runner struct {
	API    string // e.g. https://api.github.com (GITHUB_API_URL)
	Token  string // never logged (NFR-MAINT-006 AC4)
	HTTP   *http.Client
	Log    *slog.Logger
	Policy Policy
	// MergeMethod is "squash", "merge" or "rebase"; empty means "squash".
	MergeMethod string
	// DryRun logs the decision but never calls the merge endpoint.
	DryRun bool
}

// Outcome is what happened to one pull request.
type Outcome struct {
	Number   int
	Decision Decision
	Merged   bool
}

// Run handles every pull request attached to the CI run in ev.
func (r *Runner) Run(ctx context.Context, ev Event) ([]Outcome, error) {
	run := Run{
		Conclusion: ev.WorkflowRun.Conclusion,
		Event:      ev.WorkflowRun.Event,
		HeadSHA:    ev.WorkflowRun.HeadSHA,
	}
	if len(ev.WorkflowRun.PullRequests) == 0 {
		r.Log.Info("automerge: no same-repository pull request on this run", "sha", run.HeadSHA)
		return nil, nil
	}
	var out []Outcome
	var errs []error
	for _, ref := range ev.WorkflowRun.PullRequests {
		o, err := r.handle(ctx, run, ref.Number)
		if err != nil {
			errs = append(errs, fmt.Errorf("pull request #%d: %w", ref.Number, err))
			continue
		}
		out = append(out, o)
	}
	return out, errors.Join(errs...)
}

func (r *Runner) handle(ctx context.Context, run Run, number int) (Outcome, error) {
	o := Outcome{Number: number}
	pr, err := r.pullRequest(ctx, number)
	if err != nil {
		return o, err
	}
	var checks []CheckRun
	// Look up check runs only when every other rule passes (a placeholder green check
	// stands in for them); the final Decide below re-validates everything.
	if pre := Decide(r.Policy, run, pr, []CheckRun{{Status: "completed", Conclusion: "success"}}); pre.Merge {
		if checks, err = r.checkRuns(ctx, pr.HeadSHA); err != nil {
			return o, err
		}
	}
	o.Decision = Decide(r.Policy, run, pr, checks)
	if !o.Decision.Merge {
		r.Log.Info("automerge: skipped", "pr", number, "sha", pr.HeadSHA, "reason", o.Decision.Reason)
		return o, nil
	}
	if r.DryRun {
		r.Log.Info("automerge: would merge (dry run)", "pr", number, "sha", o.Decision.SHA)
		return o, nil
	}
	merged, reason, err := r.merge(ctx, number, o.Decision.SHA)
	if err != nil {
		return o, err
	}
	o.Merged = merged
	if !merged {
		o.Decision = Decision{Reason: reason}
		r.Log.Info("automerge: GitHub refused the merge", "pr", number, "sha", pr.HeadSHA, "reason", reason)
		return o, nil
	}
	r.Log.Info("automerge: merged", "pr", number, "sha", o.Decision.SHA, "method", r.method())
	return o, nil
}

func (r *Runner) method() string {
	if r.MergeMethod == "" {
		return "squash"
	}
	return r.MergeMethod
}

type ghPull struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	Draft     bool   `json:"draft"`
	Mergeable *bool  `json:"mergeable"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (r *Runner) pullRequest(ctx context.Context, number int) (PullRequest, error) {
	var g ghPull
	if _, err := r.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", r.Policy.Repo, number), nil, &g); err != nil {
		return PullRequest{}, err
	}
	pr := PullRequest{
		Number: g.Number, State: g.State, Draft: g.Draft, Author: g.User.Login,
		HeadSHA: g.Head.SHA, BaseRef: g.Base.Ref, Mergeable: g.Mergeable,
	}
	if g.Head.Repo != nil { // nil when the fork was deleted
		pr.HeadRepo = g.Head.Repo.FullName
	}
	for _, l := range g.Labels {
		pr.Labels = append(pr.Labels, l.Name)
	}
	return pr, nil
}

func (r *Runner) checkRuns(ctx context.Context, sha string) ([]CheckRun, error) {
	var all []CheckRun
	for page := 1; page <= maxCheckPages; page++ {
		var g struct {
			Total     int `json:"total_count"`
			CheckRuns []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		q := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}, "filter": {"latest"}}
		path := fmt.Sprintf("/repos/%s/commits/%s/check-runs?%s", r.Policy.Repo, url.PathEscape(sha), q.Encode())
		if _, err := r.do(ctx, http.MethodGet, path, nil, &g); err != nil {
			return nil, err
		}
		for _, c := range g.CheckRuns {
			all = append(all, CheckRun{Name: c.Name, Status: c.Status, Conclusion: c.Conclusion})
		}
		if len(g.CheckRuns) == 0 || len(all) >= g.Total {
			return all, nil
		}
	}
	// Refuse to decide on a partial list.
	return nil, fmt.Errorf("more than %d check runs on %s", maxCheckPages*100, sha)
}

// merge returns merged=false with GitHub's reason when it refuses for a state reason
// (405 not mergeable / blocked by protection, 409 head moved since the pinned SHA).
func (r *Runner) merge(ctx context.Context, number int, sha string) (bool, string, error) {
	body := map[string]string{"sha": sha, "merge_method": r.method()}
	var res struct {
		Merged  bool   `json:"merged"`
		Message string `json:"message"`
	}
	_, err := r.do(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/pulls/%d/merge", r.Policy.Repo, number), body, &res)
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusMethodNotAllowed || apiErr.Status == http.StatusConflict) {
		return false, fmt.Sprintf("GitHub answered %d: %s", apiErr.Status, apiErr.Message), nil
	}
	if err != nil {
		return false, "", err
	}
	return res.Merged, res.Message, nil
}

// APIError is a non-2xx GitHub answer. It never carries request headers.
type APIError struct {
	Method, Path string
	Status       int
	Message      string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub %s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

func (r *Runner) do(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.API, "/")+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "dv-automerge")
	if r.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		// url.Error includes the URL only (no headers, no token).
		return 0, fmt.Errorf("GitHub %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("GitHub %s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &m)
		return resp.StatusCode, &APIError{Method: method, Path: path, Status: resp.StatusCode, Message: m.Message}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("GitHub %s %s: decode: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}
