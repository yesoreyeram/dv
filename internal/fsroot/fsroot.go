// Package fsroot provides the host-owned, sandboxed file-system roots that file-based
// plugins read from (ADR-0002, FR-011, NFR-SEC-006).
//
// Operators configure named directories. Each is opened once as an os.Root, which
// resolves every path component inside the directory and refuses ".." and symlink
// escapes. On top of that, CleanPath rejects suspicious paths before any file-system
// call, and Open only returns regular files whose size is within a cap. Errors never
// contain the absolute directory of a root.
package fsroot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// MaxPathBytes bounds the length of a path inside a root (FR-011 AC3).
const MaxPathBytes = 1024

// Error kinds. Callers match them with errors.Is; every returned error wraps exactly one.
var (
	ErrInvalidPath = errors.New("invalid path")
	ErrNotFound    = errors.New("not found")
	ErrNotRegular  = errors.New("not a regular file")
	ErrTooLarge    = errors.New("too large")
	ErrUnknownRoot = errors.New("unknown file root")
)

// namePattern constrains root names; they appear in configs, logs and executed queries.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// ValidName reports whether name is an acceptable root name.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// Roots is a fixed set of named, opened roots. It is safe for concurrent use: os.Root
// is, and the map is never modified after Open.
type Roots struct {
	roots map[string]*os.Root
	dirs  map[string]string // absolute paths; never included in errors
}

