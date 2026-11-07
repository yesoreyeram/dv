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
	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/health"
	"github.com/yesoreyeram/dv/internal/httpx"
	"github.com/yesoreyeram/dv/internal/plugin"
	"github.com/yesoreyeram/dv/internal/plugins/builtin"
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
	if err := builtin.Register(plugins, builtin.Options{Roots: roots, FileMaxBytes: cfg.FileMaxBytes}); err != nil {
		return err
	}
	plugins.Freeze()
	checks := health.New(cfg.ReadinessCheckTimeout.Std(), log)
	checks.Register("file-roots", roots.ReadinessCheck)
	log.Info("plugins registered", "fileRoots", roots.Names())
	srv, err := server.New(server.Options{Config: cfg, Logger: log, Health: checks, Build: buildinfo.Get(), Plugins: plugins})
	if err != nil {
		return err
	}
	return srv.ListenAndServe(ctx)
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
