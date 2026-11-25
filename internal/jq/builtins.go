package jq

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxRegexBytes bounds patterns given to test (NFR-SEC-021 AC3).
const MaxRegexBytes = 1024

type builtin func(m *machine, n funcall, in any, e *env, out emitFn) error

// unsupportedFuncs are jq built-ins outside the supported subset (FR-073 AC3).
var unsupportedFuncs = map[string]bool{
	"env": true, "input": true, "inputs": true, "debug": true, "stderr": true, "input_filename": true,
	"input_line_number": true, "getpath": true, "setpath": true, "paths": true, "leaf_paths": true,
	"delpaths": true, "path": true, "del": true, "now": true, "halt": true, "halt_error": true,
	"localtime": true, "mktime": true, "gmtime": true, "strftime": true, "strptime": true,
	"splits": true, "sub": true, "gsub": true, "match": true, "capture": true, "scan": true,
	"tostream": true, "fromstream": true, "truncate_stream": true, "implode": true, "explode": true,
	"builtins": true, "recurse": true, "walk": true, "pick": true, "until": true, "while": true,
	"repeat": true, "combinations": true, "indices": true, "index": true, "rindex": true,
	"inside": true, "infinite": true, "nan": true, "isinfinite": true, "isnan": true,
	"isnormal": true, "isvalid": true, "get_search_list": true,
}

var builtins = map[string]map[int]builtin{}

func def(name string, arity int, f builtin) {
	if builtins[name] == nil {
		builtins[name] = map[int]builtin{}
	}
	builtins[name][arity] = f
}

func checkBuiltin(name string, arity int) error {
	if byArity, ok := builtins[name]; ok {
		if _, ok := byArity[arity]; ok {
			return nil
		}
		arities := make([]int, 0, len(byArity))
		for a := range byArity {
			arities = append(arities, a)
		}
		sort.Ints(arities)
		parts := make([]string, len(arities))
		for i, a := range arities {
			parts[i] = fmt.Sprintf("%s/%d", name, a)
		}
		return fmt.Errorf("%s/%d is not defined (supported: %s)", name, arity, strings.Join(parts, ", "))
	}
	if unsupportedFuncs[name] {
		return fmt.Errorf("%s is not supported", name)
	}
	return fmt.Errorf("%s/%d is not defined", name, arity)
}

func (m *machine) call(n funcall, in any, e *env, out emitFn) error {
	f := builtins[n.name][len(n.args)]
	if f == nil {
		return rtErr("%s/%d is not defined", n.name, len(n.args))
	}
	return f(m, n, in, e, out)
}

// simple defines a 0-arity function of its input.
func simple(name string, f func(m *machine, in any) (any, error)) {
	def(name, 0, func(m *machine, _ funcall, in any, _ *env, out emitFn) error {
		v, err := f(m, in)
		if err != nil {
			return err
		}
		return out(v)
	})
}

// withArg defines a 1-arity function whose argument is evaluated (as a generator) on the
// input; f runs once per argument value.
func withArg(name string, f func(m *machine, in, arg any) (any, error)) {
	def(name, 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		return m.eval(n.args[0], in, e, func(a any) error {
			v, err := f(m, in, a)
			if err != nil {
				return err
			}
			return out(v)
		})
	})
}

// collect evaluates f on in and returns its outputs.
func (m *machine) collect(f node, in any, e *env) ([]any, error) {
	var vs []any
	err := m.eval(f, in, e, func(v any) error {
		vs = append(vs, v)
		return nil
	})
	return vs, err
}

func asArray(name string, in any) ([]any, error) {
	a, ok := in.([]any)
	if !ok {
		return nil, rtErr("%s cannot be %s, as it is not an array", describe(in), name)
	}
	return a, nil
}