// Open opens every configured directory. dirs maps root names to absolute directory
// paths. On error, any roots opened so far are closed.
func Open(dirs map[string]string) (*Roots, error) {
	r := &Roots{roots: make(map[string]*os.Root, len(dirs)), dirs: make(map[string]string, len(dirs))}
	for _, name := range sortedKeys(dirs) {
		dir := dirs[name]
		if !ValidName(name) {
			_ = r.Close()
			return nil, fmt.Errorf("file root %q: name must match %s", name, namePattern)
		}
		if !filepath.IsAbs(dir) {
			_ = r.Close()
			return nil, fmt.Errorf("file root %q: directory must be an absolute path", name)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			_ = r.Close()
			return nil, fmt.Errorf("file root %q: %w", name, err)
		}
		r.roots[name] = root
		r.dirs[name] = dir
	}
	return r, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Names returns the sorted root names.
func (r *Roots) Names() []string {
	names := make([]string, 0, len(r.roots))
	for n := range r.roots {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Has reports whether a root with the given name exists.
func (r *Roots) Has(name string) bool {
	_, ok := r.roots[name]
	return ok
}

// Close closes every root.
func (r *Roots) Close() error {
	var errs []error
	for _, root := range r.roots {
		errs = append(errs, root.Close())
	}
	return errors.Join(errs...)
}

// Check verifies that the named root is still accessible and that its configured
// directory has not been removed, replaced or unmounted since it was opened.
func (r *Roots) Check(name string) error {
	root, ok := r.roots[name]
	if !ok {
		return fmt.Errorf("%q: %w", name, ErrUnknownRoot)
	}
	opened, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("file root %q is not accessible: %w", name, scrub(err))
	}
	current, err := os.Stat(r.dirs[name])
	if err != nil || !os.SameFile(opened, current) {
		return fmt.Errorf("file root %q was removed or replaced since start-up", name)
	}
	return nil
}

// ReadinessCheck verifies every root; register it as the "file-roots" check.
func (r *Roots) ReadinessCheck(context.Context) error {
	var errs []error
	for _, n := range r.Names() {
		errs = append(errs, r.Check(n))
	}
	return errors.Join(errs...)
}

// CleanPath validates a client-supplied path inside a root and returns its clean form.
// It rejects, without touching the file system: empty paths, paths longer than
// MaxPathBytes, absolute paths, ".." segments, backslashes, NUL and other control
// characters, Unicode bidirectional controls, paths that clean to the root itself, and
// hidden entries (any cleaned segment starting with "."), so dotfiles such as .env or
// .git/config in an exposed directory are never read (FR-011 AC3).
func CleanPath(p string) (string, error) {
	switch {
	case p == "":
		return "", fmt.Errorf("path is empty: %w", ErrInvalidPath)
	case len(p) > MaxPathBytes:
		return "", fmt.Errorf("path exceeds %d bytes: %w", MaxPathBytes, ErrInvalidPath)
	case strings.HasPrefix(p, "/"):
		return "", fmt.Errorf("path %q must be relative: %w", p, ErrInvalidPath)
	case strings.ContainsRune(p, '\\'):
		return "", fmt.Errorf("path %q must use '/' as separator: %w", p, ErrInvalidPath)
	}
	for _, c := range p {
		if c == unicode.ReplacementChar || unicode.IsControl(c) || isBidiControl(c) {
			return "", fmt.Errorf("path contains control or invalid characters: %w", ErrInvalidPath)
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("path %q must not contain '..': %w", p, ErrInvalidPath)
		}
	}
	clean := path.Clean(p)
	if clean == "." || !fs.ValidPath(clean) {
		return "", fmt.Errorf("path %q does not name a file: %w", p, ErrInvalidPath)
	}
	// After Clean, "." segments are gone and ".." was rejected above, so a leading dot
	// always marks a hidden entry.
	for _, seg := range strings.Split(clean, "/") {
		if strings.HasPrefix(seg, ".") {
			return "", fmt.Errorf("path %q names a hidden file or directory: %w", p, ErrInvalidPath)
		}
	}
	return clean, nil
}

// isBidiControl reports whether c is a Unicode bidirectional formatting control
// (Bidi_Control property). They let text display in a different order than it is stored,
// so a path could look like another file in errors, executed queries and logs.
func isBidiControl(c rune) bool {
	return unicode.Is(unicode.Bidi_Control, c)
}

// File is an opened regular file whose reads are capped at the size limit.
type File struct {
	io.Reader
	f    *os.File
	Size int64
}

// Close closes the underlying file.
func (f *File) Close() error { return f.f.Close() }

// Open opens the file at p (validated with CleanPath) in the named root. It returns
// ErrNotRegular for directories, devices, FIFOs and sockets and ErrTooLarge when the
// file is larger than maxBytes (> 0). The returned reader fails with ErrTooLarge if the
// file grows beyond maxBytes while it is read.
func (r *Roots) Open(name, p string, maxBytes int64) (*File, error) {
	root, ok := r.roots[name]
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, ErrUnknownRoot)
	}
	clean, err := CleanPath(p)
	if err != nil {
		return nil, err
	}
	// O_NONBLOCK keeps a FIFO from blocking the open; it is rejected below.
	f, err := root.OpenFile(clean, os.O_RDONLY|nonBlock, 0)
	if err != nil {
		return nil, classify(clean, err)
	}
	fi, err := f.Stat() // on the handle: no check-then-open race
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", clean, scrub(err))
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", clean, ErrNotRegular)
	}
	if maxBytes > 0 && fi.Size() > maxBytes {
		_ = f.Close()
		return nil, fmt.Errorf("%s is %d bytes, limit is %d: %w", clean, fi.Size(), maxBytes, ErrTooLarge)
	}
	var rd io.Reader = f
	if maxBytes > 0 {
		rd = &capReader{r: io.LimitReader(f, maxBytes+1), left: maxBytes, path: clean}
	}
	return &File{Reader: rd, f: f, Size: fi.Size()}, nil
}

// classify maps an open error to an error kind without leaking the root directory.
func classify(clean string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s: %w", clean, ErrNotFound)
	case isEscape(err):
		return fmt.Errorf("%s resolves outside the root: %w", clean, ErrInvalidPath)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%s: permission denied: %w", clean, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", clean, scrub(err))
}

// isEscape reports whether os.Root refused the path because it leaves the root. The
// standard library reports this with an unexported error whose text is stable.
func isEscape(err error) bool {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return err != nil && strings.Contains(err.Error(), "escapes from parent")
}

// scrub drops the path from *fs.PathError (os.Root reports relative names only, but this
// keeps any future change from leaking absolute paths).
func scrub(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s failed: %w", pe.Op, pe.Err)
	}
	return err
}

// capReader returns ErrTooLarge once more than left bytes are read. After the cap trips
// it keeps returning (0, that error), so callers that retry (bufio, scanners) never see a
// negative count, which the io.Reader contract forbids.
type capReader struct {
	r    io.Reader
	left int64
	path string
	err  error
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	n, err := c.r.Read(p)
	if int64(n) > c.left {
		n = int(c.left)
		c.left = 0
		c.err = fmt.Errorf("%s grew beyond the size limit while reading: %w", c.path, ErrTooLarge)
		return n, c.err
	}
	c.left -= int64(n)
	return n, err
}
