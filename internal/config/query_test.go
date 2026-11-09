package config

import (
	"strings"
	"testing"
	"time"
)

func TestQueryDefaults(t *testing.T) {
	d := Default()
	if d.QueryAPIEnabled || d.QueryAPIAllowRemote {
		t.Fatal("query API must be off by default (NFR-SEC-016 AC1)")
	}
	if d.QueryTimeout.Std() != 30*time.Second || d.QueryMaxRows != 100_000 || d.QueryMaxBytes != 32<<20 {
		t.Fatalf("%+v", d)
	}
}

func TestQueryEnvOverrides(t *testing.T) {
	cfg, err := Load(src(map[string]string{
		"DV_HTTP_ADDR": "127.0.0.1:9000", "DV_QUERY_API_ENABLED": "true", "DV_QUERY_API_ALLOW_REMOTE": "1",
		"DV_QUERY_TIMEOUT": "5s", "DV_QUERY_MAX_ROWS": "10", "DV_QUERY_MAX_BYTES": "2048",
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.QueryAPIEnabled || !cfg.QueryAPIAllowRemote || cfg.QueryTimeout.Std() != 5*time.Second || cfg.QueryMaxRows != 10 || cfg.QueryMaxBytes != 2048 {
		t.Fatalf("%+v", cfg)
	}
}

func TestQueryEnvParseErrors(t *testing.T) {
	for k, v := range map[string]string{
		"DV_QUERY_API_ENABLED": "maybe", "DV_QUERY_API_ALLOW_REMOTE": "yes please", "DV_QUERY_TIMEOUT": "soon",
		"DV_QUERY_MAX_ROWS": "many", "DV_QUERY_MAX_BYTES": "lots",
	} {
		if _, err := Load(src(map[string]string{k: v}, nil)); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s: %v", k, v, err)
		}
	}
}

func TestQueryLimitBounds(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"rows 0":      func(c *Config) { c.QueryMaxRows = 0 },
		"rows high":   func(c *Config) { c.QueryMaxRows = MaxQueryRows + 1 },
		"bytes low":   func(c *Config) { c.QueryMaxBytes = 1023 },
		"bytes high":  func(c *Config) { c.QueryMaxBytes = MaxQueryBytes + 1 },
		"timeout 0":   func(c *Config) { c.QueryTimeout = 0 },
		"timeout big": func(c *Config) { c.QueryTimeout = Duration(MaxQueryTimeout + time.Second) },
	} {
		c := Default()
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	c := Default()
	c.QueryMaxRows, c.QueryMaxBytes, c.QueryTimeout = MaxQueryRows, MaxQueryBytes, Duration(MaxQueryTimeout)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true, "127.1.2.3:1": true, "[::1]:8080": true, "localhost:80": true, "LOCALHOST:80": true,
		":8080": false, "0.0.0.0:8080": false, "[::]:8080": false, "10.0.0.5:8080": false, "example.com:80": false,
		"localhost.evil.com:80": false, "garbage": false,
	} {
		if got := IsLoopbackAddr(addr); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}

func TestQueryAPIGate(t *testing.T) {
	c := Default() // ":8080"
	c.QueryAPIEnabled = true
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "queryApiAllowRemote") {
		t.Fatalf("non-loopback bind accepted: %v", err)
	}
	c.QueryAPIAllowRemote = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.QueryAPIAllowRemote, c.HTTPAddr = false, "127.0.0.1:8080"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionedDataSources(t *testing.T) {
	file := `{"datasources":[{"uid":"files","type":"file","name":"Sample files","jsonData":{"root":"samples"}}]}`
	cfg, err := Load(src(map[string]string{"DV_CONFIG_FILE": "c.json"}, map[string]string{"c.json": file}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DataSources) != 1 || cfg.DataSources[0].UID != "files" || string(cfg.DataSources[0].JSONData) != `{"root":"samples"}` {
		t.Fatalf("%+v", cfg.DataSources)
	}
	for entries, want := range map[string]string{
		`{"uid":"bad uid","type":"file","name":"x"}`:                                  "uid",
		`{"uid":"` + strings.Repeat("u", 41) + `","type":"file","name":"x"}`:          "uid",
		`{"uid":"a","type":"file","name":"x"},{"uid":"a","type":"file","name":"y"}`:   "duplicate",
		`{"uid":"a","type":"file","name":""}`:                                         "name",
		`{"uid":"a","type":"file","name":"   "}`:                                      "name",
		`{"uid":"a","type":"file","name":"` + strings.Repeat("n", 101) + `"}`:         "name",
		`{"uid":"a","type":"","name":"x"}`:                                            "type",
		`{"uid":"a","type":"File","name":"x"}`:                                        "type",
		`{"uid":"a","type":"file","name":"x","secureJsonData":{"password":"hunter"}}`: "secureJsonData",
	} {
		_, err := Load(src(map[string]string{"DV_CONFIG_FILE": "c.json"}, map[string]string{"c.json": `{"datasources":[` + entries + `]}`}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", entries, err)
		}
		if err != nil && strings.Contains(err.Error(), "hunter") {
			t.Errorf("secret echoed in error: %v", err)
		}
	}
}
