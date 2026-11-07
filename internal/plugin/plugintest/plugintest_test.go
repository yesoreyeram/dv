package plugintest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yesoreyeram/dv/internal/plugin"
)

func TestStubPlugins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s, f := NewSource("s"), NewFormat("f")
	if s.Meta().ID != "s" || f.Meta().ID != "f" {
		t.Fatal("meta IDs")
	}
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
