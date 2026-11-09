package builtin

import (
	"testing"

	"github.com/yesoreyeram/dv/internal/fsroot"
	"github.com/yesoreyeram/dv/internal/plugin"
)

func TestRegister(t *testing.T) {
	roots, err := fsroot.Open(nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := plugin.NewRegistry()
	if err := Register(reg, Options{Roots: roots, FileMaxBytes: 1 << 20}); err != nil {
		t.Fatal(err)
	}
	c := reg.Catalog()
	if len(c.Sources) != 1 || c.Sources[0].ID != "file" || !sameIDs(c.Formats, "csv", "json", "ndjson", "tsv") {
		t.Fatalf("catalog = %+v", c)
	}
	// Registering twice fails on the duplicate format.
	if err := Register(reg, Options{Roots: roots, FileMaxBytes: 1 << 20}); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if err := Register(plugin.NewRegistry(), Options{Roots: roots}); err == nil {
		t.Fatal("missing size limit accepted")
	}
}

func sameIDs(fs []plugin.FormatMeta, ids ...string) bool {
	if len(fs) != len(ids) {
		return false
	}
	for i, f := range fs {
		if f.ID != ids[i] {
			return false
		}
	}
	return true
}
