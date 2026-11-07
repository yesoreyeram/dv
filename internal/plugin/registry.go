package plugin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// idPattern constrains plugin IDs (FR-015 AC2). IDs appear in URLs, logs and configs.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

// Registry errors.
var (
	ErrFrozen    = errors.New("plugin registry is frozen")
	ErrDuplicate = errors.New("plugin ID already registered")
	ErrInvalidID = errors.New("invalid plugin ID")
	ErrNotFound  = errors.New("plugin not found")
	ErrNotFrozen = errors.New("plugin registration has not completed")
)

// Registry holds the compiled-in plugins. Register everything during start-up, then call
// Freeze; registration afterwards fails (FR-015 AC3). It is safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]Source
	formats map[string]Format
	frozen  bool
}

// NewRegistry returns an empty, unfrozen registry.
func NewRegistry() *Registry {
	return &Registry{sources: map[string]Source{}, formats: map[string]Format{}}
}

// RegisterSource adds a data-source plugin.
func (r *Registry) RegisterSource(s Source) error {
	if s == nil {
		return errors.New("nil source")
	}
	id := s.Meta().ID
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLocked(id, r.sources[id] != nil); err != nil {
		return fmt.Errorf("register source %q: %w", id, err)
	}
	r.sources[id] = s
	return nil
}

// RegisterFormat adds a format plugin.
func (r *Registry) RegisterFormat(f Format) error {
	if f == nil {
		return errors.New("nil format")
	}
	id := f.Meta().ID
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkLocked(id, r.formats[id] != nil); err != nil {
		return fmt.Errorf("register format %q: %w", id, err)
	}
	r.formats[id] = f
	return nil
}

func (r *Registry) checkLocked(id string, exists bool) error {
	switch {
	case r.frozen:
		return ErrFrozen
	case !idPattern.MatchString(id):
		return ErrInvalidID
	case exists:
		return ErrDuplicate
	}
	return nil
}

// Freeze ends registration. It is idempotent.
func (r *Registry) Freeze() {
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

// Frozen reports whether Freeze has been called.
func (r *Registry) Frozen() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.frozen
}

// Source returns the data-source plugin with the given ID.
func (r *Registry) Source(id string) (Source, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.sources[id]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("source %q: %w", id, ErrNotFound)
}

// Format returns the format plugin with the given ID.
func (r *Registry) Format(id string) (Format, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if f, ok := r.formats[id]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("format %q: %w", id, ErrNotFound)
}

// Catalog is the public plugin listing served at GET /api/v1/plugins (FR-015 AC4).
type Catalog struct {
	Sources []SourceMeta `json:"sources"`
	Formats []FormatMeta `json:"formats"`
}

// Catalog returns the metadata of all plugins, sorted by ID. Slices are never nil so
// they encode as JSON arrays.
func (r *Registry) Catalog() Catalog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := Catalog{Sources: make([]SourceMeta, 0, len(r.sources)), Formats: make([]FormatMeta, 0, len(r.formats))}
	for _, s := range r.sources {
		m := s.Meta()
		m.Capabilities = nonNil(m.Capabilities)
		c.Sources = append(c.Sources, m)
	}
	for _, f := range r.formats {
		m := f.Meta()
		m.MediaTypes, m.Extensions = nonNil(m.MediaTypes), nonNil(m.Extensions)
		c.Formats = append(c.Formats, m)
	}
	sort.Slice(c.Sources, func(i, j int) bool { return c.Sources[i].ID < c.Sources[j].ID })
	sort.Slice(c.Formats, func(i, j int) bool { return c.Formats[i].ID < c.Formats[j].ID })
	return c
}

// nonNil returns a non-nil copy of s, so it encodes as a JSON array even when empty.
func nonNil(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// ReadinessCheck fails until the registry is frozen, so traffic is only accepted after
// start-up registration completed (FR-015 AC3). Register it as the "plugins" check.
func (r *Registry) ReadinessCheck(context.Context) error {
	if !r.Frozen() {
		return ErrNotFrozen
	}
	return nil
}
