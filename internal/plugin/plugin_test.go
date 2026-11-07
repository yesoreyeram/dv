package plugin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugin/plugintest"
)

func TestRegisterAndLookup(t *testing.T) {
	r := plugin.NewRegistry()
	if err := r.RegisterSource(plugintest.NewSource("zeta")); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterSource(plugintest.NewSource("alpha")); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterFormat(plugintest.NewFormat("csv")); err != nil {
		t.Fatal(err)
	}
	if s, err := r.Source("alpha"); err != nil || s.Meta().ID != "alpha" {
		t.Fatalf("lookup: %v", err)
	}
	if f, err := r.Format("csv"); err != nil || f.Meta().ID != "csv" {
		t.Fatalf("lookup: %v", err)
	}
	if _, err := r.Source("nope"); !errors.Is(err, plugin.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := r.Format("nope"); !errors.Is(err, plugin.ErrNotFound) {
		t.Fatal(err)
	}

	c := r.Catalog()
	if len(c.Sources) != 2 || c.Sources[0].ID != "alpha" || c.Sources[1].ID != "zeta" || len(c.Formats) != 1 {
		t.Fatalf("catalog: %+v", c)
	}
	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), `"mediaTypes":[]`) || !strings.Contains(string(b), `"capabilities":["query"]`) {
		t.Fatalf("catalog JSON must use arrays, got %s", b)
	}
}

func TestCatalogEmptyListsAndCopies(t *testing.T) {
	r := plugin.NewRegistry()
	b, _ := json.Marshal(r.Catalog())
	if string(b) != `{"sources":[],"formats":[]}` {
		t.Fatal(string(b))
	}
	s := plugintest.NewSource("src")
	s.M.Capabilities = nil
	_ = r.RegisterSource(s)
	if c := r.Catalog(); c.Sources[0].Capabilities == nil {
		t.Fatal("nil capabilities")
	}
	s2 := plugintest.NewSource("src2")
	_ = r.RegisterSource(s2)
	c := r.Catalog()
	c.Sources[1].Capabilities[0] = "mutated"
	if s2.M.Capabilities[0] != "query" {
		t.Fatal("catalog aliases plugin metadata")
	}
}

func TestRegistrationRules(t *testing.T) {
	r := plugin.NewRegistry()
	for _, id := range []string{"", "a", "Bad_ID", "1abc", "has space", "x" + strings.Repeat("y", 63), "ünï"} {
		if err := r.RegisterSource(plugintest.NewSource(id)); !errors.Is(err, plugin.ErrInvalidID) {
			t.Errorf("id %q: want ErrInvalidID, got %v", id, err)
		}
		if err := r.RegisterFormat(plugintest.NewFormat(id)); !errors.Is(err, plugin.ErrInvalidID) {
			t.Errorf("format id %q: want ErrInvalidID, got %v", id, err)
		}
	}
	for _, id := range []string{"ab", "postgres", "rest-api", "s3", "x" + strings.Repeat("y", 62)} {
		if err := r.RegisterSource(plugintest.NewSource(id)); err != nil {
			t.Errorf("id %q rejected: %v", id, err)
		}
	}
	if err := r.RegisterSource(plugintest.NewSource("postgres")); !errors.Is(err, plugin.ErrDuplicate) {
		t.Fatalf("duplicate source: %v", err)
	}
	_ = r.RegisterFormat(plugintest.NewFormat("csv"))
	if err := r.RegisterFormat(plugintest.NewFormat("csv")); !errors.Is(err, plugin.ErrDuplicate) {
		t.Fatalf("duplicate format: %v", err)
	}
	// Sources and formats are separate namespaces.
	if err := r.RegisterFormat(plugintest.NewFormat("postgres")); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterSource(nil); err == nil {
		t.Fatal("nil source accepted")
	}
	if err := r.RegisterFormat(nil); err == nil {
		t.Fatal("nil format accepted")
	}
}

func TestFreezeAndReadiness(t *testing.T) {
	r := plugin.NewRegistry()
	if err := r.ReadinessCheck(context.Background()); !errors.Is(err, plugin.ErrNotFrozen) {
		t.Fatal("readiness must fail before freeze")
	}
	r.Freeze()
	r.Freeze()
	if !r.Frozen() {
		t.Fatal("not frozen")
	}
	if err := r.ReadinessCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterSource(plugintest.NewSource("late")); !errors.Is(err, plugin.ErrFrozen) {
		t.Fatalf("late source: %v", err)
	}
	if err := r.RegisterFormat(plugintest.NewFormat("late")); !errors.Is(err, plugin.ErrFrozen) {
		t.Fatalf("late format: %v", err)
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := plugin.NewRegistry()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(2)
		go func() { defer wg.Done(); _ = r.RegisterSource(plugintest.NewSource(fmt.Sprintf("s%d", i))) }()
		go func() { defer wg.Done(); _ = r.Catalog(); _, _ = r.Source("s1") }()
	}
	wg.Wait()
	if len(r.Catalog().Sources) != 20 {
		t.Fatal("lost registrations")
	}
}

func TestSecureValuesNeverRevealed(t *testing.T) {
	const secret = "hunter2-s3cr3t"
	sv := plugin.SecureValues{"password": secret, "token": "", "apiKey": "k-" + secret}
	cfg := plugin.SourceConfig{UID: "u1", Type: "postgres", Name: "db", SecureJSONData: sv}

	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("secret in JSON: %s", b)
	}
	if !strings.Contains(string(b), `"secureJsonFields":{"apiKey":true,"password":true}`) {
		t.Fatalf("set flags missing: %s", b)
	}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if out := fmt.Sprintf(verb, cfg); strings.Contains(out, secret) {
			t.Errorf("%s leaks secret: %s", verb, out)
		}
		if out := fmt.Sprintf(verb, sv); strings.Contains(out, secret) {
			t.Errorf("%s leaks secret: %s", verb, out)
		}
	}

	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	log.Info("configured", "secure", sv, "cfg", cfg)
	if strings.Contains(logs.String(), secret) || !strings.Contains(logs.String(), plugin.Redacted) {
		t.Fatalf("slog leaks secret: %s", logs.String())
	}

	if v, ok := sv.Get("password"); !ok || v != secret {
		t.Fatal("Get must return the secret to server-side callers")
	}
	if got := sv.Keys(); len(got) != 2 || got[0] != "apiKey" {
		t.Fatalf("keys: %v", got)
	}
}

func TestStubPlugins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s, f := plugintest.NewSource("s"), plugintest.NewFormat("f")
	if res, err := s.Query(ctx, plugin.Query{}); err != nil || res == nil {
		t.Fatal(err)
	}
	if s.Validate(ctx, plugin.SourceConfig{}) != nil || s.Health(ctx, plugin.SourceConfig{}) != nil {
		t.Fatal("stub should succeed")
	}
	if _, err := f.Decode(ctx, strings.NewReader(""), plugin.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := s.Query(ctx, plugin.Query{}); !errors.Is(err, context.Canceled) {
		t.Fatal("stub must honour cancellation")
	}
}
