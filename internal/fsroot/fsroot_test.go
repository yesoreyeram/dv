package fsroot

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unicode"
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
		"trailing/x/":   "trailing/x",
		"spaces in.csv": "spaces in.csv",
		"v1.2/r.final":  "v1.2/r.final",
		"dir./a.csv":    "dir./a.csv",
		// Zero-width (non-)joiners are needed in Persian and Indic names; they are not
		// bidi controls and stay allowed.
		"\u0645\u06cc\u200c\u062e\u0648\u0627\u0645.csv": "\u0645\u06cc\u200c\u062e\u0648\u0627\u0645.csv",
		"\u0915\u094d\u200d\u0937.csv":                   "\u0915\u094d\u200d\u0937.csv",
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

func TestCleanPathRejectsHiddenSegments(t *testing.T) {
	for _, in := range []string{
		".env", ".git/config", "data/.secret.csv", ".aws/credentials.csv", "a/.b/c.csv",
		"./.env", "dir/./.hidden", "...csv", ".hidden/", "a//.x",
	} {
		_, err := CleanPath(in)
		if !errors.Is(err, ErrInvalidPath) || !strings.Contains(err.Error(), "hidden") {
			t.Errorf("CleanPath(%q) err = %v, want a hidden-entry ErrInvalidPath", in, err)
		}
	}
}

func TestCleanPathRejectsBidiControls(t *testing.T) {
	for _, c := range []rune{0x061C, 0x200E, 0x200F, 0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069} {
		in := "invoice" + string(c) + "vsc.exe"
		if _, err := CleanPath(in); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanPath(%+q) err = %v, want ErrInvalidPath", in, err)
		}
	}
}

func TestOpenRefusesExistingHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", ".git/config"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte("token=s3cr3t\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	roots, err := Open(map[string]string{"r": dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = roots.Close() })
	for _, name := range []string{".env", ".git/config", "./.env"} {
		f, err := roots.Open("r", name, 0)
		if err == nil {
			_ = f.Close()
			t.Errorf("Open(%q) succeeded, want ErrInvalidPath", name)
			continue
		}
		if !errors.Is(err, ErrInvalidPath) || strings.Contains(err.Error(), dir) {
			t.Errorf("Open(%q) err = %v, want ErrInvalidPath without the root directory", name, err)
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

// chunkReader returns data in fixed-size chunks, then EOF.
type chunkReader struct {
	data  []byte
	chunk int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), c.chunk, len(c.data))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

func TestCapReaderNeverReturnsNegativeCounts(t *testing.T) {
	c := &capReader{r: &chunkReader{data: []byte("0123456789abcdef"), chunk: 4}, left: 6, path: "x.csv"}
	buf := make([]byte, 4)
	var got []byte
	var errs []error
	for range 5 {
		n, err := c.Read(buf)
		if n < 0 || n > len(buf) {
			t.Fatalf("Read returned n = %d", n)
		}
		got = append(got, buf[:n]...)
		errs = append(errs, err)
	}
	if string(got) != "012345" {
		t.Fatalf("got %q, want the first 6 bytes", got)
	}
	if errs[0] != nil {
		t.Fatalf("first read: %v", errs[0])
	}
	for i, err := range errs[1:] {
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("read %d: err = %v, want ErrTooLarge", i+2, err)
		}
	}
}

func TestCapReaderSurvivesBufferedRetries(t *testing.T) {
	r, dir := openRoots(t)
	p := filepath.Join(dir, "grow.txt")
	write(t, p, "ab")
	f, err := r.Open("data", "grow.txt", 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReaderSize(f, 16)
	for range 3 { // previously panicked with "reader returned negative count from Read"
		if _, err := br.ReadString('\n'); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
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

// FuzzCleanPath pins the invariants of every accepted path: it is a valid fs path (no
// "..", no leading "/", no empty segments), has no hidden segment, no backslash and no
// control or bidi characters, fits MaxPathBytes, and cleaning is idempotent.
func FuzzCleanPath(f *testing.F) {
	for _, s := range []string{"a.csv", "./a/b.csv", ".env", "a/../b", "x\u202e.csv", `a\b`, "a//b/", "...", "ü/ß.tsv"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got, err := CleanPath(in)
		if err != nil {
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("CleanPath(%q) err = %v, want ErrInvalidPath", in, err)
			}
			return
		}
		if !fs.ValidPath(got) || got == "." || len(got) > MaxPathBytes || strings.ContainsRune(got, '\\') {
			t.Fatalf("CleanPath(%q) = %q is not a valid relative path", in, got)
		}
		for _, seg := range strings.Split(got, "/") {
			if strings.HasPrefix(seg, ".") {
				t.Fatalf("CleanPath(%q) = %q has a hidden segment", in, got)
			}
		}
		for _, c := range got {
			if unicode.IsControl(c) || unicode.Is(unicode.Bidi_Control, c) || c == unicode.ReplacementChar {
				t.Fatalf("CleanPath(%q) = %q contains %U", in, got, c)
			}
		}
		if again, err := CleanPath(got); err != nil || again != got {
			t.Fatalf("CleanPath(%q) = %q, %v; not idempotent", got, again, err)
		}
	})
}