func init() {
	def("empty", 0, func(*machine, funcall, any, *env, emitFn) error { return nil })
	def("error", 0, func(_ *machine, _ funcall, in any, _ *env, _ emitFn) error {
		msg := preview(in) + " (not a string)"
		if s, ok := in.(string); ok {
			msg = s
		}
		return &RuntimeError{Msg: msg, Value: in}
	})
	def("error", 1, func(m *machine, n funcall, in any, e *env, _ emitFn) error {
		return m.eval(n.args[0], in, e, func(v any) error {
			msg := preview(v) + " (not a string)"
			if s, ok := v.(string); ok {
				msg = s
			}
			return &RuntimeError{Msg: msg, Value: v}
		})
	})
	simple("not", func(_ *machine, in any) (any, error) { return !truthy(in), nil })
	simple("type", func(_ *machine, in any) (any, error) { return typeName(in), nil })
	simple("length", func(_ *machine, in any) (any, error) {
		switch t := in.(type) {
		case nil:
			return float64(0), nil
		case bool:
			return nil, rtErr("boolean (%v) has no length", t)
		case json.Number, float64:
			return math.Abs(toFloat(t)), nil
		case string:
			return float64(utf8.RuneCountInString(t)), nil
		case []any:
			return float64(len(t)), nil
		case *Object:
			return float64(t.Len()), nil
		}
		return nil, rtErr("%s has no length", describe(in))
	})
	keys := func(sorted bool) func(m *machine, in any) (any, error) {
		return func(m *machine, in any) (any, error) {
			switch t := in.(type) {
			case *Object:
				if err := m.step(t.Len()); err != nil {
					return nil, err
				}
				ks := t.keys
				if sorted {
					ks = t.sortedKeys()
				}
				return stringsToAny(ks), nil
			case []any:
				out := make([]any, len(t))
				for i := range t {
					out[i] = float64(i)
				}
				return out, nil
			}
			return nil, rtErr("%s has no keys", describe(in))
		}
	}
	simple("keys", keys(true))
	simple("keys_unsorted", keys(false))
	withArg("has", func(_ *machine, in, k any) (any, error) {
		switch t := in.(type) {
		case *Object:
			if ks, ok := k.(string); ok {
				_, has := t.m[ks]
				return has, nil
			}
		case []any:
			if isNumber(k) {
				f := toFloat(k)
				return f >= 0 && f < float64(len(t)), nil
			}
		}
		return nil, rtErr("Cannot check whether %s has a %s key", typeName(in), typeName(k))
	})
	withArg("contains", func(m *machine, in, b any) (any, error) {
		if typeName(in) != typeName(b) {
			return nil, rtErr("%s and %s cannot have their containment checked", describe(in), describe(b))
		}
		return contains(in, b), nil
	})
	def("select", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		return m.eval(n.args[0], in, e, func(c any) error {
			if truthy(c) {
				return out(in)
			}
			return nil
		})
	})
	def("map", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		var vals []any
		switch t := in.(type) {
		case []any:
			vals = t
		case *Object:
			for _, k := range t.keys {
				vals = append(vals, t.m[k])
			}
		default:
			return rtErr("Cannot iterate over %s", describeShort(in))
		}
		res := []any{}
		for _, v := range vals {
			if err := m.eval(n.args[0], v, e, func(x any) error {
				res = append(res, x)
				return nil
			}); err != nil {
				return err
			}
		}
		return out(res)
	})
	def("map_values", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		first := func(v any) (any, bool, error) {
			var got any
			found := false
			inner, _ := m.guard(func(o emitFn) error {
				return m.eval(n.args[0], v, e, o)
			}, func(x any) error {
				got, found = x, true
				return errFirstDone
			})
			if inner != nil {
				return nil, false, inner
			}
			return got, found, nil
		}
		switch t := in.(type) {
		case []any:
			res := []any{}
			for _, v := range t {
				x, ok, err := first(v)
				if err != nil {
					return err
				}
				if ok {
					res = append(res, x)
				}
			}
			return out(res)
		case *Object:
			res := NewObject(t.Len())
			for _, k := range t.keys {
				x, ok, err := first(t.m[k])
				if err != nil {
					return err
				}
				if ok {
					res.Set(k, x)
				}
			}
			return out(res)
		}
		return rtErr("Cannot iterate over %s", describeShort(in))
	})
	simple("add", func(m *machine, in any) (any, error) {
		var vals []any
		switch t := in.(type) {
		case nil:
			return nil, nil
		case []any:
			vals = t
		case *Object:
			for _, k := range t.keys {
				vals = append(vals, t.m[k])
			}
		default:
			return nil, rtErr("Cannot iterate over %s", describeShort(in))
		}
		var acc any
		for _, v := range vals {
			r, err := m.add(acc, v)
			if err != nil {
				return nil, err
			}
			acc = r
		}
		return acc, nil
	})
	anyAll := func(isAny bool) {
		name := "all"
		if isAny {
			name = "any"
		}
		simple(name, func(_ *machine, in any) (any, error) {
			a, err := asArray("iterated", in)
			if err != nil {
				return nil, err
			}
			for _, v := range a {
				if truthy(v) == isAny {
					return isAny, nil
				}
			}
			return !isAny, nil
		})
		def(name, 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
			a, err := asArray("iterated", in)
			if err != nil {
				return err
			}
			for _, v := range a {
				hit := false
				if err := m.eval(n.args[0], v, e, func(c any) error {
					if truthy(c) == isAny {
						hit = true
					}
					return nil
				}); err != nil {
					return err
				}
				if hit {
					return out(isAny)
				}
			}
			return out(!isAny)
		})
	}
	anyAll(true)
	anyAll(false)
	rangeFn := func(m *machine, from, to float64, out emitFn) error {
		for x := from; x < to; x++ {
			if err := m.step(1); err != nil {
				return err
			}
			if err := out(x); err != nil {
				return err
			}
		}
		return nil
	}
	def("range", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		return m.eval(n.args[0], in, e, func(to any) error {
			if !isNumber(to) {
				return rtErr("Range bounds must be numeric")
			}
			return rangeFn(m, 0, toFloat(to), out)
		})
	})
	def("range", 2, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		return m.eval(n.args[0], in, e, func(from any) error {
			return m.eval(n.args[1], in, e, func(to any) error {
				if !isNumber(from) || !isNumber(to) {
					return rtErr("Range bounds must be numeric")
				}
				return rangeFn(m, toFloat(from), toFloat(to), out)
			})
		})
	})
	def("first", 0, func(_ *machine, _ funcall, in any, _ *env, out emitFn) error {
		v, err := indexValue(in, float64(0))
		if err != nil {
			return err
		}
		return out(v)
	})
	def("last", 0, func(_ *machine, _ funcall, in any, _ *env, out emitFn) error {
		v, err := indexValue(in, float64(-1))
		if err != nil {
			return err
		}
		return out(v)
	})
	def("first", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		return m.limit(1, n.args[0], in, e, out)
	})
	def("last", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		var last any
		found := false
		if err := m.eval(n.args[0], in, e, func(v any) error {
			last, found = v, true
			return nil
		}); err != nil {
			return err
		}
		if !found {
			return nil
		}
		return out(last)
	})
	def("limit", 2, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		return m.eval(n.args[0], in, e, func(nv any) error {
			if !isNumber(nv) {
				return rtErr("Invalid limit: must be a number")
			}
			k := int(toFloat(nv))
			if k <= 0 {
				return nil
			}
			return m.limit(k, n.args[1], in, e, out)
		})
	})
	def("isempty", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		found := false
		if err := m.limit(1, n.args[0], in, e, func(any) error {
			found = true
			return nil
		}); err != nil {
			return err
		}
		return out(!found)
	})
	simple("sort", func(m *machine, in any) (any, error) {
		a, err := asArray("sorted", in)
		if err != nil {
			return nil, err
		}
		if err := m.step(len(a)); err != nil {
			return nil, err
		}
		out := append([]any{}, a...)
		sort.SliceStable(out, func(i, j int) bool { return compare(out[i], out[j]) < 0 })
		return out, nil
	})
	byKey := func(name string, f func(m *machine, items []keyed) (any, error)) {
		def(name, 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
			a, err := asArray(name+"-ed", in)
			if err != nil {
				return err
			}
			if err := m.step(len(a)); err != nil {
				return err
			}
			items := make([]keyed, len(a))
			for i, v := range a {
				k, err := m.collect(n.args[0], v, e)
				if err != nil {
					return err
				}
				if k == nil {
					k = []any{}
				}
				items[i] = keyed{key: k, val: v}
			}
			sort.SliceStable(items, func(i, j int) bool { return compare(items[i].key, items[j].key) < 0 })
			r, err := f(m, items)
			if err != nil {
				return err
			}
			return out(r)
		})
	}
	byKey("sort_by", func(_ *machine, items []keyed) (any, error) {
		out := make([]any, len(items))
		for i, it := range items {
			out[i] = it.val
		}
		return out, nil
	})
	byKey("group_by", func(_ *machine, items []keyed) (any, error) {
		out := []any{}
		for i, it := range items {
			if i == 0 || compare(items[i-1].key, it.key) != 0 {
				out = append(out, []any{})
			}
			out[len(out)-1] = append(out[len(out)-1].([]any), it.val)
		}
		return out, nil
	})
	byKey("unique_by", func(_ *machine, items []keyed) (any, error) {
		out := []any{}
		for i, it := range items {
			if i == 0 || compare(items[i-1].key, it.key) != 0 {
				out = append(out, it.val)
			}
		}
		return out, nil
	})
	byKey("min_by", func(_ *machine, items []keyed) (any, error) {
		if len(items) == 0 {
			return nil, nil
		}
		return items[0].val, nil
	})
	byKey("max_by", func(_ *machine, items []keyed) (any, error) {
		if len(items) == 0 {
			return nil, nil
		}
		// The last of the equal maxima, like jq.
		return items[len(items)-1].val, nil
	})
	simple("unique", func(m *machine, in any) (any, error) {
		a, err := asArray("sorted", in)
		if err != nil {
			return nil, err
		}
		if err := m.step(len(a)); err != nil {
			return nil, err
		}
		s := append([]any{}, a...)
		sort.SliceStable(s, func(i, j int) bool { return compare(s[i], s[j]) < 0 })
		out := []any{}
		for i, v := range s {
			if i == 0 || compare(s[i-1], v) != 0 {
				out = append(out, v)
			}
		}
		return out, nil
	})
	minMax := func(name string, wantMax bool) {
		simple(name, func(_ *machine, in any) (any, error) {
			a, err := asArray(name+"-ed", in)
			if err != nil {
				return nil, err
			}
			if len(a) == 0 {
				return nil, nil
			}
			best := a[0]
			for _, v := range a[1:] {
				c := compare(v, best)
				if (wantMax && c >= 0) || (!wantMax && c < 0) {
					best = v
				}
			}
			return best, nil
		})
	}
	minMax("min", false)
	minMax("max", true)
	simple("reverse", func(_ *machine, in any) (any, error) {
		switch t := in.(type) {
		case nil:
			return []any{}, nil
		case string:
			r := []rune(t)
			for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
				r[i], r[j] = r[j], r[i]
			}
			return string(r), nil
		case []any:
			out := make([]any, len(t))
			for i, v := range t {
				out[len(t)-1-i] = v
			}
			return out, nil
		}
		return nil, rtErr("Cannot reverse %s", describe(in))
	})
	flatten := func(m *machine, in any, depth float64) (any, error) {
		a, err := asArray("flattened", in)
		if err != nil {
			return nil, err
		}
		if depth < 0 {
			return nil, rtErr("flatten depth must not be negative")
		}
		var out []any
		var walk func(xs []any, d float64) error
		walk = func(xs []any, d float64) error {
			if err := m.step(len(xs)); err != nil {
				return err
			}
			for _, x := range xs {
				if sub, ok := x.([]any); ok && d > 0 {
					if err := walk(sub, d-1); err != nil {
						return err
					}
					continue
				}
				out = append(out, x)
			}
			return nil
		}
		if err := walk(a, depth); err != nil {
			return nil, err
		}
		if out == nil {
			out = []any{}
		}
		return out, nil
	}
	simple("flatten", func(m *machine, in any) (any, error) { return flatten(m, in, 1e9) })
	withArg("flatten", func(m *machine, in, d any) (any, error) {
		if !isNumber(d) {
			return nil, rtErr("flatten depth must be a number")
		}
		return flatten(m, in, toFloat(d))
	})
	simple("to_entries", func(m *machine, in any) (any, error) {
		o, ok := in.(*Object)
		if !ok {
			return nil, rtErr("%s has no keys", describe(in))
		}
		if err := m.step(o.Len()); err != nil {
			return nil, err
		}
		out := make([]any, 0, o.Len())
		for _, k := range o.keys {
			ent := NewObject(2)
			ent.Set("key", k)
			ent.Set("value", o.m[k])
			out = append(out, ent)
		}
		return out, nil
	})
	simple("from_entries", fromEntries)
	def("with_entries", 1, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		o, ok := in.(*Object)
		if !ok {
			return rtErr("%s has no keys", describe(in))
		}
		ents := make([]any, 0, o.Len())
		for _, k := range o.keys {
			ent := NewObject(2)
			ent.Set("key", k)
			ent.Set("value", o.m[k])
			if err := m.eval(n.args[0], ent, e, func(v any) error {
				ents = append(ents, v)
				return nil
			}); err != nil {
				return err
			}
		}
		r, err := fromEntries(m, ents)
		if err != nil {
			return err
		}
		return out(r)
	})
	simple("transpose", func(m *machine, in any) (any, error) {
		a, err := asArray("transposed", in)
		if err != nil {
			return nil, err
		}
		width := 0
		for _, row := range a {
			r, ok := row.([]any)
			if !ok {
				return nil, rtErr("Cannot transpose %s", describe(row))
			}
			width = max(width, len(r))
		}
		if err := m.step(width * (len(a) + 1)); err != nil {
			return nil, err
		}
		out := make([]any, width)
		for i := range width {
			col := make([]any, len(a))
			for j, row := range a {
				r := row.([]any)
				if i < len(r) {
					col[j] = r[i]
				}
			}
			out[i] = col
		}
		return out, nil
	})
	simple("tostring", func(m *machine, in any) (any, error) {
		if s, ok := in.(string); ok {
			return s, nil
		}
		return m.toJSON(in)
	})
	simple("tojson", func(m *machine, in any) (any, error) { return m.toJSON(in) })
	simple("fromjson", func(m *machine, in any) (any, error) {
		s, ok := in.(string)
		if !ok {
			return nil, rtErr("%s cannot be parsed as JSON", describe(in))
		}
		if err := m.step(len(s)/16 + 1); err != nil {
			return nil, err
		}
		v, err := Decode(strings.NewReader(s), m.lim.MaxDepth)
		if err != nil {
			if errors.Is(err, ErrDepth) {
				return nil, fmt.Errorf("%w (value nests more than %d levels)", ErrBudget, m.lim.MaxDepth)
			}
			return nil, rtErr("%s cannot be parsed as JSON", describe(in))
		}
		return v, nil
	})
	simple("tonumber", func(_ *machine, in any) (any, error) {
		switch t := in.(type) {
		case json.Number, float64:
			return t, nil
		case string:
			s := strings.TrimSpace(t)
			if _, err := strconv.ParseFloat(s, 64); err != nil || !json.Valid([]byte(s)) {
				return nil, rtErr("Cannot parse %s as a number", describe(in))
			}
			return json.Number(s), nil
		}
		return nil, rtErr("%s cannot be parsed as a number", describe(in))
	})
	strFn := func(name string, f func(string) string) {
		simple(name, func(_ *machine, in any) (any, error) {
			s, ok := in.(string)
			if !ok {
				return nil, rtErr("%s cannot be %s", describe(in), name)
			}
			return f(s), nil
		})
	}
	strFn("ascii_downcase", asciiLower)
	strFn("ascii_upcase", asciiUpper)
	strArg := func(name string, f func(s, a string) any, otherwise func(in any) any) {
		withArg(name, func(_ *machine, in, a any) (any, error) {
			s, ok1 := in.(string)
			as, ok2 := a.(string)
			if !ok1 || !ok2 {
				if otherwise != nil {
					return otherwise(in), nil
				}
				return nil, rtErr("%s() requires string inputs", name)
			}
			return f(s, as), nil
		})
	}
	strArg("ltrimstr", func(s, a string) any { return strings.TrimPrefix(s, a) }, func(in any) any { return in })
	strArg("rtrimstr", func(s, a string) any { return strings.TrimSuffix(s, a) }, func(in any) any { return in })
	strArg("startswith", func(s, a string) any { return strings.HasPrefix(s, a) }, nil)
	strArg("endswith", func(s, a string) any { return strings.HasSuffix(s, a) }, nil)
	withArg("split", func(m *machine, in, sep any) (any, error) {
		s, ok1 := in.(string)
		ss, ok2 := sep.(string)
		if !ok1 || !ok2 {
			return nil, rtErr("split input and separator must be strings")
		}
		return m.split(s, ss)
	})
	withArg("join", func(m *machine, in, sep any) (any, error) {
		a, err := asArray("joined", in)
		if err != nil {
			return nil, err
		}
		ss, ok := sep.(string)
		if !ok {
			return nil, rtErr("join separator must be a string")
		}
		var b strings.Builder
		for i, v := range a {
			if i > 0 {
				b.WriteString(ss)
			}
			switch t := v.(type) {
			case nil:
			case string:
				b.WriteString(t)
			case bool, json.Number, float64:
				j, _ := Marshal(t, 1)
				b.Write(j)
			default:
				return nil, rtErr("Cannot join with %s", typeName(v))
			}
			if err := m.step(b.Len()/4096 + 1); err != nil {
				return nil, err
			}
		}
		return b.String(), nil
	})
	testFn := func(m *machine, in, re, flags any) (any, error) {
		s, ok1 := in.(string)
		pat, ok2 := re.(string)
		if !ok1 || !ok2 {
			return nil, rtErr("%s cannot be matched, as it is not a string", describe(in))
		}
		if len(pat) > MaxRegexBytes {
			return nil, rtErr("regular expression is longer than %d bytes", MaxRegexBytes)
		}
		prefix := ""
		if flags != nil {
			fs, ok := flags.(string)
			if !ok {
				return nil, rtErr("%s is not a string", describe(flags))
			}
			for _, f := range fs {
				switch f {
				case 'i':
					prefix += "i"
				case 'x':
					return nil, rtErr("regex flag x is not supported")
				case 's':
					prefix += "s"
				case 'g', 'n', 'p', 'l':
					// No effect on test.
				default:
					return nil, rtErr("%s is not a valid modifier string", fs)
				}
			}
		}
		if prefix != "" {
			pat = "(?" + prefix + ")" + pat
		}
		rx, err := m.regexp(pat)
		if err != nil {
			return nil, rtErr("%s (at offset 0) is not a valid regex: %v", preview(re), err)
		}
		if err := m.step(len(s)/64 + 1); err != nil {
			return nil, err
		}
		return rx.MatchString(s), nil
	}
	withArg("test", func(m *machine, in, re any) (any, error) { return testFn(m, in, re, nil) })
	def("test", 2, func(m *machine, n funcall, in any, e *env, out emitFn) error {
		return m.eval(n.args[0], in, e, func(re any) error {
			return m.eval(n.args[1], in, e, func(fl any) error {
				v, err := testFn(m, in, re, fl)
				if err != nil {
					return err
				}
				return out(v)
			})
		})
	})
	simple("todate", func(_ *machine, in any) (any, error) {
		if !isNumber(in) {
			return nil, rtErr("todate requires a number")
		}
		f := toFloat(in)
		if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1e14 {
			return nil, rtErr("%s is out of range for todate", describe(in))
		}
		return time.Unix(int64(math.Floor(f)), 0).UTC().Format("2006-01-02T15:04:05Z"), nil
	})
	builtins["todateiso8601"] = builtins["todate"]
	simple("fromdate", func(_ *machine, in any) (any, error) {
		s, ok := in.(string)
		if !ok {
			return nil, rtErr("fromdate requires a string")
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, rtErr("date %q does not match format \"%%Y-%%m-%%dT%%H:%%M:%%SZ\"", s)
		}
		return float64(t.Unix()), nil
	})
	builtins["fromdateiso8601"] = builtins["fromdate"]
	math1 := func(name string, f func(float64) float64) {
		simple(name, func(_ *machine, in any) (any, error) {
			if !isNumber(in) {
				return nil, rtErr("%s number required", describe(in))
			}
			return f(toFloat(in)), nil
		})
	}
	math1("floor", math.Floor)
	math1("ceil", math.Ceil)
	math1("round", math.Round)
	math1("sqrt", math.Sqrt)
	math1("fabs", math.Abs)
	math1("log", math.Log)
	math1("log10", math.Log10)
	math1("exp", math.Exp)
	simple("abs", func(_ *machine, in any) (any, error) {
		if !isNumber(in) {
			return nil, rtErr("%s has no absolute value", describe(in))
		}
		if n, ok := in.(json.Number); ok && !strings.HasPrefix(string(n), "-") {
			return n, nil
		}
		return math.Abs(toFloat(in)), nil
	})
	typeFilter := func(name string, ok func(any) bool) {
		def(name, 0, func(_ *machine, _ funcall, in any, _ *env, out emitFn) error {
			if ok(in) {
				return out(in)
			}
			return nil
		})
	}
	typeFilter("values", func(v any) bool { return v != nil })
	typeFilter("nulls", func(v any) bool { return v == nil })
	typeFilter("booleans", func(v any) bool { _, ok := v.(bool); return ok })
	typeFilter("numbers", isNumber)
	typeFilter("strings", func(v any) bool { _, ok := v.(string); return ok })
	typeFilter("arrays", func(v any) bool { _, ok := v.([]any); return ok })
	typeFilter("objects", func(v any) bool { _, ok := v.(*Object); return ok })
	typeFilter("iterables", func(v any) bool {
		switch v.(type) {
		case []any, *Object:
			return true
		}
		return false
	})
	typeFilter("scalars", func(v any) bool {
		switch v.(type) {
		case []any, *Object:
			return false
		}
		return true
	})
	simple("utf8bytelength", func(_ *machine, in any) (any, error) {
		s, ok := in.(string)
		if !ok {
			return nil, rtErr("%s only strings have UTF-8 byte length", describe(in))
		}
		return float64(len(s)), nil
	})
}

