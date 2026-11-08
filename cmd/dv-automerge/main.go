// Command dv-automerge merges a pull request once its CI run is green (NFR-MAINT-006).
//
// It runs from .github/workflows/automerge.yml on `workflow_run` events, so the code
// executed is always the default branch's copy, never the pull request's.
//
// Environment (set by GitHub Actions unless noted):
//
//	GITHUB_EVENT_PATH            workflow_run payload
//	GITHUB_REPOSITORY            owner/name
//	GITHUB_API_URL               defaults to https://api.github.com
//	GITHUB_TOKEN                 repository token (never logged)
//	DV_AUTOMERGE_AUTHORS         comma-separated allow-list (required)
//	DV_AUTOMERGE_DEFAULT_BRANCH  defaults to main
//	DV_AUTOMERGE_METHOD          squash (default), merge or rebase
//	DV_AUTOMERGE_DRY_RUN         "true" logs decisions without merging
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yesoreyeram/dv/internal/automerge"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log, os.Getenv); err != nil {
		log.Error("automerge failed", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, getenv func(string) string) error {
	cfg, err := configFrom(getenv)
	if err != nil {
		return err
	}
	f, err := os.Open(getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return fmt.Errorf("open event: %w", err)
	}
	defer func() { _ = f.Close() }()
	ev, err := automerge.ParseEvent(f)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	cfg.Log = log
	_, err = cfg.Run(ctx, ev)
	return err
}

func configFrom(getenv func(string) string) (*automerge.Runner, error) {
	repo := getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return nil, errors.New("GITHUB_REPOSITORY is required")
	}
	authors := automerge.ParseAuthors(getenv("DV_AUTOMERGE_AUTHORS"))
	if len(authors) == 0 {
		return nil, errors.New("DV_AUTOMERGE_AUTHORS is required (deny by default)")
	}
	api := getenv("GITHUB_API_URL")
	if api == "" {
		api = "https://api.github.com"
	}
	branch := getenv("DV_AUTOMERGE_DEFAULT_BRANCH")
	if branch == "" {
		branch = "main"
	}
	method := getenv("DV_AUTOMERGE_METHOD")
	switch method {
	case "", "squash", "merge", "rebase":
	default:
		return nil, fmt.Errorf("DV_AUTOMERGE_METHOD %q is not squash, merge or rebase", method)
	}
	return &automerge.Runner{
		API:         api,
		Token:       getenv("GITHUB_TOKEN"),
		HTTP:        &http.Client{Timeout: automerge.DefaultTimeout},
		Policy:      automerge.Policy{Repo: repo, DefaultBranch: branch, Authors: authors},
		MergeMethod: method,
		DryRun:      getenv("DV_AUTOMERGE_DRY_RUN") == "true",
	}, nil
}
