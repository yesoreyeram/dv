package loadtest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuantileNearestRank(t *testing.T) {
	r := Result{}
	if r.Quantile(0.99) != 0 {
		t.Fatal("empty result must report 0")
	}
	for i := 1; i <= 100; i++ {
		r.Latencies = append(r.Latencies, time.Duration(i)*time.Millisecond)
	}
	cases := map[float64]time.Duration{
		0: 1 * time.Millisecond, 0.5: 50 * time.Millisecond, 0.99: 99 * time.Millisecond,
		1: 100 * time.Millisecond, -1: 1 * time.Millisecond, 2: 100 * time.Millisecond,
	}
	for q, want := range cases {
		if got := r.Quantile(q); got != want {
			t.Errorf("Quantile(%v) = %s, want %s", q, got, want)
		}
	}
	one := Result{Latencies: []time.Duration{7}}
	if one.Quantile(0.99) != 7 || one.Total() != 1 {
		t.Fatal("single sample")
	}
}

func TestRunIssuesRequestsOnSchedule(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/teapot" {
			w.WriteHeader(http.StatusTeapot)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	start := time.Now()
	res, err := Run(context.Background(), Options{Client: NewClient(time.Second), URL: srv.URL + "/teapot", Rate: 200, Duration: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total() != 50 || hits.Load() != 50 || res.Status[http.StatusTeapot] != 50 || res.Errors != 0 {
		t.Fatalf("unexpected result: %s (hits %d)", res, hits.Load())
	}
	// 50 requests at 5 ms spacing: the last is scheduled at 245 ms.
	if el := time.Since(start); el < 240*time.Millisecond {
		t.Fatalf("finished in %s; requests were not paced", el)
	}
	for i := 1; i < len(res.Latencies); i++ {
		if res.Latencies[i] < res.Latencies[i-1] {
			t.Fatal("latencies must be sorted")
		}
	}
	if !strings.Contains(res.String(), "requests=50") {
		t.Fatalf("summary: %s", res)
	}
}

// A slow server must show up as latency, not as a slower request schedule.
func TestRunIsOpenLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
	}))
	defer srv.Close()

	start := time.Now()
	res, err := Run(context.Background(), Options{Client: NewClient(time.Second), URL: srv.URL, Rate: 100, Duration: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total() != 20 || res.Status[200] != 20 {
		t.Fatalf("unexpected result: %s", res)
	}
	// Closed loop would take 20 × 40 ms = 800 ms.
	if el := time.Since(start); el > 600*time.Millisecond {
		t.Fatalf("took %s; generator waited for responses", el)
	}
	if res.Quantile(0) < 40*time.Millisecond {
		t.Fatalf("min latency %s below server delay", res.Quantile(0))
	}
}

func TestRunCountsTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused from here on

	res, err := Run(context.Background(), Options{Client: NewClient(time.Second), URL: url, Rate: 100, Duration: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if res.Errors != res.Total() || res.Total() != 5 || res.FirstErr == nil || len(res.Status) != 0 {
		t.Fatalf("unexpected result: %s (%v)", res, res.FirstErr)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := Run(ctx, Options{Client: NewClient(time.Second), URL: srv.URL, Rate: 50, Duration: 10 * time.Second})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second || res.Total() == 0 || res.Total() > 10 {
		t.Fatalf("did not stop promptly: %s", res)
	}

	// Already cancelled while behind schedule: returns without issuing more.
	done, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Run(done, Options{Client: NewClient(time.Second), URL: srv.URL, Rate: 1e5, Duration: time.Second}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunValidatesOptions(t *testing.T) {
	c := NewClient(time.Second)
	bad := []Options{
		{URL: "http://x", Rate: 1, Duration: time.Second},
		{Client: c, Rate: 1, Duration: time.Second},
		{Client: c, URL: "http://x", Rate: 0, Duration: time.Second},
		{Client: c, URL: "http://x", Rate: -1, Duration: time.Second},
		{Client: c, URL: "http://x", Rate: 1, Duration: 0},
		{Client: c, URL: "http://x", Rate: 1e6, Duration: 2 * time.Second},
	}
	for i, o := range bad {
		if _, err := Run(context.Background(), o); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
	// A malformed URL is a per-request error, not a crash.
	res, err := Run(context.Background(), Options{Client: c, URL: "http://\x7f", Rate: 10, Duration: 100 * time.Millisecond})
	if err != nil || res.Errors != 1 {
		t.Fatalf("err=%v res=%s", err, res)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	res, err := Run(context.Background(), Options{Client: NewClient(time.Second), URL: srv.URL, Rate: 10, Duration: 100 * time.Millisecond})
	if err != nil || res.Status[http.StatusFound] != 1 {
		t.Fatalf("err=%v res=%s", err, res)
	}
}
