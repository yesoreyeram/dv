package jq

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parser limits (FR-073 AC5).
const (
	MaxExpressionBytes = 4096
	MaxParseDepth      = 64
)

// ParseError reports a syntax error or an unsupported construct with its byte offset.
type ParseError struct {
	Offset int
	Msg    string
}

func (e *ParseError) Error() string { return fmt.Sprintf("jq: %s at offset %d", e.Msg, e.Offset) }

type tokKind int

const (
	tEOF     tokKind = iota
	tDot             // .
	tRecurse         // ..
	tField           // .name
	tIdent           // name, keywords
	tVar             // $name
	tNumber          // 1.5
	tString          // "..."
	tFormat          // @base64
	tOp              // punctuation and operators
)

type token struct {
	kind tokKind
	text string // identifier/field/var name, operator text, or decoded string
	num  json.Number
	pos  int
}

func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdentPart(c byte) bool  { return isIdentStart(c) || (c >= '0' && c <= '9') }

func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '.':
			switch {
			case i+1 < len(src) && src[i+1] == '.':
				toks = append(toks, token{kind: tRecurse, pos: i})
				i += 2
			case i+1 < len(src) && isIdentStart(src[i+1]):
				j := i + 1
				for j < len(src) && isIdentPart(src[j]) {
					j++
				}
				toks = append(toks, token{kind: tField, text: src[i+1 : j], pos: i})
				i = j
			case i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
				j, n, err := lexNumber(src, i)
				if err != nil {
					return nil, err
				}
				toks = append(toks, token{kind: tNumber, num: n, pos: i})
				i = j
			default:
				toks = append(toks, token{kind: tDot, pos: i})
				i++
			}
		case c == '$':
			j := i + 1
			if j < len(src) && isIdentStart(src[j]) {
				for j < len(src) && isIdentPart(src[j]) {
					j++
				}
				toks = append(toks, token{kind: tVar, text: src[i+1 : j], pos: i})
				i = j
			} else {
				return nil, &ParseError{i, "expected a variable name after $"}
			}
		case c == '@':
			j := i + 1
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			toks = append(toks, token{kind: tFormat, text: src[i:j], pos: i})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(src) && (isIdentPart(src[j]) || (src[j] == ':' && j+1 < len(src) && src[j+1] == ':')) {
				if src[j] == ':' {
					j += 2
					continue
				}
				j++
			}
			toks = append(toks, token{kind: tIdent, text: src[i:j], pos: i})
			i = j
		case c >= '0' && c <= '9':
			j, n, err := lexNumber(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tNumber, num: n, pos: i})
			i = j
		case c == '"':
			s, j, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tString, text: s, pos: i})
			i = j
		default:
			op := ""
			for _, cand := range []string{"?//", "|=", "+=", "-=", "*=", "/=", "%=", "//=", "==", "!=", "<=", ">=", "//"} {
				if strings.HasPrefix(src[i:], cand) {
					op = cand
					break
				}
			}
			if op == "" {
				if !strings.ContainsRune("|,+-*/%<>=()[]{}:;?", rune(c)) {
					r, _ := utf8.DecodeRuneInString(src[i:])
					return nil, &ParseError{i, fmt.Sprintf("unexpected character %q", r)}
				}
				op = string(c)
			}
			toks = append(toks, token{kind: tOp, text: op, pos: i})
			i += len(op)
		}
	}
	return append(toks, token{kind: tEOF, pos: len(src)}), nil
}

func lexNumber(src string, i int) (int, json.Number, error) {
	j := i
	for j < len(src) && src[j] >= '0' && src[j] <= '9' {
		j++
	}
	if j < len(src) && src[j] == '.' {
		j++
		for j < len(src) && src[j] >= '0' && src[j] <= '9' {
			j++
		}
	}
	if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
		k := j + 1
		if k < len(src) && (src[k] == '+' || src[k] == '-') {
			k++
		}
		if k < len(src) && src[k] >= '0' && src[k] <= '9' {
			for k < len(src) && src[k] >= '0' && src[k] <= '9' {
				k++
			}
			j = k
		}
	}
	text := src[i:j]
	if _, err := strconv.ParseFloat(text, 64); err != nil && !strings.Contains(err.Error(), "range") {
		return 0, "", &ParseError{i, fmt.Sprintf("invalid number %q", text)}
	}
	// Normalise to valid JSON number text: ".5" -> "0.5", "1." -> "1".
	if strings.HasPrefix(text, ".") {
		text = "0" + text
	}
	text = strings.Replace(text, ".e", "e", 1)
	text = strings.Replace(text, ".E", "E", 1)
	text = strings.TrimSuffix(text, ".")
	return j, json.Number(text), nil
}

