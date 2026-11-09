// Command dv runs the dv data visualization server.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yesoreyeram/dv/internal/buildinfo"
	"github.com/yesoreyeram/dv/internal/config"
	"github.com/yesoreyeram/dv/internal/frame"
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/health"
	"github.com/yesoreyeram/dv/internal/httpx"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/builtin"
	"github.com/yesoreyeram/dv/internal/query"
	"github.com/yesoreyeram/dv/internal/server"
)

func main() { os.Exit(realMain()) }

func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], config.OSSource(), os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "dv:", err)
		return 1
	}
	return 0
}

// run is the testable entry point.
func run(ctx context.Context, args []string, src config.Source, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			info := buildinfo.Get()
			_, err := fmt.Fprintf(stdout, "dv %s (commit %s, built %s, %s)\n", info.Version, info.Commit, info.BuildDate, info.GoVersion)
			return err
		default:
			return fmt.Errorf("unknown argument %q (usage: dv [version])", args[0])
		}
	}
	cfg, err := config.Load(src)
	if err != nil {
		return err
	}
	log := newLogger(cfg, stderr)
	slog.SetDefault(log)
	// Host-owned file roots are opened once; plugins only ever see them through
	// fsroot.Roots (ADR-0002, NFR-SEC-006).
	roots, err := fsroot.Open(cfg.FileRoots)
	if err != nil {
		return err
	}
	defer func() { _ = roots.Close() }()
	// Compiled-in plugins are registered here before the registry is frozen (ADR-0002).
	plugins := plugin.NewRegistry()
	if err := builtin.Register(plugins, builtin.Options{
		Roots: roots, FileMaxBytes: cfg.FileMaxBytes,
		QueryDefaults: frame.Limits{MaxRows: cfg.QueryMaxRows, MaxBytes: cfg.QueryMaxBytes},
	}); err != nil {
		return err
	}
	plugins.Freeze()
	checks := health.New(cfg.ReadinessCheckTimeout.Std(), log)
	checks.Register("file-roots", roots.ReadinessCheck)
	log.Info("plugins registered", "fileRoots", roots.Names())
	// Provisioned data sources are validated by their plugins even while the query API
	// is off, so a broken config fails at start-up rather than on first use (FR-017 AC2).
	queries, err := newQueryService(ctx, cfg, plugins, log)
	if err != nil {
		return err
	}
	srv, err := server.New(server.Options{Config: cfg, Logger: log, Health: checks, Build: buildinfo.Get(), Plugins: plugins, Query: queries})
	if err != nil {
		return err
	}
	return srv.ListenAndServe(ctx)
}

func newQueryService(ctx context.Context, cfg config.Config, plugins *plugin.Registry, log *slog.Logger) (*query.Service, error) {
	instances := make([]query.Instance, 0, len(cfg.DataSources))
	for _, ds := range cfg.DataSources {
		instances = append(instances, query.Instance{UID: ds.UID, Type: ds.Type, Name: ds.Name, JSONData: ds.JSONData})
	}
	svc, err := query.New(ctx, query.Options{
		Sources: plugins, Instances: instances, Timeout: cfg.QueryTimeout.Std(), Logger: log,
		Limits: frame.Limits{MaxRows: cfg.QueryMaxRows, MaxBytes: cfg.QueryMaxBytes},
	})
	if err != nil {
		return nil, fmt.Errorf("provisioned data sources: %w", err)
	}
	if cfg.QueryAPIEnabled {
		log.Info("query API enabled", "addr", cfg.HTTPAddr, "datasources", len(instances))
		if cfg.QueryAPIAllowRemote && !config.IsLoopbackAddr(cfg.HTTPAddr) {
			log.Warn("query API is reachable from the network WITHOUT authentication (queryApiAllowRemote=true); "+
				"anyone who can reach this address can read every exposed file root", "addr", cfg.HTTPAddr)
		}
	}
	return svc, nil
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	level, _ := cfg.SlogLevel() // validated by config.Load
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(w, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(httpx.ContextHandler{Handler: h}).With("service", "dv")
}
