package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/yesoreyeram/dv/internal/plugin"
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
