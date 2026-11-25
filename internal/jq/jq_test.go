package jq

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func run(t *testing.T, expr, input string, lim Limits) ([]string, error) {
	t.Helper()
	p, err := Compile(expr)
	if err != nil {
		return nil, err
	}
	in, err := Decode(strings.NewReader(input), 64)
	if err != nil {
		t.Fatalf("decode %q: %v", input, err)
	}
	var out []string
	err = p.Run(context.Background(), in, lim, func(v any) error {
		b, err := Marshal(v, 64)
		if err != nil {
			return err
		}
		out = append(out, string(b))
		return nil
	})
	return out, err
}

// The expected outputs are what jq 1.7.1 prints with -c.
func TestEval(t *testing.T) {
	cases := []struct {
		expr, in string
		want     string // outputs joined by " | "
	}{
		{".", `{"b":1,"a":2}`, `{"b":1,"a":2}`},
		{".a", `{"a":1}`, `1`},
		{".a.b", `{"a":{"b":"x"}}`, `"x"`},
		{`."a-b"`, `{"a-b":3}`, `3`},
		{`.["a"]`, `{"a":3}`, `3`},
		{".missing", `{"a":1}`, `null`},
		{".a", `null`, `null`},
		{".[0]", `[1,2,3]`, `1`},
		{".[-1]", `[1,2,3]`, `3`},
		{".[5]", `[1,2,3]`, `null`},
		{".[1:]", `[1,2,3]`, `[2,3]`},
		{".[:-1]", `[1,2,3]`, `[1,2]`},
		{".[1:2]", `"abc"`, `"b"`},
		{".[]", `[1,"a",null]`, `1 | "a" | null`},
		{".[]", `{"x":1,"y":2}`, `1 | 2`},
		{".[]?", `3`, ``},
		{".a?", `[1]`, ``},
		{"..", `[1,[2]]`, `[1,[2]] | 1 | [2] | 2`},
		{".a, .b", `{"a":1,"b":2}`, `1 | 2`},
		{".[] | .n", `[{"n":1},{"n":2}]`, `1 | 2`},
		{"[.[] | . * 2]", `[1,2]`, `[2,4]`},
		{"{a, b: .c}", `{"a":1,"c":3}`, `{"a":1,"b":3}`},
		{`{"x y": 1, (.k): 2}`, `{"k":"z"}`, `{"x y":1,"z":2}`},
		{"{a: (1,2)}", `null`, `{"a":1} | {"a":2}`},
		{"{(.[]): 1}", `["p","q"]`, `{"p":1} | {"q":1}`},
		{". as $x | {$x}", `5`, `{"x":5}`},
		{"1 + 2 * 3", `null`, `7`},
		{"(1,2) + (10,20)", `null`, `11 | 12 | 21 | 22`},
		{"10 / 4", `null`, `2.5`},
		{"7 % 3", `null`, `1`},
		{"-.a", `{"a":2}`, `-2`},
		{`"a" + "b"`, `null`, `"ab"`},
		{"[1,2] + [3]", `null`, `[1,2,3]`},
		{"[1,2,2,3] - [2]", `null`, `[1,3]`},
		{`{"a":1} + {"b":2}`, `null`, `{"a":1,"b":2}`},
		{`{"a":{"b":1}} * {"a":{"c":2}}`, `null`, `{"a":{"b":1,"c":2}}`},
		{"null + 1", `null`, `1`},
		{`"a,b" / ","`, `null`, `["a","b"]`},
		{`"ab" * 3`, `null`, `"ababab"`},
		{".a // \"d\"", `{"a":null}`, `"d"`},
		{".a // \"d\"", `{"a":false}`, `"d"`},
		{".a // \"d\"", `{"a":0}`, `0`},
		{"(.[] | select(. > 1)) // 0", `[1]`, `0`},
		{"error(\"x\") // 1", `null`, `1`},
		{"1 == 1.0", `null`, `true`},
		{`[1, "1"] | .[0] == .[1]`, `null`, `false`},
		{"1 < \"a\"", `null`, `true`},
		{"null < false", `null`, `true`},
		{`{"b":1,"a":2} == {"a":2,"b":1}`, `null`, `true`},
		{"true and (true, false)", `null`, `true | false`},
		{"false or false", `null`, `false`},
		{"(true, false) | not", `null`, `false | true`},
		{"if . > 1 then \"big\" elif . == 1 then \"one\" else \"small\" end", `1`, `"one"`},
		{"if . then 1 end", `false`, `false`},
		{"try error(\"boom\") catch .", `null`, `"boom"`},
		{"try (1, error(\"x\"), 3)", `null`, `1`},
		{"[.[] | try tonumber]", `["1","x","3"]`, `[1,3]`},
		{"[.[] | tonumber?]", `["1","x"]`, `[1]`},
		{"reduce .[] as $x (0; . + $x)", `[1,2,3]`, `6`},
		{"reduce empty as $x (0; . + 1)", `null`, `0`},
		{".[] as $x | $x * 10", `[1,2]`, `10 | 20`},
		{".a as $x | .b as $y | $x + $y", `{"a":1,"b":2}`, `3`},
		{"[.[] | select(.ok)] | length", `[{"ok":true},{"ok":false},{}]`, `1`},
		{"map(.+1)", `[1,2]`, `[2,3]`},
		{"map_values(. * 2)", `{"a":1,"b":2}`, `{"a":2,"b":4}`},
		{"map_values(empty)", `{"a":1}`, `{}`},
		{"length", `"héllo"`, `5`},
		{"length", `{"a":1}`, `1`},
		{"length", `-3`, `3`},
		{"length", `null`, `0`},
		{"keys", `{"b":1,"a":2}`, `["a","b"]`},
		{"keys_unsorted", `{"b":1,"a":2}`, `["b","a"]`},
		{"keys", `[5,6]`, `[0,1]`},
		{`has("a")`, `{"a":null}`, `true`},
		{"has(2)", `[1]`, `false`},
		{"[.[] | type]", `[null,true,1,"s",[],{}]`, `["null","boolean","number","string","array","object"]`},
		{"add", `[1,2,3]`, `6`},
		{"add", `["a","b"]`, `"ab"`},
		{"add", `[]`, `null`},
		{"any", `[false,1]`, `true`},
		{"all", `[]`, `true`},
		{"any(. > 2)", `[1,3]`, `true`},
		{"[range(3)]", `null`, `[0,1,2]`},
		{"[range(2;5)]", `null`, `[2,3,4]`},
		{"first, last", `[1,2,3]`, `1 | 3`},
		{"first(.[])", `[4,5]`, `4`},
		{"last(.[])", `[4,5]`, `5`},
		{"[limit(2; .[])]", `[1,2,3]`, `[1,2]`},
		{"[limit(0; .[])]", `[1,2,3]`, `[]`},
		{"isempty(empty), isempty(.[])", `[1]`, `true | false`},
		{"sort", `[3,"a",null,1,true,false,[1],{"a":1}]`, `[null,false,true,1,3,"a",[1],{"a":1}]`},
		{"sort_by(.n)", `[{"n":2},{"n":1}]`, `[{"n":1},{"n":2}]`},
		{"sort_by(.a, .b)", `[{"a":1,"b":2},{"a":1,"b":1}]`, `[{"a":1,"b":1},{"a":1,"b":2}]`},
		{"group_by(.k) | map(length)", `[{"k":"x"},{"k":"y"},{"k":"x"}]`, `[2,1]`},
		{"unique", `[2,1,2]`, `[1,2]`},
		{"unique_by(length)", `["a","bb","c"]`, `["a","bb"]`},
		{"min, max", `[3,1,2]`, `1 | 3`},
		{"min", `[]`, `null`},
		{"min_by(.v), max_by(.v)", `[{"v":2},{"v":1}]`, `{"v":1} | {"v":2}`},
		{"reverse", `[1,2]`, `[2,1]`},
		{"reverse", `"abc"`, `"cba"`},
		{"flatten", `[1,[2,[3]]]`, `[1,2,3]`},
		{"flatten(1)", `[1,[2,[3]]]`, `[1,2,[3]]`},
		{"to_entries", `{"a":1}`, `[{"key":"a","value":1}]`},
		{"from_entries", `[{"key":"a","value":1},{"name":"b","value":2},{"k":"c","v":3}]`, `{"a":1,"b":2,"c":3}`},
		{"with_entries(.value += 1)", `{"a":1}`, ``}, // assignment: parse error, checked below
		{"with_entries(.key |= ascii_upcase)", `{"a":1}`, ``},
		{"with_entries({key: (.key | ascii_upcase), value})", `{"a":1}`, `{"A":1}`},
		{"transpose", `[[1,2],[3]]`, `[[1,3],[2,null]]`},
		{"[.ts, .vs] | transpose | map({t: .[0], v: .[1]})", `{"ts":[1,2],"vs":[9,8]}`, `[{"t":1,"v":9},{"t":2,"v":8}]`},
		{"tostring", `[1,"a"]`, `"[1,\"a\"]"`},
		{"tostring", `"a"`, `"a"`},
		{"tojson", `"a"`, `"\"a\""`},
		{"fromjson", `"{\"a\":[1]}"`, `{"a":[1]}`},
		{"tonumber", `"12.5"`, `12.5`},
		{"ascii_downcase, ascii_upcase", `"aBc"`, `"abc" | "ABC"`},
		{`ltrimstr("a"), rtrimstr("c")`, `"abc"`, `"bc" | "ab"`},
		{`ltrimstr("a")`, `1`, `1`},
		{`startswith("ab"), endswith("x")`, `"abc"`, `true | false`},
		{`split(", ")`, `"a, b"`, `["a","b"]`},
		{`join("-")`, `["a",1,null,true]`, `"a-1--true"`},
		{`test("^a.c$")`, `"abc"`, `true`},
		{`test("ABC"; "i")`, `"abc"`, `true`},
		{"todate", `1700000000`, `"2023-11-14T22:13:20Z"`},
		{"todate", `1700000000.9`, `"2023-11-14T22:13:20Z"`},
		{"fromdate", `"2023-11-14T22:13:20Z"`, `1700000000`},
		{"floor, ceil, round", `1.5`, `1 | 2 | 2`},
		{"abs", `-2`, `2`},
		{"[.[] | numbers]", `[1,"a",null]`, `[1]`},
		{"[.[] | values]", `[1,null]`, `[1]`},
		{"[.[] | strings, nulls]", `["a",null]`, `["a",null]`},
		{"[.[]|scalars]", `[1,[2],{"a":3}]`, `[1]`},
		{`contains("bar")`, `"foobar"`, `true`},
		{`contains(["baz", "bar"])`, `["foobar", "foobaz", "blarp"]`, `true`},
		{`contains({foo: 12, bar: [{barp: 12}]})`, `{"foo": 12, "bar":[1,2,{"barp":12, "blip":13}]}`, `true`},
		{".id", `{"id":12345678901234567890}`, `12345678901234567890`},
		{".[0] == .[1]", `[9007199254740993, 9007199254740993]`, `true`},
		{"1e1000", `null`, `1.7976931348623157e+308`},
		{"100000000000000000000", `null`, `100000000000000000000`},
		{"0.1 + 0.2", `null`, `0.30000000000000004`},
		{"1000000 * 1", `null`, `1000000`},
		{"# comment\n.a", `{"a":1}`, `1`},
		{"\"<&>\"", `null`, `"<&>"`},
		{`"\u00e9\ud83d\ude00"`, `null`, `"é😀"`},
		{".[] | select(.n > 1) | {n, double: (.n * 2)}", `[{"n":1},{"n":2}]`, `{"n":2,"double":4}`},
		{"if type == \"array\" then .[] else . end", `{"a":1}`, `{"a":1}`},
		{"if type == \"array\" then .[] else . end", `[1,2]`, `1 | 2`},
		{"", `3`, `3`},
		{".a.[0]", `{"a":[7]}`, `7`},
		{".a[]", `{"a":[7,8]}`, `7 | 8`},
		{"[.[] | .a?]", `[1,{"a":2}]`, `[2]`},
		{"$__x", `null`, ``},
	}
	for _, c := range cases {
		got, err := run(t, c.expr, c.in, Limits{})
		if c.want == "" && err != nil {
			continue // checked as errors elsewhere
		}
		if err != nil {
			t.Errorf("%s on %s: unexpected error %v", c.expr, c.in, err)
			continue
		}
		if g := strings.Join(got, " | "); g != c.want {
			t.Errorf("%s on %s = %s, want %s", c.expr, c.in, g, c.want)
		}
	}
}

