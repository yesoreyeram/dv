package config

import (
	"strings"
	"testing"
)

func TestSecretReferences(t *testing.T) {
	file := `{"datasources":[{"uid":"gh","type":"http","name":"GitHub","jsonData":{},
	  "secureJsonData":{"token":{"env":"GH_TOKEN"},"key":{"file":"/run/secrets/key"}}}]}`
	cfg, err := Load(src(map[string]string{"DV_CONFIG_FILE": "c.json"}, map[string]string{"c.json": file}))
	if err != nil {
		t.Fatal(err)
	}
	ds := cfg.DataSources[0]
	got, err := ds.ResolveSecrets(src(map[string]string{"GH_TOKEN": "ghp_x"}, map[string]string{"/run/secrets/key": "k-value\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if got["token"] != "ghp_x" || got["key"] != "k-value" {
		t.Fatalf("resolved = %v", got)
	}
	for name, s := range map[string]Source{
		"missing env":  src(nil, map[string]string{"/run/secrets/key": "k"}),
		"missing file": src(map[string]string{"GH_TOKEN": "x"}, nil),
		"empty file":   src(map[string]string{"GH_TOKEN": "x"}, map[string]string{"/run/secrets/key": "\n"}),
		"too long":     src(map[string]string{"GH_TOKEN": strings.Repeat("s", MaxSecretBytes+1)}, map[string]string{"/run/secrets/key": "k"}),
	} {
		_, err := ds.ResolveSecrets(s)
		if err == nil || !strings.Contains(err.Error(), `datasource "gh"`) || strings.Contains(err.Error(), "sss") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestSecretReferenceValidation(t *testing.T) {
	for entry, want := range map[string]string{
		`"secureJsonData":{"token":"ghp_plaintext"}`:           "{env} or {file} references",
		`"secureJsonData":{"token":42}`:                        "{env} or {file} references",
		`"secureJsonData":{"token":{"value":"ghp_plaintext"}}`: "{env} or {file} references",
		`"secureJsonData":{"token":{}}`:                        "exactly one of env or file",
		`"secureJsonData":{"token":{"env":"A","file":"/b"}}`:   "exactly one of env or file",
		`"secureJsonData":{"token":{"env":"1BAD"}}`:            "not a valid variable name",
		`"secureJsonData":{"token":{"file":"relative/path"}}`:  "absolute path",
		`"secureJsonData":{"bad key!":{"env":"A"}}`:            "secureJsonData key",
	} {
		file := `{"datasources":[{"uid":"gh","type":"http","name":"GitHub",` + entry + `}]}`
		_, err := Load(src(map[string]string{"DV_CONFIG_FILE": "c.json"}, map[string]string{"c.json": file}))
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "ghp_plaintext") {
			t.Errorf("%s: err = %v, want %q", entry, err, want)
		}
	}
}

func TestOutboundAllowedCIDRs(t *testing.T) {
	cfg, err := Load(src(map[string]string{"DV_OUTBOUND_ALLOWED_CIDRS": "127.0.0.1, 10.0.0.0/8"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OutboundAllowedCIDRs) != 2 || cfg.OutboundAllowedCIDRs[1] != "10.0.0.0/8" {
		t.Fatalf("%v", cfg.OutboundAllowedCIDRs)
	}
	if _, err := Load(src(map[string]string{"DV_OUTBOUND_ALLOWED_CIDRS": "nope"}, nil)); err == nil || !strings.Contains(err.Error(), "outboundAllowedCidrs") {
		t.Fatalf("err = %v", err)
	}
}
