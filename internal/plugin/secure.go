package plugin

import (
	"encoding/json"
	"log/slog"
	"sort"
)

// Redacted replaces secret values wherever they would otherwise be printed.
const Redacted = "[REDACTED]"

// SecureValues holds secret settings of a source instance (passwords, tokens, keys).
//
// It is write-only toward clients and logs: MarshalJSON emits only {"key": true} for
// each non-empty value, String/GoString return a redacted form, and LogValue makes slog
// print [REDACTED] (fmt verbs %v, %+v, %#v, %s and %q are covered). Callers read
// secrets with Get. Writing secrets through the API will use a dedicated request type
// with envelope encryption at rest (B-025).
type SecureValues map[string]string

// Get returns the secret for key.
func (s SecureValues) Get(key string) (string, bool) {
	v, ok := s[key]
	return v, ok
}

// Keys returns the sorted names of the keys that have a non-empty value.
func (s SecureValues) Keys() []string {
	keys := make([]string, 0, len(s))
	for k, v := range s {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// MarshalJSON reports which secrets are set, never their values.
func (s SecureValues) MarshalJSON() ([]byte, error) {
	set := make(map[string]bool, len(s))
	for _, k := range s.Keys() {
		set[k] = true
	}
	return json.Marshal(set)
}

// String implements fmt.Stringer without revealing values.
func (s SecureValues) String() string { return Redacted }

// GoString implements fmt.GoStringer so %#v does not reveal values.
func (s SecureValues) GoString() string { return "plugin.SecureValues(" + Redacted + ")" }

// LogValue implements slog.LogValuer.
func (s SecureValues) LogValue() slog.Value { return slog.StringValue(Redacted) }
