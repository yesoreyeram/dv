package automerge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const secretToken = "ghs_TESTTOKEN_must_not_leak"

// fakeGitHub serves the three endpoints the runner uses.
type fakeGitHub struct {
	mu         sync.Mutex
	pr         map[string]any
	checks     []map[string]any
	pageSize   int
	mergeCode  int
	mergeBody  map[string]any
	mergeCalls []map[string]string
	authOK     bool
}

func newFake() *fakeGitHub {
	return &fakeGitHub{
		pr: map[string]any{
			"number": 7, "state": "open", "draft": false, "mergeable": true,
			"user":   map[string]any{"login": "dependabot[bot]"},
			"head":   map[string]any{"sha": "abc", "repo": map[string]any{"full_name": "yesoreyeram/dv"}},
			"base":   map[string]any{"ref": "main"},
			"labels": []any{map[string]any{"name": "dependencies"}},
		},
		checks:    []map[string]any{{"name": "go", "status": "completed", "conclusion": "success"}},
		pageSize:  100,
		mergeCode: 200,
		mergeBody: map[string]any{"merged": true, "message": "Pull Request successfully merged"},
		authOK:    true,
	}
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+secretToken {
		f.authOK = false
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/yesoreyeram/dv/pulls/7":
		_ = json.NewEncoder(w).Encode(f.pr)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/yesoreyeram/dv/commits/abc/check-runs":
		page := 1
		_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
		lo := min((page-1)*f.pageSize, len(f.checks))
		hi := min(lo+f.pageSize, len(f.checks))
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(f.checks), "check_runs": f.checks[lo:hi]})
	case r.Method == http.MethodPut && r.URL.Path == "/repos/yesoreyeram/dv/pulls/7/merge":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mergeCalls = append(f.mergeCalls, body)
		w.WriteHeader(f.mergeCode)
		_ = json.NewEncoder(w).Encode(f.mergeBody)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}
}

func event(numbers ...int) Event {
	var ev Event
	ev.WorkflowRun.Conclusion = "success"
	ev.WorkflowRun.Event = "pull_request"
	ev.WorkflowRun.HeadSHA = "abc"
	for _, n := range numbers {
		ev.WorkflowRun.PullRequests = append(ev.WorkflowRun.PullRequests, struct {
			Number int `json:"number"`
		}{n})
	}
	return ev
}

func runner(t *testing.T, f *fakeGitHub) (*Runner, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	var logs bytes.Buffer
	return &Runner{
		API: srv.URL + "/", Token: secretToken, HTTP: srv.Client(),
		Log:    slog.New(slog.NewTextHandler(&logs, nil)),
		Policy: basePolicy(),
	}, &logs
}

func TestRunMergesWithPinnedSHA(t *testing.T) {
	f := newFake()
	r, logs := runner(t, f)
	out, err := r.Run(context.Background(), event(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !out[0].Merged {
		t.Fatalf("want merged, got %+v", out)
	}
	if len(f.mergeCalls) != 1 || f.mergeCalls[0]["sha"] != "abc" || f.mergeCalls[0]["merge_method"] != "squash" {
		t.Fatalf("merge must pin sha and squash: %+v", f.mergeCalls)
	}
	if !f.authOK {
		t.Fatal("every request must carry the bearer token")
	}
	if !strings.Contains(logs.String(), "automerge: merged") || !strings.Contains(logs.String(), "pr=7") {
		t.Fatalf("missing audit log line: %s", logs)
	}
}

func TestRunSkipsWithoutMergeCall(t *testing.T) {
	f := newFake()
	f.pr["labels"] = []any{map[string]any{"name": "hold"}}
	r, logs := runner(t, f)
	out, err := r.Run(context.Background(), event(7))
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Merged || len(f.mergeCalls) != 0 {
		t.Fatal("held PR must not be merged")
	}
	if !strings.Contains(logs.String(), `reason="pull request has the label \"hold\""`) {
		t.Fatalf("skip reason not logged: %s", logs)
	}
}

func TestRunFailingCheckOnLaterPage(t *testing.T) {
	f := newFake()
	f.pageSize = 2
	f.checks = append(f.checks,
		map[string]any{"name": "b", "status": "completed", "conclusion": "success"},
		map[string]any{"name": "c", "status": "completed", "conclusion": "failure"})
	r, _ := runner(t, f)
	out, err := r.Run(context.Background(), event(7))
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Merged || !strings.Contains(out[0].Decision.Reason, `"c"`) {
		t.Fatalf("check on page 2 must be seen: %+v", out[0])
	}
}

func TestRunTooManyCheckRunsRefusesToDecide(t *testing.T) {
	f := newFake()
	f.pageSize = 1
	f.checks = nil
	for i := range maxCheckPages + 1 {
		f.checks = append(f.checks, map[string]any{"name": fmt.Sprint(i), "status": "completed", "conclusion": "success"})
	}
	r, _ := runner(t, f)
	if _, err := r.Run(context.Background(), event(7)); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("want pagination bound error, got %v", err)
	}
	if len(f.mergeCalls) != 0 {
		t.Fatal("must not merge on a partial check list")
	}
}

func TestRunGitHubRefusesMerge(t *testing.T) {
	for _, code := range []int{http.StatusMethodNotAllowed, http.StatusConflict} {
		f := newFake()
		f.mergeCode = code
		f.mergeBody = map[string]any{"message": "Head branch was modified"}
		r, _ := runner(t, f)
		out, err := r.Run(context.Background(), event(7))
		if err != nil {
			t.Fatalf("%d: %v", code, err)
		}
		if out[0].Merged || !strings.Contains(out[0].Decision.Reason, fmt.Sprint(code)) {
			t.Fatalf("%d: %+v", code, out[0])
		}
	}
}

func TestRunMergedFalseInBody(t *testing.T) {
	f := newFake()
	f.mergeBody = map[string]any{"merged": false, "message": "nope"}
	r, _ := runner(t, f)
	out, err := r.Run(context.Background(), event(7))
	if err != nil || out[0].Merged || out[0].Decision.Reason != "nope" {
		t.Fatalf("got %+v, %v", out, err)
	}
}

func TestRunNeverLogsToken(t *testing.T) {
	f := newFake()
	f.mergeCode = http.StatusInternalServerError
	r, logs := runner(t, f)
	_, err := r.Run(context.Background(), event(7, 99)) // 99 is a 404
	if err == nil {
		t.Fatal("want API errors")
	}
	for _, s := range []string{err.Error(), logs.String()} {
		if strings.Contains(s, secretToken) || strings.Contains(s, "Bearer") {
			t.Fatalf("token leaked: %s", s)
		}
	}
	if !strings.Contains(err.Error(), "pulls/99: 404 Not Found") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("errors should name status and path: %v", err)
	}
}

