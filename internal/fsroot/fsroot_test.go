package fsroot

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openRoots(t *testing.T) (*Roots, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := Open(map[string]string{"data": dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, dir
}

func TestCleanPath(t *testing.T) {
	ok := map[string]string{
		"a.csv":         "a.csv",
		"dir/a.csv":     "dir/a.csv",
		"./dir//a.csv":  "dir/a.csv",
		"dir/./a.csv":   "dir/a.csv",
		"ünïcødé.csv":   "ünïcødé.csv",
		"dir/a..b.csv":  "dir/a..b.csv",
		"...csv":        "...csv",
		"trailing/x/":   "trailing/x",
		"spaces in.csv": "spaces in.csv",
	}
	for in, want := range ok {
		got, err := CleanPath(in)
		if err != nil || got != want {
			t.Errorf("CleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"", ".", "./", "/etc/passwd", "../x", "a/../../x", "a/..", "..",
		`a\..\x`, `C:\x`, "a\x00b", "a\nb", "a\x7fb", "bad\xffutf8",
		strings.Repeat("a", MaxPathBytes+1),
	}
	for _, in := range bad {
		if _, err := CleanPath(in); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanPath(%q) err = %v, want ErrInvalidPath", in, err)
		}
	}
}

func TestOpenValidation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	write(t, file, "x")
	cases := map[string]map[string]string{
		"bad name":     {"Bad": dir},
		"relative":     {"data": "relative/dir"},
		"missing":      {"data": filepath.Join(dir, "missing")},
		"file as root": {"data": file},
	}
	for name, cfg := range cases {
		if _, err := Open(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// A failure after a successful root closes the opened ones and still errors.
	if _, err := Open(map[string]string{"a": dir, "b": "rel"}); err == nil {
		t.Fatal("expected error")
	}
	r, err := Open(nil)
	if err != nil || len(r.Names()) != 0 {
		t.Fatalf("empty roots: %v %v", r, err)
	}
	if err := r.ReadinessCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOpenReadsRegularFiles(t *testing.T) {
	r, dir := openRoots(t)
	write(t, filepath.Join(dir, "sub", "a.csv"), "a,b\n1,2\n")
	if !r.Has("data") || r.Has("other") || strings.Join(r.Names(), ",") != "data" {
		t.Fatal("names")
	}
	f, err := r.Open("data", "sub/a.csv", 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "a,b\n1,2\n" || f.Size != 8 {
		t.Fatalf("read %q %v size %d", b, err, f.Size)
	}
	// Unlimited size.
	f2, err := r.Open("data", "sub/a.csv", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = f2.Close()
}

func TestOpenErrors(t *testing.T) {
	r, dir := openRoots(t)
	outside := filepath.Join(t.TempDir(), "secret.csv")
	write(t, outside, "secret")
	write(t, filepath.Join(dir, "big.csv"), strings.Repeat("x", 100))
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.csv")); err != nil {
		t.Fatal(err)
	}
	// The escape target must exist: some Go versions report a missing target as
	// "not found" before noticing the escape.
	write(t, filepath.Join(filepath.Dir(dir), "parent-secret.csv"), "secret")
	if err := os.Symlink("../", filepath.Join(dir, "up")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "inner", "ok.csv"), "ok")
	if err := os.Symlink("inner/ok.csv", filepath.Join(dir, "inside.csv")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		root string
		path string
		want error
	}{
		{"unknown root", "nope", "a.csv", ErrUnknownRoot},
		{"traversal", "data", "../secret.csv", ErrInvalidPath},
		{"absolute", "data", outside, ErrInvalidPath},
		{"symlink escape", "data", "escape.csv", ErrInvalidPath},
		{"symlink dir escape", "data", "up/parent-secret.csv", ErrInvalidPath},
		{"missing", "data", "missing.csv", ErrNotFound},
		{"directory", "data", "folder", ErrNotRegular},
		{"too large", "data", "big.csv", ErrTooLarge},
	}
	for _, c := range cases {
		_, err := r.Open(c.root, c.path, 64)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
			continue
		}
		if strings.Contains(err.Error(), dir) {
			t.Errorf("%s: error %q reveals the root directory", c.name, err)
		}
	}
	// A symlink that stays inside the root is allowed.
	f, err := r.Open("data", "inside.csv", 64)
	if err != nil {
		t.Fatalf("inside symlink: %v", err)
	}
	_ = f.Close()
}

func TestOpenRejectsFIFOWithoutBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no FIFOs on windows")
	}
	r, dir := openRoots(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.csv"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if _, err := r.Open("data", "pipe.csv", 64); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenPermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	r, dir := openRoots(t)
	p := filepath.Join(dir, "locked.csv")
	write(t, p, "x")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Open("data", "locked.csv", 64); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCapReaderDetectsGrowth(t *testing.T) {
	r, dir := openRoots(t)
	p := filepath.Join(dir, "grow.csv")
	write(t, p, "12345")
	f, err := r.Open("data", "grow.csv", 8)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	// The file grows after the size check passed.
	if err := os.WriteFile(p, []byte("1234567890"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if !errors.Is(err, ErrTooLarge) || len(b) != 8 {
		t.Fatalf("read %d bytes, err = %v", len(b), err)
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "root")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	r, err := Open(map[string]string{"data": sub})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if err := r.Check("data"); err != nil {
		t.Fatal(err)
	}
	if err := r.Check("nope"); !errors.Is(err, ErrUnknownRoot) {
		t.Fatalf("err = %v", err)
	}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	err = r.ReadinessCheck(context.Background())
	if err == nil || strings.Contains(err.Error(), dir) {
		t.Fatalf("readiness after removal: %v", err)
	}
}

func TestScrubAndClassify(t *testing.T) {
	plain := errors.New("boom")
	if got := scrub(plain); !errors.Is(got, plain) || got.Error() != "boom" {
		t.Fatal("non-path errors are unchanged")
	}
	pe := &os.PathError{Op: "open", Path: "/abs/secret", Err: plain}
	if s := scrub(pe).Error(); strings.Contains(s, "/abs") || !errors.Is(scrub(pe), plain) {
		t.Fatalf("scrub = %q", s)
	}
	if got := classify("x", pe); strings.Contains(got.Error(), "/abs") {
		t.Fatalf("classify = %q", got)
	}
	if isEscape(nil) {
		t.Fatal("nil is not an escape")
	}
}
