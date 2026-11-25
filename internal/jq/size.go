package jq

import (
	"fmt"
	"reflect"
)

// MaxValueNodes bounds the expanded size of any value a program builds or outputs
// (NFR-SEC-021 AC2). Values can share subtrees (`[., .]` repeated), so a value's expanded
// size can grow exponentially with the steps spent building it; measuring it with a memo
// keyed by identity keeps every later walk (encoding, depth checks, comparisons) bounded.
const MaxValueNodes = 1 << 20

type sliceKey struct {
	p uintptr
	n int
}

type measured struct {
	keep  any // keeps the value alive so its address is not reused during the run
	size  int
	depth int
}

// measure returns the expanded node count and nesting depth of v. Each distinct
// container is measured once per run and charged one step.
func (m *machine) measure(v any) (size, depth int, err error) {
	switch t := v.(type) {
	case []any:
		if len(t) == 0 {
			return 1, 1, nil
		}
		k := sliceKey{reflect.ValueOf(t).Pointer(), len(t)}
		if r, ok := m.slices[k]; ok {
			return r.size, r.depth, nil
		}
		if err := m.step(1); err != nil {
			return 0, 0, err
		}
		size, depth = 1, 1
		for _, e := range t {
			s, d, err := m.measure(e)
			if err != nil {
				return 0, 0, err
			}
			size = min(size+s, MaxValueNodes+1)
			depth = max(depth, d+1)
		}
		if m.slices == nil {
			m.slices = map[sliceKey]measured{}
		}
		m.slices[k] = measured{t, size, depth}
		return size, depth, nil
	case *Object:
		if r, ok := m.objects[t]; ok {
			return r.size, r.depth, nil
		}
		if err := m.step(1); err != nil {
			return 0, 0, err
		}
		size, depth = 1, 1
		for _, k := range t.keys {
			s, d, err := m.measure(t.m[k])
			if err != nil {
				return 0, 0, err
			}
			size = min(size+s, MaxValueNodes+1)
			depth = max(depth, d+1)
		}
		if m.objects == nil {
			m.objects = map[*Object]measured{}
		}
		m.objects[t] = measured{t, size, depth}
		return size, depth, nil
	}
	return 1, 0, nil
}

// bound fails when v is larger or deeper than the budgets allow.
func (m *machine) bound(v any) error {
	size, depth, err := m.measure(v)
	if err != nil {
		return err
	}
	if size > MaxValueNodes {
		return fmt.Errorf("%w (a value has more than %d nodes)", ErrBudget, MaxValueNodes)
	}
	if depth > m.lim.MaxDepth {
		return fmt.Errorf("%w (value nests more than %d levels)", ErrBudget, m.lim.MaxDepth)
	}
	return nil
}

// chargeSize charges steps proportional to the expanded size of v, for operations that
// walk a whole value (encoding, deep comparison, sorting).
func (m *machine) chargeSize(vs ...any) error {
	total := 0
	for _, v := range vs {
		s, _, err := m.measure(v)
		if err != nil {
			return err
		}
		total += s
	}
	return m.step(total)
}