func TestRunTimesOutOnSlowAPI(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)
	r := &Runner{
		API: srv.URL, Token: secretToken, HTTP: &http.Client{Timeout: 50 * time.Millisecond},
		Log: slog.New(slog.DiscardHandler), Policy: basePolicy(),
	}
	start := time.Now()
	_, err := r.Run(context.Background(), event(7))
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("want timeout error quickly, got %v after %s", err, time.Since(start))
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatal("token leaked")
	}
}

func TestRunDryRunAndNoPRs(t *testing.T) {
	f := newFake()
	r, logs := runner(t, f)
	r.DryRun = true
	out, err := r.Run(context.Background(), event(7))
	if err != nil || out[0].Merged || len(f.mergeCalls) != 0 || !strings.Contains(logs.String(), "dry run") {
		t.Fatalf("dry run must not merge: %+v %v", out, err)
	}
	out, err = r.Run(context.Background(), event())
	if err != nil || out != nil {
		t.Fatalf("no PRs: %+v %v", out, err)
	}
}

func TestRunDeletedForkAndMethod(t *testing.T) {
	f := newFake()
	f.pr["head"] = map[string]any{"sha": "abc", "repo": nil}
	r, _ := runner(t, f)
	out, err := r.Run(context.Background(), event(7))
	if err != nil || out[0].Merged || !strings.Contains(out[0].Decision.Reason, "fork") {
		t.Fatalf("got %+v %v", out, err)
	}
	f2 := newFake()
	r2, _ := runner(t, f2)
	r2.MergeMethod = "rebase"
	if _, err := r2.Run(context.Background(), event(7)); err != nil || f2.mergeCalls[0]["merge_method"] != "rebase" {
		t.Fatalf("method not honoured: %+v %v", f2.mergeCalls, err)
	}
}

func TestRunBadJSONAndDefaultClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{not json")
	}))
	defer srv.Close()
	r := &Runner{API: srv.URL, Log: slog.New(slog.DiscardHandler), Policy: basePolicy()}
	if _, err := r.Run(context.Background(), event(7)); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
	r.API = "http://127.0.0.1:1"
	if _, err := r.Run(context.Background(), event(7)); err == nil {
		t.Fatal("want dial error")
	}
	r.API = "://bad"
	if _, err := r.Run(context.Background(), event(7)); err == nil {
		t.Fatal("want URL error")
	}
}

func TestParseEvent(t *testing.T) {
	ev, err := ParseEvent(strings.NewReader(`{"workflow_run":{"name":"CI","conclusion":"success","event":"pull_request","head_sha":"abc","pull_requests":[{"number":7}]}}`))
	if err != nil || ev.WorkflowRun.HeadSHA != "abc" || ev.WorkflowRun.PullRequests[0].Number != 7 {
		t.Fatalf("got %+v %v", ev, err)
	}
	if _, err := ParseEvent(strings.NewReader("[")); err == nil {
		t.Fatal("want error")
	}
}