func lexString(src string, i int) (string, int, error) {
	var b strings.Builder
	j := i + 1
	for j < len(src) {
		c := src[j]
		switch {
		case c == '"':
			return b.String(), j + 1, nil
		case c == '\\':
			if j+1 >= len(src) {
				return "", 0, &ParseError{j, "unterminated string"}
			}
			e := src[j+1]
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
				j += 2
			case 'b':
				b.WriteByte('\b')
				j += 2
			case 'f':
				b.WriteByte('\f')
				j += 2
			case 'n':
				b.WriteByte('\n')
				j += 2
			case 'r':
				b.WriteByte('\r')
				j += 2
			case 't':
				b.WriteByte('\t')
				j += 2
			case 'u':
				if j+6 > len(src) {
					return "", 0, &ParseError{j, "invalid \\u escape"}
				}
				r, ok := hex4(src[j+2 : j+6])
				if !ok {
					return "", 0, &ParseError{j, "invalid \\u escape"}
				}
				j += 6
				if r >= 0xD800 && r < 0xDC00 && j+6 <= len(src) && src[j] == '\\' && src[j+1] == 'u' {
					if lo, ok := hex4(src[j+2 : j+6]); ok && lo >= 0xDC00 && lo < 0xE000 {
						r = (r-0xD800)<<10 + (lo - 0xDC00) + 0x10000
						j += 6
					}
				}
				b.WriteRune(r)
			case '(':
				return "", 0, &ParseError{j, "string interpolation \\( … ) is not supported"}
			default:
				return "", 0, &ParseError{j, fmt.Sprintf("invalid escape \\%c", e)}
			}
		case c < 0x20:
			return "", 0, &ParseError{j, "control character in string literal"}
		default:
			b.WriteByte(c)
			j++
		}
	}
	return "", 0, &ParseError{i, "unterminated string"}
}

// hex4 decodes exactly four hex digits.
func hex4(s string) (rune, bool) {
	if len(s) != 4 {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		c := s[i]
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | rune(d)
	}
	return r, true
}

// ---- AST ----

type node interface{}

type (
	identity struct{}
	recurse  struct{}
	literal  struct{ v any }
	index    struct {
		target node
		key    node // evaluated key (string or number)
	}
	slice struct {
		target   node
		from, to node // nil means open
	}
	iterate struct{ target node }
	tryNode struct {
		body  node
		catch node // nil: suppress errors
	}
	pipe    struct{ left, right node }
	comma   struct{ left, right node }
	alt     struct{ left, right node }
	andNode struct{ left, right node }
	orNode  struct{ left, right node }
	binary  struct {
		op          string
		left, right node
	}
	neg      struct{ x node }
	arrayCon struct{ body node } // nil body: []
	objEntry struct {
		key   node // evaluates to the key string
		value node
	}
	objectCon struct{ entries []objEntry }
	ifNode    struct {
		cond, then, els node // els nil: identity
	}
	asNode struct {
		source node
		name   string
		body   node
	}
	reduceNode struct {
		source       node
		name         string
		init, update node
	}
	varRef  struct{ name string }
	funcall struct {
		name string
		args []node
		pos  int
	}
)

type parser struct {
	toks  []token
	pos   int
	depth int
	vars  []string // variables in scope, for compile-time checks
	noAs  bool     // the next postfix term is a reduce source: "as" belongs to reduce
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) isOp(s string) bool {
	t := p.peek()
	return t.kind == tOp && t.text == s
}

func (p *parser) isKeyword(s string) bool {
	t := p.peek()
	return t.kind == tIdent && t.text == s
}

func (p *parser) errf(t token, format string, a ...any) error {
	return &ParseError{t.pos, fmt.Sprintf(format, a...)}
}

func (p *parser) expectOp(s string) error {
	t := p.next()
	if t.kind != tOp || t.text != s {
		return p.errf(t, "expected %q", s)
	}
	return nil
}

