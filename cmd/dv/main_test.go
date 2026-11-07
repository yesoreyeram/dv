package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yesoreyeram/dv/internal/config"
)

func env(m map[string]string) config.Source {
	return config.Source{Getenv: func(k string) string { return m[k] }, ReadFile: func(string) ([]byte, error) { return nil, nil }}
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"version"}, env(nil), &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "dv dev") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestUnknownArg(t *testing.T) {
	if err := run(context.Background(), []string{"serve"}, env(nil), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestInvalidConfig(t *testing.T) {
	err := run(context.Background(), nil, env(map[string]string{"DV_LOG_LEVEL": "loud"}), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "logLevel") {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidWebDir(t *testing.T) {
	err := run(context.Background(), nil, env(map[string]string{"DV_WEB_DIR": t.TempDir(), "DV_HTTP_ADDR": "127.0.0.1:0"}), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunAndShutdown(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		var logs bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := run(ctx, nil, env(map[string]string{"DV_HTTP_ADDR": "127.0.0.1:0", "DV_LOG_FORMAT": format}), &bytes.Buffer{}, &logs)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(logs.String(), "server listening") || !strings.Contains(logs.String(), "server stopped") {
			t.Fatalf("%s logs = %s", format, logs.String())
		}
	}
}
