// Package loadtest is a small open-loop HTTP load generator used to verify latency
// requirements (NFR-PERF-001) in tests, BDD scenarios and `make perf`.
//
// Open loop means requests are issued on a fixed schedule regardless of how long
// earlier ones take, so a slow server cannot slow the generator down and hide its own
// latency (coordinated omission). Latency is measured from each request's scheduled
// start, not from when a goroutine happened to run it.
package loadtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

// MaxRequests bounds one run so a mistyped rate or duration cannot exhaust memory.
const MaxRequests = 1_000_000

// Options configures a run.
type Options struct {
	// Client sends the requests. It must not follow redirects for meaningful numbers.
	Client *http.Client
	// URL is requested with GET.
	URL string
	// Rate is the number of requests started per second (> 0).
	Rate float64
	// Duration is how long requests are started for (> 0).
	Duration time.Duration
}

// Result summarises a run. Latencies are sorted ascending.
type Result struct {
	Latencies []time.Duration
	Status    map[int]int // count per HTTP status
	Errors    int         // transport errors (no HTTP status)
	FirstErr  error
}

// Total is the number of requests issued.
func (r Result) Total() int { return len(r.Latencies) }

// Quantile returns the q-quantile (0 ≤ q ≤ 1) by the nearest-rank method, or 0 for
// an empty result.
func (r Result) Quantile(q float64) time.Duration {
	n := len(r.Latencies)
	if n == 0 {
		return 0
	}
	q = min(max(q, 0), 1)
	rank := int(q*float64(n)+0.999999999) - 1 // ceil(q·n) - 1
	return r.Latencies[min(max(rank, 0), n-1)]
}

// String renders a one-line summary for logs.
func (r Result) String() string {
	return fmt.Sprintf("requests=%d status=%v errors=%d p50=%s p90=%s p99=%s max=%s",
		r.Total(), r.Status, r.Errors, r.Quantile(0.5), r.Quantile(0.9), r.Quantile(0.99), r.Quantile(1))
}

// Run issues GET requests at opts.Rate for opts.Duration and waits for all of them.
// Cancelling ctx stops scheduling new requests and aborts in-flight ones.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Client == nil || opts.URL == "" {
		return Result{}, errors.New("loadtest: client and URL are required")
	}
	if !(opts.Rate > 0) || opts.Duration <= 0 {
		return Result{}, errors.New("loadtest: rate and duration must be positive")
	}
	total := opts.Rate * opts.Duration.Seconds()
	if total > MaxRequests {
		return Result{}, fmt.Errorf("loadtest: %.0f requests exceeds the limit of %d", total, MaxRequests)
	}
	n := max(int(total), 1)
	interval := time.Duration(float64(time.Second) / opts.Rate)

	var (
		mu  sync.Mutex
		res = Result{Latencies: make([]time.Duration, 0, n), Status: map[int]int{}}
		wg  sync.WaitGroup
	)
	record := func(lat time.Duration, code int, err error) {
		mu.Lock()
		defer mu.Unlock()
		res.Latencies = append(res.Latencies, lat)
		if err != nil {
			res.Errors++
			if res.FirstErr == nil {
				res.FirstErr = err
			}
			return
		}
		res.Status[code]++
	}

	start := time.Now()
	for i := range n {
		scheduled := start.Add(time.Duration(i) * interval)
		if d := time.Until(scheduled); d > 0 {
			t := time.NewTimer(d)
			select {
			case <-ctx.Done():
				t.Stop()
				wg.Wait()
				return finish(res), ctx.Err()
			case <-t.C:
			}
		} else if ctx.Err() != nil {
			wg.Wait()
			return finish(res), ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, err := get(ctx, opts.Client, opts.URL)
			record(time.Since(scheduled), code, err)
		}()
	}
	wg.Wait()
	return finish(res), nil
}

func finish(r Result) Result {
	slices.Sort(r.Latencies)
	return r
}

func get(ctx context.Context, c *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the keep-alive connection is reused; probe bodies are tiny, cap anyway.
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)); err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}

// NewClient returns a keep-alive client suited to load tests against one host: it
// never follows redirects and keeps enough idle connections that the generator does
// not measure TCP handshakes.
func NewClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:               nil, // load tests target local servers; never go via a proxy
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 256,
		IdleConnTimeout:     30 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
