package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func src(env map[string]string, files map[string]string) Source {
	return Source{
		Getenv: func(k string) string { return env[k] },
		ReadFile: func(p string) ([]byte, error) {
			if v, ok := files[p]; ok {
				return []byte(v), nil
			}
			return nil, fs.ErrNotExist
		},
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(src(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("got %+v, want defaults", cfg)
	}
	if lvl, _ := cfg.SlogLevel(); lvl != slog.LevelInfo {
		t.Fatalf("level = %v", lvl)
	}
}

func TestPrecedenceEnvOverFileOverDefault(t *testing.T) {
	files := map[string]string{"/etc/dv.json": `{"httpAddr":":9000","logLevel":"debug","shutdownTimeout":"3s","rateLimitRps":5}`}
	env := map[string]string{"DV_CONFIG_FILE": "/etc/dv.json", "DV_HTTP_ADDR": "127.0.0.1:9100", "DV_RATE_LIMIT_BURST": "7"}
	cfg, err := Load(src(env, files))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9100" {
		t.Errorf("env should win: %q", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" || cfg.ShutdownTimeout.Std() != 3*time.Second || cfg.RateLimitRPS != 5 {
		t.Errorf("file values not applied: %+v", cfg)
	}
	if cfg.RateLimitBurst != 7 {
		t.Errorf("burst = %d", cfg.RateLimitBurst)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("default not kept: %q", cfg.LogFormat)
	}
}

func TestAllEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{
		"DV_LOG_FORMAT": "text", "DV_WEB_DIR": dir, "DV_RATE_LIMIT_RPS": "1.5",
		"DV_READ_HEADER_TIMEOUT": "1s", "DV_READ_TIMEOUT": "2s", "DV_WRITE_TIMEOUT": "3s",
		"DV_IDLE_TIMEOUT": "4s", "DV_READINESS_CHECK_TIMEOUT": "500ms",
		"DV_MAX_HEADER_BYTES": "2048", "DV_MAX_BODY_BYTES": "4096",
	}
	cfg, err := Load(src(env, nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogFormat != "text" || cfg.WebDir != dir || cfg.RateLimitRPS != 1.5 ||
		cfg.ReadHeaderTimeout.Std() != time.Second || cfg.ReadTimeout.Std() != 2*time.Second ||
		cfg.WriteTimeout.Std() != 3*time.Second || cfg.IdleTimeout.Std() != 4*time.Second ||
		cfg.ReadinessCheckTimeout.Std() != 500*time.Millisecond ||
		cfg.MaxHeaderBytes != 2048 || cfg.MaxBodyBytes != 4096 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		files map[string]string
		want  string
	}{
		{"missing file", map[string]string{"DV_CONFIG_FILE": "/nope.json"}, nil, "read config file"},
		{"unknown field", map[string]string{"DV_CONFIG_FILE": "/c.json"}, map[string]string{"/c.json": `{"httpAdr":":1"}`}, "unknown field"},
		{"bad file duration", map[string]string{"DV_CONFIG_FILE": "/c.json"}, map[string]string{"/c.json": `{"idleTimeout":"soon"}`}, "parse config file"},
		{"non-string duration", map[string]string{"DV_CONFIG_FILE": "/c.json"}, map[string]string{"/c.json": `{"idleTimeout":5}`}, "duration must be a string"},
		{"bad env duration", map[string]string{"DV_SHUTDOWN_TIMEOUT": "x"}, nil, "DV_SHUTDOWN_TIMEOUT"},
		{"bad env int", map[string]string{"DV_RATE_LIMIT_BURST": "many"}, nil, "DV_RATE_LIMIT_BURST"},
		{"bad env float", map[string]string{"DV_RATE_LIMIT_RPS": "fast"}, nil, "DV_RATE_LIMIT_RPS"},
		{"bad env int64", map[string]string{"DV_MAX_BODY_BYTES": "big"}, nil, "DV_MAX_BODY_BYTES"},
		{"bad addr", map[string]string{"DV_HTTP_ADDR": "8080"}, nil, "httpAddr"},
		{"bad level", map[string]string{"DV_LOG_LEVEL": "loud"}, nil, "logLevel"},
		{"bad format", map[string]string{"DV_LOG_FORMAT": "xml"}, nil, "logFormat"},
		{"negative rps", map[string]string{"DV_RATE_LIMIT_RPS": "-1"}, nil, "rateLimitRps"},
		{"zero burst", map[string]string{"DV_RATE_LIMIT_BURST": "0"}, nil, "rateLimitBurst"},
		{"huge burst", map[string]string{"DV_RATE_LIMIT_BURST": "1000001"}, nil, "rateLimitBurst"},
		{"NaN rps", map[string]string{"DV_RATE_LIMIT_RPS": "NaN"}, nil, "rateLimitRps"},
		{"Inf rps", map[string]string{"DV_RATE_LIMIT_RPS": "+Inf"}, nil, "rateLimitRps"},
		{"-Inf rps", map[string]string{"DV_RATE_LIMIT_RPS": "-Inf"}, nil, "rateLimitRps"},
		{"tiny rps", map[string]string{"DV_RATE_LIMIT_RPS": "1e-300"}, nil, "rateLimitRps"},
		{"huge rps", map[string]string{"DV_RATE_LIMIT_RPS": "1e7"}, nil, "rateLimitRps"},
		{"zero timeout", map[string]string{"DV_IDLE_TIMEOUT": "0s"}, nil, "idleTimeout"},
		{"header bytes", map[string]string{"DV_MAX_HEADER_BYTES": "10"}, nil, "maxHeaderBytes"},
		{"body bytes", map[string]string{"DV_MAX_BODY_BYTES": "999999999"}, nil, "maxBodyBytes"},
		{"web dir missing", map[string]string{"DV_WEB_DIR": "/definitely/not/here"}, nil, "webDir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(src(tt.env, tt.files))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestWebDirMustBeDirectory(t *testing.T) {
	cfg := Default()
	cfg.WebDir = "config_test.go"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v", err)
	}
}

func TestRateLimitDisabledAllowsZeroBurst(t *testing.T) {
	cfg := Default()
	cfg.RateLimitRPS, cfg.RateLimitBurst = 0, 0
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimitBoundsAreInclusive(t *testing.T) {
	for _, tc := range []struct {
		rps   float64
		burst int
	}{{MinRateLimitRPS, 1}, {MaxRateLimitRPS, MaxRateLimitBurst}} {
		cfg := Default()
		cfg.RateLimitRPS, cfg.RateLimitBurst = tc.rps, tc.burst
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%v/%d: %v", tc.rps, tc.burst, err)
		}
	}
}

func TestDurationJSONRoundTrip(t *testing.T) {
	b, err := json.Marshal(Duration(90 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"1m30s"` {
		t.Fatalf("marshal = %s", b)
	}
	var d Duration
	if err := json.Unmarshal(b, &d); err != nil || d.Std() != 90*time.Second {
		t.Fatalf("unmarshal = %v %v", d, err)
	}
}

func TestOSSource(t *testing.T) {
	s := OSSource()
	t.Setenv("DV_TEST_ONLY", "x")
	if s.Getenv("DV_TEST_ONLY") != "x" {
		t.Fatal("getenv not wired")
	}
	if _, err := s.ReadFile("/definitely/missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readfile err = %v", err)
	}
}

func TestFileRoots(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	files := map[string]string{"/etc/dv.json": `{"fileRoots":{"fromfile":"` + filepath.ToSlash(dir) + `"},"fileMaxBytes":2048}`}
	cfg, err := Load(src(map[string]string{"DV_CONFIG_FILE": "/etc/dv.json"}, files))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FileRoots["fromfile"] != filepath.ToSlash(dir) || cfg.FileMaxBytes != 2048 {
		t.Fatalf("file: %+v", cfg)
	}
	// The environment replaces the file's roots.
	env := map[string]string{"DV_CONFIG_FILE": "/etc/dv.json", "DV_FILE_ROOTS": " a = " + dir + " , ,b=" + other, "DV_FILE_MAX_BYTES": "4096"}
	cfg, err = Load(src(env, files))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.FileRoots, map[string]string{"a": dir, "b": other}) || cfg.FileMaxBytes != 4096 {
		t.Fatalf("env: %+v", cfg)
	}
}

func TestFileRootErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ env, want string }{
		"syntax":     {"DV_FILE_ROOTS=novalue", "name=/absolute/dir"},
		"empty name": {"DV_FILE_ROOTS==" + dir, "name=/absolute/dir"},
		"duplicate":  {"DV_FILE_ROOTS=a=" + dir + ",a=" + dir, "twice"},
		"bad name":   {"DV_FILE_ROOTS=Bad=" + dir, "must match"},
		"relative":   {"DV_FILE_ROOTS=a=rel/dir", "absolute"},
		"missing":    {"DV_FILE_ROOTS=a=" + filepath.Join(dir, "nope"), "fileRoots.a"},
		"not a dir":  {"DV_FILE_ROOTS=a=" + file, "not a directory"},
		"max bytes":  {"DV_FILE_MAX_BYTES=abc", "DV_FILE_MAX_BYTES"},
		"max small":  {"DV_FILE_MAX_BYTES=10", "fileMaxBytes"},
	}
	for name, c := range cases {
		k, v, _ := strings.Cut(c.env, "=")
		_, err := Load(src(map[string]string{k: v}, nil))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}