type keyed struct {
	key []any
	val any
}

// limit emits at most k outputs of f.
func (m *machine) limit(k int, f node, in any, e *env, out emitFn) error {
	count := 0
	tok := new(int)
	err := m.eval(f, in, e, func(v any) error {
		if err := out(v); err != nil {
			return &passErr{tok, err}
		}
		count++
		if count >= k {
			return &passErr{tok, nil}
		}
		return nil
	})
	var pe *passErr
	if errors.As(err, &pe) && pe.tok == tok {
		return pe.err
	}
	return err
}

func (m *machine) toJSON(v any) (any, error) {
	var b bytes.Buffer
	if err := AppendJSON(&b, v, m.lim.MaxDepth); err != nil {
		return nil, fmt.Errorf("%w (value nests more than %d levels)", ErrBudget, m.lim.MaxDepth)
	}
	if err := m.step(b.Len()/16 + 1); err != nil {
		return nil, err
	}
	return b.String(), nil
}

func (m *machine) regexp(pat string) (*regexp.Regexp, error) {
	// Compilation is linear in the pattern (RE2); charge it so many distinct patterns
	// cannot be used to burn CPU outside the budget.
	if err := m.step(len(pat) + 1); err != nil {
		return nil, err
	}
	return regexp.Compile(pat)
}

