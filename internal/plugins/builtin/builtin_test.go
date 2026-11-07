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
	if len(c.Sources) != 1 || c.Sources[0].ID != "file" || len(c.Formats) != 2 || c.Formats[0].ID != "csv" || c.Formats[1].ID != "tsv" {
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
