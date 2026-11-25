package query

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugin/plugintest"
)

// Connector error kinds map to stable statuses and codes (FR-040 AC5).
func TestMapConnectorErrors(t *testing.T) {
	ctx := context.Background()
	for kind, want := range map[error]struct {
		status int
		code   string
	}{
		plugin.ErrDestinationNotAllowed: {403, "destination_not_allowed"},
		plugin.ErrUpstreamAuth:          {502, "upstream_auth_failed"},
		plugin.ErrUpstreamNotFound:      {404, "not_found"},
		plugin.ErrUpstreamRateLimited:   {429, "upstream_rate_limited"},
		plugin.ErrUpstreamUnreachable:   {502, "upstream_unreachable"},
		plugin.ErrUpstream:              {502, "upstream_error"},
		plugin.ErrResponseTooLarge:      {413, "too_large"},
		plugin.ErrUnsupportedMediaType:  {415, "unsupported_format"},
		plugin.ErrInvalidQuery:          {400, "invalid_query"},
		plugin.ErrInvalidConfig:         {400, "invalid_query"},
	} {
		r := mapError(ctx, ctx, fmt.Errorf("%w: detail", kind))
		if r.Status != want.status || r.Error == nil || r.Error.Code != want.code || r.Error.Message == "" {
			t.Errorf("%v: %+v", kind, r)
		}
	}
}

type panicSource struct{ *plugintest.Source }

func (panicSource) Query(context.Context, plugin.Query) (*frame.Result, error) {
	var s []int
	_ = s[3] // index out of range, as a plugin defect would
	return nil, nil
}

type oneSource struct{ src plugin.Source }

func (o oneSource) Source(string) (plugin.Source, error) { return o.src, nil }

// A panicking plugin fails its query with 500 instead of terminating the server
// (NFR-SEC-012, Codex P1 on #33).
func TestPluginPanicIsContained(t *testing.T) {
	svc, err := New(context.Background(), Options{
		Sources:   oneSource{panicSource{plugintest.NewSource("boom")}},
		Instances: []Instance{{UID: "b", Type: "boom", Name: "Boom"}},
		Limits:    frame.Limits{MaxRows: 10, MaxBytes: 1 << 20}, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, status, err := svc.Execute(context.Background(), Request{Queries: []Item{{RefID: "A", DataSource: DataSourceRef{UID: "b"}}}})
	if err != nil {
		t.Fatal(err)
	}
	r := resp.Results["A"]
	if status != 500 || r.Status != 500 || r.Error == nil || r.Error.Code != "query_failed" || strings.Contains(r.Error.Message, "index out of range") {
		t.Fatalf("status=%d result=%+v", status, r)
	}
}