func fromEntries(m *machine, in any) (any, error) {
	a, ok := in.([]any)
	if !ok {
		return nil, rtErr("Cannot iterate over %s", describeShort(in))
	}
	if err := m.step(len(a)); err != nil {
		return nil, err
	}
	out := NewObject(len(a))
	for _, ent := range a {
		o, ok := ent.(*Object)
		if !ok {
			return nil, rtErr("Cannot index %s with \"key\"", typeName(ent))
		}
		var k any
		for _, name := range []string{"key", "k", "name", "Name", "Key", "K"} {
			if v, ok := o.m[name]; ok && v != nil && v != false {
				k = v
				break
			}
		}
		var key string
		switch t := k.(type) {
		case string:
			key = t
		case json.Number, float64, bool:
			b, _ := Marshal(t, 1)
			key = string(b)
		case nil:
			key = "null"
		default:
			return nil, rtErr("Cannot use %s as object key", describe(k))
		}
		var v any
		for _, name := range []string{"value", "v", "Value", "V"} {
			if x, ok := o.m[name]; ok {
				v = x
				break
			}
		}
		out.Set(key, v)
	}
	return out, nil
}

func contains(a, b any) bool {
	switch x := a.(type) {
	case *Object:
		y := b.(*Object)
		for _, k := range y.keys {
			av, ok := x.m[k]
			if !ok || typeName(av) != typeName(y.m[k]) || !contains(av, y.m[k]) {
				return false
			}
		}
		return true
	case []any:
		y := b.([]any)
		for _, bv := range y {
			found := false
			for _, av := range x {
				if typeName(av) == typeName(bv) && contains(av, bv) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case string:
		return strings.Contains(x, b.(string))
	}
	return compare(a, b) == 0
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
