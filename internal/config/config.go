// Package config loads and validates server configuration.
//
// Precedence (NFR-PORT-002): built-in defaults < JSON config file (DV_CONFIG_FILE) <
// environment variables. Invalid configuration fails fast at start-up (NFR-SEC-009).
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Duration is a time.Duration that (un)marshals from/to a Go duration string ("15s").
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"15s\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Std returns the value as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Config is the complete server configuration.
type Config struct {
	HTTPAddr  string `json:"httpAddr"`
	LogLevel  string `json:"logLevel"`
	LogFormat string `json:"logFormat"`
	// WebDir is the directory containing the built SPA. Empty disables UI serving.
	WebDir string `json:"webDir"`

	RateLimitRPS   float64 `json:"rateLimitRps"`
	RateLimitBurst int     `json:"rateLimitBurst"`

	ReadHeaderTimeout     Duration `json:"readHeaderTimeout"`
	ReadTimeout           Duration `json:"readTimeout"`
	WriteTimeout          Duration `json:"writeTimeout"`
	IdleTimeout           Duration `json:"idleTimeout"`
	ShutdownTimeout       Duration `json:"shutdownTimeout"`
	ReadinessCheckTimeout Duration `json:"readinessCheckTimeout"`
	MaxHeaderBytes        int      `json:"maxHeaderBytes"`
	MaxBodyBytes          int64    `json:"maxBodyBytes"`

	// FileRoots maps root names to absolute directories the file source may read
	// (FR-011 AC1). Empty disables file access.
	FileRoots map[string]string `json:"fileRoots"`
	// FileMaxBytes caps the size of a file read by the file source (FR-011 AC4).
	FileMaxBytes int64 `json:"fileMaxBytes"`
}

// Default returns the secure-by-default configuration.
func Default() Config {
	return Config{
		HTTPAddr:              ":8080",
		LogLevel:              "info",
		LogFormat:             "json",
		RateLimitRPS:          20,
		RateLimitBurst:        40,
		ReadHeaderTimeout:     Duration(5 * time.Second),
		ReadTimeout:           Duration(30 * time.Second),
		WriteTimeout:          Duration(60 * time.Second),
		IdleTimeout:           Duration(120 * time.Second),
		ShutdownTimeout:       Duration(15 * time.Second),
		ReadinessCheckTimeout: Duration(2 * time.Second),
		MaxHeaderBytes:        64 << 10,
		MaxBodyBytes:          1 << 20,
		FileMaxBytes:          64 << 20,
	}
}

// Source abstracts the process environment and file system for testability.
type Source struct {
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
}

// OSSource reads from the real environment and file system.
func OSSource() Source { return Source{Getenv: os.Getenv, ReadFile: os.ReadFile} }

// Load builds the configuration from defaults, an optional file and the environment.
func Load(src Source) (Config, error) {
	cfg := Default()
	if path := strings.TrimSpace(src.Getenv("DV_CONFIG_FILE")); path != "" {
		b, err := src.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("parse config file %q: %w", path, err)
		}
	}
	if err := applyEnv(&cfg, src.Getenv); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config, getenv func(string) string) error {
	var errs []error
	str := func(key string, dst *string) {
		if v, ok := lookup(getenv, key); ok {
			*dst = v
		}
	}
	dur := func(key string, dst *Duration) {
		if v, ok := lookup(getenv, key); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return
			}
			*dst = Duration(d)
		}
	}
	integer := func(key string, dst *int) {
		if v, ok := lookup(getenv, key); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
				return
			}
			*dst = n
		}
	}
	str("DV_HTTP_ADDR", &cfg.HTTPAddr)
	str("DV_LOG_LEVEL", &cfg.LogLevel)
	str("DV_LOG_FORMAT", &cfg.LogFormat)
	str("DV_WEB_DIR", &cfg.WebDir)
	if v, ok := lookup(getenv, "DV_RATE_LIMIT_RPS"); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("DV_RATE_LIMIT_RPS: %w", err))
		} else {
			cfg.RateLimitRPS = f
		}
	}
	integer("DV_RATE_LIMIT_BURST", &cfg.RateLimitBurst)
	dur("DV_READ_HEADER_TIMEOUT", &cfg.ReadHeaderTimeout)
	dur("DV_READ_TIMEOUT", &cfg.ReadTimeout)
	dur("DV_WRITE_TIMEOUT", &cfg.WriteTimeout)
	dur("DV_IDLE_TIMEOUT", &cfg.IdleTimeout)
	dur("DV_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout)
	dur("DV_READINESS_CHECK_TIMEOUT", &cfg.ReadinessCheckTimeout)
	integer("DV_MAX_HEADER_BYTES", &cfg.MaxHeaderBytes)
	if v, ok := lookup(getenv, "DV_MAX_BODY_BYTES"); ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("DV_MAX_BODY_BYTES: %w", err))
		} else {
			cfg.MaxBodyBytes = n
		}
	}
	if v, ok := lookup(getenv, "DV_FILE_ROOTS"); ok {
		roots, err := parseRoots(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("DV_FILE_ROOTS: %w", err))
		} else {
			cfg.FileRoots = roots
		}
	}
	if v, ok := lookup(getenv, "DV_FILE_MAX_BYTES"); ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("DV_FILE_MAX_BYTES: %w", err))
		} else {
			cfg.FileMaxBytes = n
		}
	}
	return errors.Join(errs...)
}