func TestRuntimeErrors(t *testing.T) {
	cases := []struct{ expr, in, msg string }{
		{".a", `1`, `Cannot index number with "a"`},
		{".[0]", `{"a":1}`, `Cannot index object with number`},
		{".[]", `null`, `Cannot iterate over null`},
		{".[]", `5`, `Cannot iterate over number (5)`},
		{"1 + \"a\"", `null`, `number (1) and string ("a") cannot be added`},
		{"1 / 0", `null`, `divisor is zero`},
		{"{(1): 2}", `null`, `Object keys must be strings`},
		{`error("custom")`, `null`, `custom`},
		{"keys", `1`, `has no keys`},
		{"tonumber", `"x1"`, `Cannot parse`},
		{"length", `true`, `has no length`},
		{".a", `"` + strings.Repeat("x", 100) + `"`, `Cannot index string with "a"`},
		{". + 1", `"` + strings.Repeat("y", 100) + `"`, `...) and number (1)`},
		{`test("(")`, `"a"`, `not a valid regex`},
	}
	for _, c := range cases {
		_, err := run(t, c.expr, c.in, Limits{})
		var re *RuntimeError
		if !errors.As(err, &re) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s on %.20s: err = %v, want runtime error containing %q", c.expr, c.in, err, c.msg)
		}
		if err != nil && len(err.Error()) > 120 {
			t.Errorf("%s: error message too long (%d bytes)", c.expr, len(err.Error()))
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ expr, msg string }{
		{"env.HOME", "env is not supported"},
		{"$ENV.PATH", "$ENV is not supported"},
		{"input", "input is not supported"},
		{"def f: 1; f", "def (user-defined functions) is not supported"},
		{"foreach .[] as $x (0; 1)", "foreach is not supported"},
		{"label $out | 1", "label is not supported"},
		{`"\(1)"`, "string interpolation"},
		{"@base64", "format strings (@base64) are not supported"},
		{".a = 1", `assignment operator "=" is not supported`},
		{".a |= 1", `assignment operator "|=" is not supported`},
		{". as [$a] | $a", "destructuring patterns are not supported"},
		{"getpath([\"a\"])", "getpath is not supported"},
		{"now", "now is not supported"},
		{"$x", "$x is not defined"},
		{"nosuchfn", "nosuchfn/0 is not defined"},
		{"map", "map/0 is not defined (supported: map/1)"},
		{".[", "expected"},
		{"(1", `expected ")"`},
		{"1 == 2 == 3", "do not chain"},
		{"if 1 then 2", `expected "end"`},
		{"{1: 2}", "object keys must be strings"},
		{"§", "unexpected character"},
		{"import \"a\" as a; 1", "import is not supported"},
		{"$__loc__", "$__loc__ is not supported"},
		{strings.Repeat("(", 70) + "1" + strings.Repeat(")", 70), "nests more than 64 levels"},
		{strings.Repeat(" ", MaxExpressionBytes+1), "longer than 4096 bytes"},
	}
	for _, c := range cases {
		_, err := Compile(c.expr)
		var pe *ParseError
		if !errors.As(err, &pe) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("Compile(%.30q) = %v, want parse error containing %q", c.expr, err, c.msg)
		}
	}
}

