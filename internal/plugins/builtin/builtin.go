// Package builtin registers dv's compiled-in plugins (ADR-0002).
package builtin

import (
	"fmt"

	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/netguard"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/csvformat"
	"github.com/yesoreyeram/dv/internal/plugins/filesource"
	"github.com/yesoreyeram/dv/internal/plugins/httpsource"
	"github.com/yesoreyeram/dv/internal/plugins/jsonformat"
)

// Options carry the host-owned resources plugins receive.
type Options struct {
	// Roots are the operator-configured file roots (may be empty, never nil).
	Roots *fsroot.Roots
	// FileMaxBytes caps files read by the file source.
	FileMaxBytes int64
	// QueryDefaults are the result limits sources apply when a query sets none; zero
	// members use each source's built-in defaults.
	QueryDefaults frame.Limits
	// Outbound is the SSRF policy for connector sources (NFR-SEC-005).
	Outbound netguard.Policy
}

// Register adds every built-in plugin to reg. It does not freeze reg.
func Register(reg *plugin.Registry, opts Options) error {
	for _, f := range []plugin.Format{csvformat.NewCSV(), csvformat.NewTSV(), jsonformat.NewJSON(), jsonformat.NewNDJSON()} {
		if err := reg.RegisterFormat(f); err != nil {
			return err
		}
	}
	fs, err := filesource.New(opts.Roots, reg, opts.FileMaxBytes, opts.QueryDefaults)
	if err != nil {
		return fmt.Errorf("file source: %w", err)
	}
	if err := reg.RegisterSource(fs); err != nil {
		return err
	}
	return reg.RegisterSource(httpsource.New(httpsource.Options{Policy: opts.Outbound, Defaults: opts.QueryDefaults}))
}