func (p *parser) expectKeyword(s string) error {
	t := p.next()
	if t.kind != tIdent || t.text != s {
		return p.errf(t, "expected %q", s)
	}
	return nil
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > MaxParseDepth {
		return p.errf(p.peek(), "expression nests more than %d levels", MaxParseDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func parse(src string) (node, error) {
	if len(src) > MaxExpressionBytes {
		return nil, &ParseError{MaxExpressionBytes, fmt.Sprintf("expression is longer than %d bytes", MaxExpressionBytes)}
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if p.peek().kind == tEOF {
		return identity{}, nil
	}
	n, err := p.parsePipe(false)
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		if t.kind == tOp {
			return nil, p.errf(t, "unexpected %q", t.text)
		}
		return nil, p.errf(t, "unexpected token")
	}
	return n, nil
}

// unsupportedKeywords are jq keywords outside the supported subset (FR-073 AC3).
var unsupportedKeywords = map[string]string{
	"def": "def (user-defined functions)", "foreach": "foreach", "label": "label", "import": "import",
	"include": "include", "__loc__": "$__loc__",
}

// parsePipe parses `a | b`, `a as $x | b`; noComma stops at ',' (object values).
func (p *parser) parsePipe(noComma bool) (node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	var left node
	var err error
	if noComma {
		left, err = p.parseAlt()
	} else {
		left, err = p.parseComma()
	}
	if err != nil {
		return nil, err
	}
	if p.isOp("|") {
		p.next()
		right, err := p.parsePipe(noComma)
		if err != nil {
			return nil, err
		}
		return pipe{left, right}, nil
	}
	return left, nil
}

func (p *parser) parseComma() (node, error) {
	left, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	for p.isOp(",") {
		p.next()
		right, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		left = comma{left, right}
	}
	return left, nil
}

// parseAlt parses `a // b` (right-associative) and rejects assignment operators.
func (p *parser) parseAlt() (node, error) {
	left, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind == tOp {
		switch t.text {
		case "=", "|=", "+=", "-=", "*=", "/=", "%=", "//=":
			return nil, p.errf(t, "assignment operator %q is not supported", t.text)
		case "?//":
			return nil, p.errf(t, "?// alternative destructuring is not supported")
		}
	}
	if p.isOp("//") {
		p.next()
		if err := p.enter(); err != nil {
			return nil, err
		}
		right, err := p.parseAlt()
		p.leave()
		if err != nil {
			return nil, err
		}
		return alt{left, right}, nil
	}
	return left, nil
}

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("or") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orNode{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseCompare()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("and") {
		p.next()
		right, err := p.parseCompare()
		if err != nil {
			return nil, err
		}
		left = andNode{left, right}
	}
	return left, nil
}

func (p *parser) parseCompare() (node, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind == tOp {
		switch t.text {
		case "==", "!=", "<", "<=", ">", ">=":
			p.next()
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			if t2 := p.peek(); t2.kind == tOp {
				switch t2.text {
				case "==", "!=", "<", "<=", ">", ">=":
					return nil, p.errf(t2, "comparison operators do not chain; use parentheses")
				}
			}
			return binary{t.text, left, right}, nil
		}
	}
	return left, nil
}

func (p *parser) parseAdditive() (node, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.isOp("+") || p.isOp("-") {
		op := p.next().text
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = binary{op, left, right}
	}
	return left, nil
}

func (p *parser) parseMultiplicative() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.isOp("*") || p.isOp("/") || p.isOp("%") {
		op := p.next().text
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binary{op, left, right}
	}
	return left, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.isOp("-") {
		p.next()
		if err := p.enter(); err != nil {
			return nil, err
		}
		x, err := p.parseUnary()
		p.leave()
		if err != nil {
			return nil, err
		}
		return neg{x}, nil
	}
	return p.parsePostfix()
}

// parsePostfix parses a term followed by suffixes: .a, ."a", [..], ?, as well as
// `.a?`.
func (p *parser) parsePostfix() (node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	noAs := p.noAs
	p.noAs = false
	t, err := p.parseSuffixes()
	if err != nil {
		return nil, err
	}
	if noAs || !p.isKeyword("as") {
		return t, nil
	}
	// Term "as" $var "|" Pipe: the body extends as far right as a pipe does.
	p.next()
	vt := p.next()
	if vt.kind != tVar {
		if vt.kind == tOp && (vt.text == "[" || vt.text == "{") {
			return nil, p.errf(vt, "destructuring patterns are not supported")
		}
		return nil, p.errf(vt, "expected a $variable after as")
	}
	if vt.text == "ENV" || vt.text == "__loc__" {
		return nil, p.errf(vt, "$%s cannot be rebound", vt.text)
	}
	if p.isOp("?//") {
		return nil, p.errf(p.peek(), "?// alternative destructuring is not supported")
	}
	if err := p.expectOp("|"); err != nil {
		return nil, err
	}
	p.vars = append(p.vars, vt.text)
	body, err := p.parsePipe(false)
	p.vars = p.vars[:len(p.vars)-1]
	if err != nil {
		return nil, err
	}
	return asNode{source: t, name: vt.text, body: body}, nil
}

// parseSuffixes parses a term and its suffixes.
func (p *parser) parseSuffixes() (node, error) {
	t, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		switch tk := p.peek(); {
		case tk.kind == tField:
			p.next()
			t = index{target: t, key: literal{tk.text}}
		case tk.kind == tDot && p.toks[p.pos+1].kind == tString:
			p.next()
			s := p.next()
			t = index{target: t, key: literal{s.text}}
		case tk.kind == tDot && p.toks[p.pos+1].kind == tOp && p.toks[p.pos+1].text == "[":
			p.next()
			t, err = p.parseBracket(t)
			if err != nil {
				return nil, err
			}
		case tk.kind == tOp && tk.text == "[":
			t, err = p.parseBracket(t)
			if err != nil {
				return nil, err
			}
		case tk.kind == tOp && tk.text == "?":
			p.next()
			t = tryNode{body: t}
		default:
			return t, nil
		}
	}
}

// parseBracket parses [], [e], [e:], [:e], [e:e] after target.
func (p *parser) parseBracket(target node) (node, error) {
	if err := p.expectOp("["); err != nil {
		return nil, err
	}
	if p.isOp("]") {
		p.next()
		return iterate{target}, nil
	}
	var from node
	if !p.isOp(":") {
		e, err := p.parsePipe(false)
		if err != nil {
			return nil, err
		}
		from = e
	}
	if p.isOp(":") {
		p.next()
		var to node
		if !p.isOp("]") {
			e, err := p.parsePipe(false)
			if err != nil {
				return nil, err
			}
			to = e
		}
		if err := p.expectOp("]"); err != nil {
			return nil, err
		}
		if from == nil && to == nil {
			return nil, p.errf(p.peek(), "a slice needs a start or an end")
		}
		return slice{target, from, to}, nil
	}
	if err := p.expectOp("]"); err != nil {
		return nil, err
	}
	return index{target, from}, nil
}

func (p *parser) parseTerm() (node, error) {
	t := p.peek()
	switch t.kind {
	case tDot:
		p.next()
		if p.peek().kind == tString {
			s := p.next()
			return index{identity{}, literal{s.text}}, nil
		}
		return identity{}, nil
	case tRecurse:
		p.next()
		return recurse{}, nil
	case tField:
		p.next()
		return index{identity{}, literal{t.text}}, nil
	case tNumber:
		p.next()
		return literal{t.num}, nil
	case tString:
		p.next()
		return literal{t.text}, nil
	case tFormat:
		return nil, p.errf(t, "format strings (%s) are not supported", t.text)
	case tVar:
		p.next()
		switch t.text {
		case "ENV":
			return nil, p.errf(t, "$ENV is not supported")
		case "__loc__", "__prog_args":
			return nil, p.errf(t, "$%s is not supported", t.text)
		}
		if !p.inScope(t.text) {
			return nil, p.errf(t, "$%s is not defined", t.text)
		}
		return varRef{t.text}, nil
	case tOp:
		switch t.text {
		case "(":
			p.next()
			e, err := p.parsePipe(false)
			if err != nil {
				return nil, err
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			return e, nil
		case "[":
			p.next()
			if p.isOp("]") {
				p.next()
				return arrayCon{}, nil
			}
			e, err := p.parsePipe(false)
			if err != nil {
				return nil, err
			}
			if err := p.expectOp("]"); err != nil {
				return nil, err
			}
			return arrayCon{e}, nil
		case "{":
			return p.parseObject()
		}
		return nil, p.errf(t, "unexpected %q", t.text)
	case tIdent:
		return p.parseIdentTerm()
	case tEOF:
		return nil, p.errf(t, "unexpected end of expression")
	}
	return nil, p.errf(t, "unexpected token")
}

func (p *parser) inScope(name string) bool {
	for i := len(p.vars) - 1; i >= 0; i-- {
		if p.vars[i] == name {
			return true
		}
	}
	return false
}

func (p *parser) parseIdentTerm() (node, error) {
	t := p.next()
	if what, ok := unsupportedKeywords[t.text]; ok {
		return nil, p.errf(t, "%s is not supported", what)
	}
	if strings.Contains(t.text, "::") {
		return nil, p.errf(t, "modules are not supported")
	}
	switch t.text {
	case "true":
		return literal{true}, nil
	case "false":
		return literal{false}, nil
	case "null":
		return literal{nil}, nil
	case "if":
		return p.parseIf()
	case "try":
		body, err := p.parsePostfixNoTry()
		if err != nil {
			return nil, err
		}
		var c node
		if p.isKeyword("catch") {
			p.next()
			c, err = p.parsePostfixNoTry()
			if err != nil {
				return nil, err
			}
		}
		return tryNode{body: body, catch: c}, nil
	case "reduce":
		p.noAs = true
		src, err := p.parsePostfix()
		if err != nil {
			return nil, err
		}
		if err := p.expectKeyword("as"); err != nil {
			return nil, err
		}
		vt := p.next()
		if vt.kind != tVar {
			return nil, p.errf(vt, "expected a $variable after as")
		}
		if err := p.expectOp("("); err != nil {
			return nil, err
		}
		init, err := p.parsePipe(false)
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(";"); err != nil {
			return nil, err
		}
		p.vars = append(p.vars, vt.text)
		upd, err := p.parsePipe(false)
		p.vars = p.vars[:len(p.vars)-1]
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		return reduceNode{source: src, name: vt.text, init: init, update: upd}, nil
	case "then", "elif", "else", "end", "as", "catch", "and", "or":
		return nil, p.errf(t, "unexpected %q", t.text)
	}
	// Function call: name or name(a; b).
	var args []node
	if p.isOp("(") {
		p.next()
		for {
			a, err := p.parsePipe(false)
			if err != nil {
				return nil, err
			}
			args = append(args, a)
			if p.isOp(";") {
				p.next()
				continue
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			break
		}
	}
	if err := checkBuiltin(t.text, len(args)); err != nil {
		return nil, p.errf(t, "%s", err)
	}
	return funcall{name: t.text, args: args, pos: t.pos}, nil
}

// parsePostfixNoTry parses the body of `try` (a postfix term).
func (p *parser) parsePostfixNoTry() (node, error) { return p.parsePostfix() }

func (p *parser) parseIf() (node, error) {
	cond, err := p.parsePipe(false)
	if err != nil {
		return nil, err
	}
	if err := p.expectKeyword("then"); err != nil {
		return nil, err
	}
	then, err := p.parsePipe(false)
	if err != nil {
		return nil, err
	}
	n := ifNode{cond: cond, then: then}
	switch {
	case p.isKeyword("elif"):
		p.next()
		if err := p.enter(); err != nil {
			return nil, err
		}
		rest, err := p.parseIf()
		p.leave()
		if err != nil {
			return nil, err
		}
		n.els = rest
		return n, nil
	case p.isKeyword("else"):
		p.next()
		els, err := p.parsePipe(false)
		if err != nil {
			return nil, err
		}
		n.els = els
	}
	if err := p.expectKeyword("end"); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) parseObject() (node, error) {
	if err := p.expectOp("{"); err != nil {
		return nil, err
	}
	var entries []objEntry
	if p.isOp("}") {
		p.next()
		return objectCon{}, nil
	}
	for {
		t := p.next()
		var key node
		var shorthand node
		switch t.kind {
		case tIdent:
			key = literal{t.text}
			shorthand = index{identity{}, literal{t.text}}
		case tVar:
			if t.text == "ENV" || t.text == "__loc__" {
				return nil, p.errf(t, "$%s is not supported", t.text)
			}
			if !p.inScope(t.text) {
				return nil, p.errf(t, "$%s is not defined", t.text)
			}
			key = literal{t.text}
			shorthand = varRef{t.text}
		case tString:
			key = literal{t.text}
			shorthand = index{identity{}, literal{t.text}}
		case tNumber:
			return nil, p.errf(t, "object keys must be strings")
		case tFormat:
			return nil, p.errf(t, "format strings (%s) are not supported", t.text)
		case tOp:
			if t.text != "(" {
				return nil, p.errf(t, "expected an object key")
			}
			k, err := p.parsePipe(false)
			if err != nil {
				return nil, err
			}
			if err := p.expectOp(")"); err != nil {
				return nil, err
			}
			key = k
		default:
			return nil, p.errf(t, "expected an object key")
		}
		var value node
		if p.isOp(":") {
			p.next()
			v, err := p.parsePipe(true)
			if err != nil {
				return nil, err
			}
			value = v
		} else {
			if shorthand == nil {
				return nil, p.errf(p.peek(), "expected ':' after a computed key")
			}
			value = shorthand
		}
		entries = append(entries, objEntry{key, value})
		if p.isOp(",") {
			p.next()
			continue
		}
		if err := p.expectOp("}"); err != nil {
			return nil, err
		}
		return objectCon{entries}, nil
	}
}