// parseRoots parses "name=/abs/dir,other=/dir2". It replaces any roots from the file.
func parseRoots(v string) (map[string]string, error) {
	roots := map[string]string{}
	for _, entry := range strings.Split(v, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, dir, ok := strings.Cut(entry, "=")
		name, dir = strings.TrimSpace(name), strings.TrimSpace(dir)
		if !ok || name == "" || dir == "" {
			return nil, fmt.Errorf("entry %q must be name=/absolute/dir", entry)
		}
		if _, dup := roots[name]; dup {
			return nil, fmt.Errorf("root %q is listed twice", name)
		}
		roots[name] = dir
	}
	return roots, nil
}

func lookup(getenv func(string) string, key string) (string, bool) {
	v := strings.TrimSpace(getenv(key))
	return v, v != ""
}

// Validate checks every field and returns all problems at once.
func (c Config) Validate() error {
	var errs []error
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("httpAddr %q: %w", c.HTTPAddr, err))
	}
	if _, err := c.SlogLevel(); err != nil {
		errs = append(errs, err)
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("logFormat %q: must be json or text", c.LogFormat))
	}
	if c.RateLimitRPS < 0 {
		errs = append(errs, errors.New("rateLimitRps must be >= 0 (0 disables rate limiting)"))
	}
	if c.RateLimitRPS > 0 && c.RateLimitBurst < 1 {
		errs = append(errs, errors.New("rateLimitBurst must be >= 1 when rate limiting is enabled"))
	}
	for _, d := range []struct {
		name string
		v    Duration
	}{
		{"readHeaderTimeout", c.ReadHeaderTimeout},
		{"readTimeout", c.ReadTimeout},
		{"writeTimeout", c.WriteTimeout},
		{"idleTimeout", c.IdleTimeout},
		{"shutdownTimeout", c.ShutdownTimeout},
		{"readinessCheckTimeout", c.ReadinessCheckTimeout},
	} {
		if d.v <= 0 {
			errs = append(errs, fmt.Errorf("%s must be > 0", d.name))
		}
	}
	if c.MaxHeaderBytes < 1<<10 || c.MaxHeaderBytes > 1<<20 {
		errs = append(errs, errors.New("maxHeaderBytes must be between 1KiB and 1MiB"))
	}
	if c.MaxBodyBytes < 1<<10 || c.MaxBodyBytes > 64<<20 {
		errs = append(errs, errors.New("maxBodyBytes must be between 1KiB and 64MiB"))
	}
	if c.WebDir != "" {
		fi, err := os.Stat(c.WebDir)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("webDir: %w", err))
		case !fi.IsDir():
			errs = append(errs, fmt.Errorf("webDir %q is not a directory", c.WebDir))
		}
	}
	if c.FileMaxBytes < 1<<10 || c.FileMaxBytes > 1<<30 {
		errs = append(errs, errors.New("fileMaxBytes must be between 1KiB and 1GiB"))
	}
	errs = append(errs, validateRoots(c.FileRoots)...)
	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return nil
}

// rootName mirrors fsroot.ValidName; config must not import plugin packages.
var rootName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func validateRoots(roots map[string]string) []error {
	names := make([]string, 0, len(roots))
	for n := range roots {
		names = append(names, n)
	}
	sort.Strings(names)
	var errs []error
	for _, name := range names {
		dir := roots[name]
		if !rootName.MatchString(name) {
			errs = append(errs, fmt.Errorf("fileRoots: name %q must match %s", name, rootName))
			continue
		}
		if !filepath.IsAbs(dir) {
			errs = append(errs, fmt.Errorf("fileRoots.%s: %q must be an absolute path", name, dir))
			continue
		}
		fi, err := os.Stat(dir)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("fileRoots.%s: %w", name, err))
		case !fi.IsDir():
			errs = append(errs, fmt.Errorf("fileRoots.%s: %q is not a directory", name, dir))
		}
	}
	return errs
}

// SlogLevel parses LogLevel.
func (c Config) SlogLevel() (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return 0, fmt.Errorf("logLevel %q: must be debug, info, warn or error", c.LogLevel)
	}
	return l, nil
}
