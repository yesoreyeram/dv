package jq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Limits bound one evaluation (FR-073 AC5, NFR-SEC-021 AC2). Zero members use the
// defaults.
type Limits struct {
	MaxSteps   int
	MaxOutputs int
	MaxDepth   int
}

// Default budgets.
const (
	DefaultMaxSteps   = 1_000_000
	DefaultMaxOutputs = 100_000
	DefaultMaxDepth   = 64
	cancelCheckEvery  = 1024
)

func (l Limits) withDefaults() Limits {
	if l.MaxSteps <= 0 {
		l.MaxSteps = DefaultMaxSteps
	}
	if l.MaxOutputs <= 0 {
		l.MaxOutputs = DefaultMaxOutputs
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	return l
}

// ErrBudget is returned when an evaluation exceeds one of its Limits. It is never caught
// by try or `?`.
var ErrBudget = errors.New("selector exceeded its evaluation budget")

// RuntimeError is a jq evaluation error such as indexing a number. try and `?` catch it.
type RuntimeError struct {
	Msg   string
	Value any // the value of error(v); nil for built-in errors
}

func (e *RuntimeError) Error() string { return e.Msg }

func rtErr(format string, a ...any) error { return &RuntimeError{Msg: fmt.Sprintf(format, a...)} }

// Program is a compiled expression. It is immutable and safe for concurrent use.
type Program struct {
	root node
	src  string
}

// Compile parses src. Unsupported jq constructs are parse errors (FR-073 AC3).
func Compile(src string) (*Program, error) {
	n, err := parse(src)
	if err != nil {
		return nil, err
	}
	return &Program{root: n, src: src}, nil
}

// String returns the source of the program.
func (p *Program) String() string { return p.src }

// Run evaluates the program on input and calls emit for every output, in order.
func (p *Program) Run(ctx context.Context, input any, lim Limits, emit func(any) error) error {
	m := &machine{ctx: ctx, lim: lim.withDefaults()}
	err := m.eval(p.root, input, nil, func(v any) error {
		m.outputs++
		if m.outputs > m.lim.MaxOutputs {
			return fmt.Errorf("%w (more than %d outputs)", ErrBudget, m.lim.MaxOutputs)
		}
		if err := m.bound(v); err != nil {
			return err
		}
		return emit(v)
	})
	var pe *passErr
	if errors.As(err, &pe) {
		return pe.err
	}
	return err
}

// All evaluates the program and returns every output.
func (p *Program) All(ctx context.Context, input any, lim Limits) ([]any, error) {
	var out []any
	err := p.Run(ctx, input, lim, func(v any) error {
		out = append(out, v)
		return nil
	})
	return out, err
}

var errFirstDone = errors.New("first output found")

// First returns the first output of the program, or ok=false when it outputs nothing.
func (p *Program) First(ctx context.Context, input any, lim Limits) (v any, ok bool, err error) {
	err = p.Run(ctx, input, lim, func(x any) error {
		v, ok = x, true
		return errFirstDone
	})
	if errors.Is(err, errFirstDone) {
		err = nil
	}
	return v, ok, err
}

type env struct {
	name string
	val  any
	next *env
}

func (e *env) lookup(name string) (any, bool) {
	for ; e != nil; e = e.next {
		if e.name == name {
			return e.val, true
		}
	}
	return nil, false
}

// passErr carries an error raised downstream of a try/alternative/limit through that
// construct untouched; tok identifies the construct that wrapped it.
type passErr struct {
	tok *int
	err error
}

func (e *passErr) Error() string { return e.err.Error() }
func (e *passErr) Unwrap() error { return e.err }

type machine struct {
	ctx      context.Context
	lim      Limits
	steps    int
	outputs  int
	slices   map[sliceKey]measured
	objects  map[*Object]measured
	retained int // container elements the measurement memo keeps alive
}

type emitFn func(any) error

func (m *machine) step(n int) error {
	before := m.steps
	m.steps += n
	if m.steps > m.lim.MaxSteps {
		return fmt.Errorf("%w (more than %d steps)", ErrBudget, m.lim.MaxSteps)
	}
	if before/cancelCheckEvery != m.steps/cancelCheckEvery {
		if err := m.ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// guard runs body and separates errors raised inside it (returned as is) from errors
// raised by out, the continuation (returned wrapped in errDownstream).
func (m *machine) guard(body func(out emitFn) error, out emitFn) (inner error, downstream error) {
	tok := new(int)
	err := body(func(v any) error {
		if err := out(v); err != nil {
			return &passErr{tok, err}
		}
		return nil
	})
	if err == nil {
		return nil, nil
	}
	var pe *passErr
	if errors.As(err, &pe) && pe.tok == tok {
		return nil, pe.err
	}
	return err, nil
}

// catchable reports whether err may be swallowed by try, `?` or `//`.
func catchable(err error) bool {
	var re *RuntimeError
	var pe *passErr
	if errors.As(err, &pe) {
		return false
	}
	return errors.As(err, &re)
}

func (m *machine) eval(n node, in any, e *env, out emitFn) error {
	if err := m.step(1); err != nil {
		return err
	}
	switch n := n.(type) {
	case identity:
		return out(in)
	case literal:
		return out(n.v)
	case varRef:
		v, ok := e.lookup(n.name)
		if !ok {
			return rtErr("$%s is not defined", n.name)
		}
		return out(v)
	case recurse:
		return m.recurse(in, 0, out)
	case index:
		return m.eval(n.target, in, e, func(t any) error {
			return m.eval(n.key, in, e, func(k any) error {
				v, err := indexValue(t, k)
				if err != nil {
					return err
				}
				return out(v)
			})
		})
	case slice:
		return m.eval(n.target, in, e, func(t any) error {
			return m.optEval(n.to, in, e, func(to any) error {
				return m.optEval(n.from, in, e, func(from any) error {
					v, err := sliceValue(t, from, to)
					if err != nil {
						return err
					}
					if err := m.step(linearCost(v)); err != nil {
						return err
					}
					return out(v)
				})
			})
		})
	case iterate:
		return m.eval(n.target, in, e, func(t any) error {
			switch c := t.(type) {
			case []any:
				for _, v := range c {
					if err := out(v); err != nil {
						return err
					}
				}
				return nil
			case *Object:
				for _, k := range c.keys {
					if err := out(c.m[k]); err != nil {
						return err
					}
				}
				return nil
			}
			return rtErr("Cannot iterate over %s", describeShort(t))
		})
	case tryNode:
		inner, down := m.guard(func(o emitFn) error { return m.eval(n.body, in, e, o) }, out)
		if down != nil {
			return down
		}
		if inner == nil {
			return nil
		}
		if !catchable(inner) {
			return inner
		}
		if n.catch == nil {
			return nil
		}
		var msg any = inner.Error()
		var re *RuntimeError
		if errors.As(inner, &re) && re.Value != nil {
			msg = re.Value
		}
		return m.eval(n.catch, msg, e, out)
	case pipe:
		return m.eval(n.left, in, e, func(v any) error { return m.eval(n.right, v, e, out) })
	case comma:
		if err := m.eval(n.left, in, e, out); err != nil {
			return err
		}
		return m.eval(n.right, in, e, out)
	case alt:
		any1 := false
		inner, down := m.guard(func(o emitFn) error {
			return m.eval(n.left, in, e, func(v any) error {
				if truthy(v) {
					any1 = true
					return o(v)
				}
				return nil
			})
		}, out)
		if down != nil {
			return down
		}
		if inner != nil && !catchable(inner) {
			return inner
		}
		if any1 {
			return nil
		}
		return m.eval(n.right, in, e, out)
	case andNode:
		return m.eval(n.left, in, e, func(l any) error {
			if !truthy(l) {
				return out(false)
			}
			return m.eval(n.right, in, e, func(r any) error { return out(truthy(r)) })
		})
	case orNode:
		return m.eval(n.left, in, e, func(l any) error {
			if truthy(l) {
				return out(true)
			}
			return m.eval(n.right, in, e, func(r any) error { return out(truthy(r)) })
		})
	case binary:
		return m.eval(n.right, in, e, func(r any) error {
			return m.eval(n.left, in, e, func(l any) error {
				v, err := m.binop(n.op, l, r)
				if err != nil {
					return err
				}
				if n.op == "+" || n.op == "*" || n.op == "-" {
					if err := m.bound(v); err != nil {
						return err
					}
				}
				return out(v)
			})
		})
	case neg:
		return m.eval(n.x, in, e, func(v any) error {
			if !isNumber(v) {
				return rtErr("%s cannot be negated", describe(v))
			}
			if num, ok := v.(json.Number); ok && !strings.HasPrefix(string(num), "-") {
				return out(json.Number("-" + string(num)))
			}
			return out(-toFloat(v))
		})
	case arrayCon:
		arr := []any{}
		if n.body != nil {
			if err := m.eval(n.body, in, e, func(v any) error {
				arr = append(arr, v)
				return nil
			}); err != nil {
				return err
			}
		}
		if err := m.bound(arr); err != nil {
			return err
		}
		return out(arr)
	case objectCon:
		return m.buildObject(n.entries, 0, NewObject(len(n.entries)), in, e, out)
	case ifNode:
		return m.eval(n.cond, in, e, func(c any) error {
			if truthy(c) {
				return m.eval(n.then, in, e, out)
			}
			if n.els == nil {
				return out(in)
			}
			return m.eval(n.els, in, e, out)
		})
	case asNode:
		return m.eval(n.source, in, e, func(v any) error {
			return m.eval(n.body, in, &env{n.name, v, e}, out)
		})
	case reduceNode:
		return m.eval(n.init, in, e, func(acc any) error {
			if err := m.eval(n.source, in, e, func(x any) error {
				next, found := any(nil), false
				if err := m.eval(n.update, acc, &env{n.name, x, e}, func(v any) error {
					next, found = v, true
					return nil
				}); err != nil {
					return err
				}
				if found {
					acc = next
				} else {
					acc = nil
				}
				return nil
			}); err != nil {
				return err
			}
			return out(acc)
		})
	case funcall:
		return m.call(n, in, e, out)
	}
	return fmt.Errorf("jq: unknown node %T", n)
}

func (m *machine) optEval(n node, in any, e *env, out emitFn) error {
	if n == nil {
		return out(nil)
	}
	return m.eval(n, in, e, out)
}

func (m *machine) recurse(v any, depth int, out emitFn) error {
	if depth > m.lim.MaxDepth {
		return fmt.Errorf("%w (value nests more than %d levels)", ErrBudget, m.lim.MaxDepth)
	}
	if err := m.step(1); err != nil {
		return err
	}
	if err := out(v); err != nil {
		return err
	}
	switch c := v.(type) {
	case []any:
		for _, x := range c {
			if err := m.recurse(x, depth+1, out); err != nil {
				return err
			}
		}
	case *Object:
		for _, k := range c.keys {
			if err := m.recurse(c.m[k], depth+1, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *machine) buildObject(entries []objEntry, i int, acc *Object, in any, e *env, out emitFn) error {
	if i == len(entries) {
		if err := m.bound(acc); err != nil {
			return err
		}
		return out(acc)
	}
	ent := entries[i]
	return m.eval(ent.key, in, e, func(k any) error {
		ks, ok := k.(string)
		if !ok {
			return rtErr("Object keys must be strings")
		}
		return m.eval(ent.value, in, e, func(v any) error {
			next := acc.clone()
			next.Set(ks, v)
			return m.buildObject(entries, i+1, next, in, e, out)
		})
	})
}

// describeShort is jq's type-with-value for iteration and index errors.
func describeShort(v any) string {
	if v == nil {
		return "null"
	}
	return describe(v)
}

func indexValue(t, k any) (any, error) {
	switch c := t.(type) {
	case nil:
		switch k.(type) {
		case string, nil, json.Number, float64, *Object:
			return nil, nil
		}
	case *Object:
		if ks, ok := k.(string); ok {
			v, _ := c.Get(ks)
			return v, nil
		}
		return nil, rtErr("Cannot index object with %s", typeName(k))
	case []any:
		if isNumber(k) {
			f := toFloat(k)
			if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1e15 {
				return nil, nil
			}
			i := int(math.Floor(f))
			if i < 0 {
				i += len(c)
			}
			if i < 0 || i >= len(c) {
				return nil, nil
			}
			return c[i], nil
		}
		if o, ok := k.(*Object); ok {
			// .[{"start": a, "end": b}] is how jq expresses slices internally.
			return sliceValue(c, o.m["start"], o.m["end"])
		}
		return nil, rtErr("Cannot index array with %s", typeName(k))
	}
	if ks, ok := k.(string); ok {
		return nil, rtErr("Cannot index %s with %q", typeName(t), ks)
	}
	return nil, rtErr("Cannot index %s with %s", typeName(t), typeName(k))
}

func sliceIndex(v any, n int) (int, error) {
	if v == nil {
		return 0, nil
	}
	if !isNumber(v) {
		return 0, rtErr("Start and end indices of an array slice must be numbers")
	}
	f := math.Floor(toFloat(v))
	if math.IsNaN(f) {
		f = 0 // a NaN bound must never reach the integer conversion below
	}
	if f < 0 {
		f += float64(n)
	}
	if f < 0 {
		f = 0
	}
	if f > float64(n) {
		f = float64(n)
	}
	return int(f), nil
}

func sliceValue(t, from, to any) (any, error) {
	var n int
	switch c := t.(type) {
	case nil:
		return nil, nil
	case []any:
		n = len(c)
	case string:
		n = len([]rune(c))
	default:
		return nil, rtErr("Cannot index %s with object", typeName(t))
	}
	start, err := sliceIndex(from, n)
	if err != nil {
		return nil, err
	}
	end := n
	if to != nil {
		if end, err = sliceIndex(to, n); err != nil {
			return nil, err
		}
	}
	if end < start {
		end = start
	}
	switch c := t.(type) {
	case []any:
		return append([]any{}, c[start:end]...), nil
	case string:
		return string([]rune(c)[start:end]), nil
	}
	return nil, nil
}

func (m *machine) binop(op string, l, r any) (any, error) {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		if err := m.chargeSize(l, r); err != nil {
			return nil, err
		}
	}
	switch op {
	case "==":
		return compare(l, r) == 0, nil
	case "!=":
		return compare(l, r) != 0, nil
	case "<":
		return compare(l, r) < 0, nil
	case "<=":
		return compare(l, r) <= 0, nil
	case ">":
		return compare(l, r) > 0, nil
	case ">=":
		return compare(l, r) >= 0, nil
	case "+":
		return m.add(l, r)
	case "-":
		return m.sub(l, r)
	case "*":
		return m.mul(l, r)
	case "/":
		return m.div(l, r)
	case "%":
		if isNumber(l) && isNumber(r) {
			a, b := truncInt(toFloat(l)), truncInt(toFloat(r))
			if b == 0 {
				return nil, rtErr("%s and %s cannot be divided because the divisor is zero", describe(l), describe(r))
			}
			if b < 0 {
				b = -b
			}
			res := a % b
			return float64(res), nil
		}
		return nil, rtErr("%s and %s cannot be divided", describe(l), describe(r))
	}
	return nil, fmt.Errorf("jq: unknown operator %q", op)
}

func truncInt(f float64) int64 {
	if math.IsNaN(f) {
		return 0
	}
	if f > math.MaxInt64 {
		return math.MaxInt64
	}
	if f < math.MinInt64 {
		return math.MinInt64
	}
	return int64(f)
}

func (m *machine) add(l, r any) (any, error) {
	if l == nil {
		return r, nil
	}
	if r == nil {
		return l, nil
	}
	switch a := l.(type) {
	case json.Number, float64:
		if isNumber(r) {
			return toFloat(a) + toFloat(r), nil
		}
	case string:
		if b, ok := r.(string); ok {
			if err := m.step(len(a)/16 + len(b)/16); err != nil {
				return nil, err
			}
			return a + b, nil
		}
	case []any:
		if b, ok := r.([]any); ok {
			if err := m.step(len(a) + len(b)); err != nil {
				return nil, err
			}
			out := make([]any, 0, len(a)+len(b))
			return append(append(out, a...), b...), nil
		}
	case *Object:
		if b, ok := r.(*Object); ok {
			if err := m.step(a.Len() + b.Len()); err != nil {
				return nil, err
			}
			out := a.clone()
			for _, k := range b.keys {
				out.Set(k, b.m[k])
			}
			return out, nil
		}
	}
	return nil, rtErr("%s and %s cannot be added", describe(l), describe(r))
}

func (m *machine) sub(l, r any) (any, error) {
	if isNumber(l) && isNumber(r) {
		return toFloat(l) - toFloat(r), nil
	}
	if a, ok := l.([]any); ok {
		if b, ok := r.([]any); ok {
			if err := m.step(len(a) * (len(b) + 1)); err != nil {
				return nil, err
			}
			out := []any{}
		outer:
			for _, x := range a {
				for _, y := range b {
					if compare(x, y) == 0 {
						continue outer
					}
				}
				out = append(out, x)
			}
			return out, nil
		}
	}
	return nil, rtErr("%s and %s cannot be subtracted", describe(l), describe(r))
}

func (m *machine) mul(l, r any) (any, error) {
	if isNumber(l) && isNumber(r) {
		return toFloat(l) * toFloat(r), nil
	}
	if s, ok := l.(string); ok && isNumber(r) {
		return m.repeat(s, toFloat(r))
	}
	if s, ok := r.(string); ok && isNumber(l) {
		return m.repeat(s, toFloat(l))
	}
	if a, ok := l.(*Object); ok {
		if b, ok := r.(*Object); ok {
			return m.deepMerge(a, b)
		}
	}
	return nil, rtErr("%s and %s cannot be multiplied", describe(l), describe(r))
}

func (m *machine) repeat(s string, n float64) (any, error) {
	if n <= 0 || math.IsNaN(n) {
		return nil, nil
	}
	count := int(math.Ceil(n))
	if n > 1e7 {
		return nil, fmt.Errorf("%w (string repeat too large)", ErrBudget)
	}
	if err := m.step(count * (len(s)/16 + 1)); err != nil {
		return nil, err
	}
	return strings.Repeat(s, count), nil
}

func (m *machine) deepMerge(a, b *Object) (*Object, error) {
	if err := m.step(a.Len() + b.Len()); err != nil {
		return nil, err
	}
	out := a.clone()
	for _, k := range b.keys {
		bv := b.m[k]
		if ao, ok := out.m[k].(*Object); ok {
			if bo, ok := bv.(*Object); ok {
				merged, err := m.deepMerge(ao, bo)
				if err != nil {
					return nil, err
				}
				out.Set(k, merged)
				continue
			}
		}
		out.Set(k, bv)
	}
	return out, nil
}

func (m *machine) div(l, r any) (any, error) {
	if isNumber(l) && isNumber(r) {
		if toFloat(r) == 0 {
			return nil, rtErr("%s and %s cannot be divided because the divisor is zero", describe(l), describe(r))
		}
		return toFloat(l) / toFloat(r), nil
	}
	if a, ok := l.(string); ok {
		if b, ok := r.(string); ok {
			return m.split(a, b)
		}
	}
	return nil, rtErr("%s and %s cannot be divided", describe(l), describe(r))
}

func (m *machine) split(s, sep string) (any, error) {
	if err := m.step(len(s)/16 + 1); err != nil {
		return nil, err
	}
	if s == "" {
		return []any{}, nil
	}
	var parts []string
	if sep == "" {
		for _, r := range s {
			parts = append(parts, string(r))
		}
	} else {
		parts = strings.Split(s, sep)
	}
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out, nil
}
