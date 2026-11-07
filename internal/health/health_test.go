package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEmptyRegistryIsReady(t *testing.T) {
	rep := New(time.Second, nil).Check(context.Background())
	if !rep.Ready || rep.Status != "ready" || len(rep.Checks) != 0 {
		t.Fatalf("got %+v", rep)
	}
}

func TestAggregation(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return nil })
	r.Register("b", func(context.Context) error { return errors.New("secret host 10.0.0.1") })
	rep := r.Check(context.Background())
	if rep.Ready || rep.Status != "not_ready" {
		t.Fatalf("got %+v", rep)
	}
	if rep.Checks["a"] != Pass || rep.Checks["b"] != Fail {
		t.Fatalf("checks = %v", rep.Checks)
	}
}

func TestTimeout(t *testing.T) {
	r := New(20*time.Millisecond, nil)
	r.Register("slow", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return nil
	})
	start := time.Now()
	rep := r.Check(context.Background())
	if rep.Checks["slow"] != Fail {
		t.Fatalf("slow check should fail: %v", rep.Checks)
	}
	if time.Since(start) > time.Second {
		t.Fatal("timeout not enforced")
	}
}

func TestIgnoringContextStillTimesOut(t *testing.T) {
	r := New(20*time.Millisecond, nil)
	release := make(chan struct{})
	defer close(release)
	r.Register("stuck", func(context.Context) error { <-release; return nil })
	if rep := r.Check(context.Background()); rep.Checks["stuck"] != Fail {
		t.Fatalf("got %v", rep.Checks)
	}
}

func TestPanickingCheckFails(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("boom", func(context.Context) error { panic("kaboom") })
	if rep := r.Check(context.Background()); rep.Checks["boom"] != Fail {
		t.Fatalf("got %v", rep.Checks)
	}
}

func TestDraining(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return nil })
	r.SetDraining()
	if !r.Draining() {
		t.Fatal("draining not set")
	}
	if rep := r.Check(context.Background()); rep.Ready || rep.Status != "not_ready" {
		t.Fatalf("got %+v", rep)
	}
}

func TestRegisterReplaces(t *testing.T) {
	r := New(time.Second, nil)
	r.Register("a", func(context.Context) error { return errors.New("x") })
	r.Register("a", func(context.Context) error { return nil })
	if rep := r.Check(context.Background()); !rep.Ready {
		t.Fatalf("got %+v", rep)
	}
}
