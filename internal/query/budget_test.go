package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

// fill builds one frame that uses its byte limit (or 10 000 rows if unlimited).
func fill(l frame.Limits) (*frame.Result, error) {
	fld, err := frame.NewField("s", frame.TypeString)
	if err != nil {
		return nil, err
	}
	b, err := frame.NewBuilder("f", l, fld)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 10000; i++ {
		if err := b.AppendRow(strings.Repeat("x", 20000)); errors.Is(err, frame.ErrLimitReached) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	return &frame.Result{Frames: []*frame.Frame{b.Frame()}}, nil
}

func TestBudgetReserveShares(t *testing.T) {
	b := newBudget(1200, 6)
	// Four queries start together: each gets a sixth.
	for i := 0; i < MaxConcurrency; i++ {
		if g := b.reserve(); g != 200 {
			t.Fatalf("grant %d = %d, want 200", i, g)
		}
	}
	// One finishes using 50: 150 comes back and is split by the two queries left.
	b.settle(200, 50)
	if g := b.reserve(); g != 275 {
		t.Fatalf("after a partial settle grant = %d, want (400+150)/2 = 275", g)
	}
	// Everything else is used in full; the last query finds nothing left.
	b.settle(200, 200)
	b.settle(200, 200)
	b.settle(200, 200)
	b.settle(275, 275)
	if g := b.reserve(); g != 275 {
		t.Fatalf("last query grant = %d, want the remaining 275", g)
	}
	b.settle(275, 275)
	if b.remaining != 0 || b.inFlight != 0 || b.notStarted != 0 {
		t.Fatalf("state remaining=%d inFlight=%d notStarted=%d", b.remaining, b.inFlight, b.notStarted)
	}
	if g := b.reserve(); g != 0 {
		t.Fatalf("exhausted budget granted %d", g)
	}
}

func TestBudgetSingleQueryGetsEverything(t *testing.T) {
	if g := newBudget(777, 1).reserve(); g != 777 {
		t.Fatalf("grant = %d", g)
	}
}

func TestBudgetTinyRemainderIsNotZero(t *testing.T) {
	b := newBudget(3, 4)
	if g := b.reserve(); g != 3 {
		t.Fatalf("grant = %d, want the remainder 3 (0 would mean unlimited)", g)
	}
}

func TestBudgetOvershootLeavesNothing(t *testing.T) {
	b := newBudget(100, 2)
	g := b.reserve()
	b.settle(g, g+500) // a source ignored its limit
	if got := b.reserve(); got != 0 {
		t.Fatalf("grant after overshoot = %d", got)
	}
}

func TestBudgetConcurrentNeverOverCommits(t *testing.T) {
	const total = 1 << 16
	b := newBudget(total, MaxQueries)
	var mu sync.Mutex
	var open, peak int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, MaxConcurrency)
	for i := 0; i < MaxQueries; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			g := b.reserve()
			mu.Lock()
			open += g
			peak = max(peak, open)
			mu.Unlock()
			time.Sleep(time.Millisecond)
			b.settle(g, g/2)
			mu.Lock()
			open -= g - g/2
			mu.Unlock()
		}()
	}
	wg.Wait()
	if peak > total || open > total {
		t.Fatalf("committed %d (peak %d) of %d", open, peak, total)
	}
}

// B-062: the whole request retains at most the host byte limit (FR-016 AC9).
func TestExecuteSharesTheByteBudget(t *testing.T) {
	f := &fake{}
	s := newService(t, f, 5*time.Second, nil) // MaxBytes 1 MiB
	single, _, err := s.Execute(context.Background(), Request{Queries: []Item{q("A", "a", `{"fill":true}`)}})
	if err != nil {
		t.Fatal(err)
	}
	one := single.Results["A"].Frames[0].EstimatedBytes()
	if one > s.limits.MaxBytes || one < s.limits.MaxBytes*9/10 {
		t.Fatalf("single query used %d of %d", one, s.limits.MaxBytes)
	}
	for _, n := range []int{2, 4, 7, MaxQueries} {
		qs := make([]Item, n)
		for i := range qs {
			qs[i] = q(fmt.Sprintf("Q%d", i), "a", `{"fill":true}`)
		}
		resp, _, err := s.Execute(context.Background(), Request{Queries: qs})
		if err != nil {
			t.Fatal(err)
		}
		var total int64
		shared := 0
		for _, r := range resp.Results {
			for _, fr := range r.Frames {
				total += fr.EstimatedBytes()
				for _, no := range fr.Meta.Notices {
					if strings.Contains(no.Text, "request byte budget") {
						shared++
					}
				}
			}
		}
		if total > s.limits.MaxBytes {
			t.Fatalf("%d queries retained %d bytes, budget %d", n, total, s.limits.MaxBytes)
		}
		if shared == 0 {
			t.Fatalf("%d queries: no budget notice", n)
		}
	}
}

func TestExhaustedBudgetIs413WithoutRunning(t *testing.T) {
	f := &fake{}
	s := newService(t, f, 5*time.Second, nil)
	res, used := s.run(context.Background(), q("B", "a", `{"err":"internal"}`), plugin.TimeRange{}, 0, 2)
	if res.Status != 413 || res.Error == nil || res.Error.Code != "budget_exhausted" || used != 0 {
		t.Fatalf("result %+v used %d", res, used)
	}
	if f.lastLimits.Load() != nil {
		t.Fatal("the source ran although the budget was exhausted")
	}
	// An unknown uid still reports the uid, not the budget.
	if res, _ := s.run(context.Background(), q("C", "nope", `{}`), plugin.TimeRange{}, 0, 2); res.Status != 404 {
		t.Fatalf("unknown uid with no budget: %+v", res)
	}
}

func TestRowTruncationGetsNoBudgetNotice(t *testing.T) {
	f := &fake{}
	s := newService(t, f, 5*time.Second, nil) // MaxRows 100
	qs := []Item{q("A", "a", `{"fill":true}`), q("B", "a", `{"fill":true}`)}
	qs[0].MaxRows = 3
	resp, _, err := s.Execute(context.Background(), Request{Queries: qs})
	if err != nil {
		t.Fatal(err)
	}
	fr := resp.Results["A"].Frames[0]
	if !fr.Meta.Truncated || fr.Rows() != 3 {
		t.Fatalf("A: truncated=%v rows=%d", fr.Meta.Truncated, fr.Rows())
	}
	for _, no := range fr.Meta.Notices {
		if strings.Contains(no.Text, "request byte budget") {
			t.Fatalf("row-limited frame got a budget notice: %q", no.Text)
		}
	}
}
