package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// staticHandler serves the built SPA. Files are read through os.Root so symlinks
// cannot escape the web directory (threat model F-01, E). Unknown extension-less paths
// fall back to index.html for client-side routing; unknown asset paths return 404.
type staticHandler struct {
	fsys  fs.FS
	files http.Handler
}

func newStaticHandler(dir string) (*staticHandler, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open web dir: %w", err)
	}
	fsys := root.FS()
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return nil, fmt.Errorf("web dir %q has no index.html: %w", dir, err)
	}
	return &staticHandler{fsys: fsys, files: http.FileServerFS(fsys)}, nil
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean("/" + r.URL.Path)
	name := strings.TrimPrefix(clean, "/")
	if name == "" || name == "index.html" {
		h.serveIndex(w, r)
		return
	}
	fi, err := fs.Stat(h.fsys, name)
	switch {
	case err == nil && !fi.IsDir():
		if strings.HasPrefix(name, "assets/") {
			// Vite emits content-hashed file names under assets/.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		h.files.ServeHTTP(w, r)
	case isAssetPath(clean) || (err != nil && !errors.Is(err, fs.ErrNotExist)):
		http.NotFound(w, r)
	default:
		h.serveIndex(w, r)
	}
}

func (h *staticHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(h.fsys, "index.html")
	if err != nil {
		http.Error(w, "index unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	// b is the embedded index.html built into the binary; no request data reaches it.
	_, _ = w.Write(b) //nolint:gosec // G705 false positive: static embedded content, served with CSP (NFR-SEC-007).
}
