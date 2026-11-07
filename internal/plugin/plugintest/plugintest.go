// Package plugintest provides minimal in-memory plugins for tests. It is not compiled
// into the dv binary.
package plugintest

import (
	"context"
	"io"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/plugin"
)

// Source is a stub data-source plugin that returns an empty result.
type Source struct{ M plugin.SourceMeta }

// NewSource returns a stub source with the given ID.
func NewSource(id string) *Source {
	return &Source{M: plugin.SourceMeta{ID: id, Name: id, Description: "test source " + id, Capabilities: []string{"query"}}}
}

// Meta implements plugin.Source.
func (s *Source) Meta() plugin.SourceMeta { return s.M }

// Validate implements plugin.Source.
func (s *Source) Validate(ctx context.Context, _ plugin.SourceConfig) error { return ctx.Err() }

// Query implements plugin.Source.
func (s *Source) Query(ctx context.Context, _ plugin.Query) (*frame.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &frame.Result{Frames: []*frame.Frame{}}, nil
}

// Health implements plugin.Source.
func (s *Source) Health(ctx context.Context, _ plugin.SourceConfig) error { return ctx.Err() }

// Format is a stub format plugin that decodes nothing.
type Format struct{ M plugin.FormatMeta }

// NewFormat returns a stub format with the given ID.
func NewFormat(id string) *Format {
	return &Format{M: plugin.FormatMeta{ID: id, Name: id, Description: "test format " + id}}
}

// Meta implements plugin.Format.
func (f *Format) Meta() plugin.FormatMeta { return f.M }

// Decode implements plugin.Format.
func (f *Format) Decode(ctx context.Context, _ io.Reader, _ plugin.DecodeOptions) ([]*frame.Frame, error) {
	return nil, ctx.Err()
}