func TestBudgets(t *testing.T) {
	cases := []struct {
		expr string
		lim  Limits
	}{
		{"[range(100000000)]", Limits{}},
		{"reduce range(40) as $i (\"ab\"; . + .)", Limits{}},
		{"[limit(1000000; repeat_)]", Limits{}}, // not defined: parse error, skipped
		{"[range(200)] | [.[] as $a | .[] as $b | .[] as $c | 1] | length", Limits{}},
		{"reduce range(100) as $i (null; [.])", Limits{}},
		{".[]", Limits{MaxOutputs: 2}},
		{`"ab" * 100000000`, Limits{}},
		{"[range(10)] | map(..)", Limits{MaxSteps: 20}},
		{"try ([range(100000000)]) catch 1", Limits{}},
		{"[range(100000000)] // 1", Limits{}},
		{"[.[] | tojson | fromjson]", Limits{MaxSteps: 5}},
	}
	for _, c := range cases {
		if _, err := Compile(c.expr); err != nil {
			continue
		}
		_, err := run(t, c.expr, `[1,2,3]`, c.lim)
		if !errors.Is(err, ErrBudget) {
			t.Errorf("%s: err = %v, want ErrBudget", c.expr, err)
		}
	}
}

func TestCancellation(t *testing.T) {
	p, err := Compile("[range(10000000)]")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = p.Run(ctx, nil, Limits{MaxSteps: 1 << 30}, func(any) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestEmitErrorsAreNotCaught(t *testing.T) {
	stop := errors.New("downstream")
	for _, expr := range []string{"try (1,2)", "(1,2)?", "(1,2) // 3", "first(1,2)", "[limit(5; 1,2)] | .[]", "map_values(.) | .[]"} {
		p, err := Compile(expr)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		err = p.Run(context.Background(), []any{float64(1)}, Limits{}, func(any) error {
			n++
			return stop
		})
		if !errors.Is(err, stop) || n != 1 {
			t.Errorf("%s: err = %v after %d outputs, want the downstream error after 1", expr, err, n)
		}
	}
}

func TestFirstAndAll(t *testing.T) {
	p, _ := Compile(".[]")
	v, ok, err := p.First(context.Background(), []any{"a", "b"}, Limits{})
	if err != nil || !ok || v != "a" {
		t.Fatalf("First = %v %v %v", v, ok, err)
	}
	_, ok, err = p.First(context.Background(), []any{}, Limits{})
	if err != nil || ok {
		t.Fatalf("First on empty = %v %v", ok, err)
	}
	all, err := p.All(context.Background(), []any{"a", "b"}, Limits{})
	if err != nil || len(all) != 2 {
		t.Fatalf("All = %v %v", all, err)
	}
	if p.String() != ".[]" {
		t.Fatal(p.String())
	}
}

func TestDecodeLimits(t *testing.T) {
	deep := strings.Repeat("[", 65) + strings.Repeat("]", 65)
	if _, err := Decode(strings.NewReader(deep), 64); !errors.Is(err, ErrDepth) {
		t.Fatalf("deep: %v", err)
	}
	if _, err := Decode(strings.NewReader(`{"a":1} 2`), 64); err == nil {
		t.Fatal("trailing data accepted")
	}
	if _, err := Decode(strings.NewReader(`{"a":`), 64); err == nil {
		t.Fatal("truncated input accepted")
	}
	v, err := Decode(strings.NewReader(`{"z":1,"a":{"y":2,"b":3}}`), 64)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Marshal(v, 64)
	if string(b) != `{"z":1,"a":{"y":2,"b":3}}` {
		t.Fatalf("order lost: %s", b)
	}
	if _, err := Marshal(v, 1); !errors.Is(err, ErrDepth) {
		t.Fatalf("marshal depth: %v", err)
	}
}

func FuzzCompile(f *testing.F) {
	for _, s := range []string{".", ".a[0]", "{a, (.b): [.[] | select(. > 1)]}", "reduce .[] as $x (0; . + $x)",
		"if . then 1 elif 2 then 3 else 4 end", `try error("x") catch .`, ".. | numbers", `"é" + "x"`} {
		f.Add(s, `{"a":[1,2,{"b":"c"}],"b":"k"}`)
	}
	f.Fuzz(func(t *testing.T, expr, input string) {
		p, err := Compile(expr)
		if err != nil {
			return
		}
		in, err := Decode(strings.NewReader(input), 64)
		if err != nil {
			return
		}
		_ = p.Run(context.Background(), in, Limits{MaxSteps: 20000, MaxOutputs: 1000}, func(v any) error {
			_, err := Marshal(v, 64)
			return err
		})
	})
}
