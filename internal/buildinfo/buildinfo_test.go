package buildinfo

import (
	"runtime"
	"testing"
)

func TestGetDefaults(t *testing.T) {
	info := Get()
	if info.Version != "dev" {
		t.Fatalf("version = %q, want dev", info.Version)
	}
	if info.GoVersion != runtime.Version() {
		t.Fatalf("goVersion = %q", info.GoVersion)
	}
}
