// Package expr is the automation expression language: a small, total, side-effect
// free evaluator. There is no eval, no reflection beyond JSON-shaped values, no
// loops, no assignment and no user-defined functions.
//
// Grammar (lowest to highest precedence):
//
//	or      := and ( "||" and )*
//	and     := eq ( "&&" eq )*
//	eq      := cmp ( ("==" | "!=") cmp )*
//	cmp     := unary ( ("<" | "<=" | ">" | ">=" | "contains" | "startsWith" | "endsWith" | "in") unary )*
//	unary   := ("!" | "-") unary | postfix
//	postfix := primary ( "." ident | "[" or "]" )*
//	primary := number | string | true | false | null | ident | ident "(" args ")" | "(" or ")"
//
// Values are JSON values (nil, bool, float64, string, []any, map[string]any).
// Reading a missing field yields null instead of an error, so a filter like
// `event.data.conclusion == 'failure'` is simply false when the field is absent.
// Ordering comparisons between different types are errors; == between different
// types is false. Truthiness: null, false, 0, "", [] and {} are false.
//
// Functions: len(x), lower(s), upper(s), string(x), number(s).
package expr

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits protect the daemon from pathological input.
const (
	MaxSourceLen = 2048
	MaxDepth     = 32
	MaxNodes     = 512
)

// Expr is a parsed expression.
type Expr struct {
	src  string
	root node
}

// Source returns the original text.
func (e *Expr) Source() string { return e.src }

// Parse parses src.
func Parse(src string) (*Expr, error) {
	if len(src) > MaxSourceLen {
		return nil, fmt.Errorf("expression longer than %d bytes", MaxSourceLen)
	}
	if !utf8.ValidString(src) {
		return nil, fmt.Errorf("expression is not valid UTF-8")
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, p.errf("unexpected %s", p.peek().describe())
	}
	return &Expr{src: src, root: n}, nil
}

// Eval evaluates against env (the root identifiers).
func (e *Expr) Eval(env map[string]any) (any, error) { return e.root.eval(env) }

// EvalBool evaluates and applies truthiness.
func (e *Expr) EvalBool(env map[string]any) (bool, error) {
	v, err := e.root.eval(env)
	if err != nil {
		return false, err
	}
	return Truthy(v), nil
}

// Truthy implements the language's truthiness rules.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// ---- lexer ----

type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tStr
	tIdent
	tOp // punctuation and operators, text in tok.s
)

type token struct {
	kind tokKind
	s    string
	n    float64
	pos  int
}

func (t token) describe() string {
	switch t.kind {
	case tEOF:
		return "end of expression"
	case tStr:
		return "string"
	case tNum:
		return "number"
	}
	return fmt.Sprintf("%q", t.s)
}

func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			f, err := strconv.ParseFloat(src[i:j], 64)
			if err != nil || math.IsInf(f, 0) {
				return nil, fmt.Errorf("position %d: bad number %q", i, src[i:j])
			}
			out = append(out, token{kind: tNum, n: f, pos: i})
			i = j
		case c == '\'' || c == '"':
			s, j, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			out = append(out, token{kind: tStr, s: s, pos: i})
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			out = append(out, token{kind: tIdent, s: src[i:j], pos: i})
			i = j
		default:
			if i+1 < len(src) {
				switch two := src[i : i+2]; two {
				case "==", "!=", "<=", ">=", "&&", "||":
					out = append(out, token{kind: tOp, s: two, pos: i})
					i += 2
					continue
				}
			}
			switch c {
			case '<', '>', '!', '(', ')', '[', ']', '.', ',', '-':
				out = append(out, token{kind: tOp, s: string(c), pos: i})
				i++
			default:
				return nil, fmt.Errorf("position %d: unexpected character %q", i, string(rune(c)))
			}
		}
	}
	out = append(out, token{kind: tEOF, pos: len(src)})
	return out, nil
}

func isIdentStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isIdentPart(c byte) bool  { return isIdentStart(c) || c >= '0' && c <= '9' || c == '-' }

func lexString(src string, start int) (string, int, error) {
	q := src[start]
	var b strings.Builder
	i := start + 1
	for i < len(src) {
		c := src[i]
		switch {
		case c == q:
			return b.String(), i + 1, nil
		case c == '\\':
			i++
			if i >= len(src) {
				return "", 0, fmt.Errorf("position %d: unterminated string", start)
			}
			switch src[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\', '\'', '"':
				b.WriteByte(src[i])
			default:
				return "", 0, fmt.Errorf("position %d: unknown escape \\%c", i-1, src[i])
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", 0, fmt.Errorf("position %d: unterminated string", start)
}

// ---- parser ----

type parser struct {
	toks  []token
	i     int
	depth int
	nodes int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }
func (p *parser) errf(format string, a ...any) error {
	return fmt.Errorf("position %d: %s", p.peek().pos, fmt.Sprintf(format, a...))
}

func (p *parser) isOp(s string) bool {
	t := p.peek()
	return t.kind == tOp && t.s == s
}

func (p *parser) isWord(s string) bool {
	t := p.peek()
	return t.kind == tIdent && t.s == s
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > MaxDepth {
		return p.errf("expression nested deeper than %d", MaxDepth)
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) mk(n node) (node, error) {
	p.nodes++
	if p.nodes > MaxNodes {
		return nil, fmt.Errorf("expression has more than %d nodes", MaxNodes)
	}
	return n, nil
}

func (p *parser) parseOr() (node, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isOp("||") {
		p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		if l, err = p.mk(&logic{or: true, l: l, r: r}); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (p *parser) parseAnd() (node, error) {
	l, err := p.parseEq()
	if err != nil {
		return nil, err
	}
	for p.isOp("&&") {
		p.next()
		r, err := p.parseEq()
		if err != nil {
			return nil, err
		}
		if l, err = p.mk(&logic{l: l, r: r}); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (p *parser) parseEq() (node, error) {
	l, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.isOp("==") || p.isOp("!=") {
		op := p.next().s
		r, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		if l, err = p.mk(&binary{op: op, l: l, r: r}); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (p *parser) parseCmp() (node, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		isCmp := t.kind == tOp && (t.s == "<" || t.s == "<=" || t.s == ">" || t.s == ">=") ||
			t.kind == tIdent && (t.s == "contains" || t.s == "startsWith" || t.s == "endsWith" || t.s == "in")
		if !isCmp {
			return l, nil
		}
		p.next()
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if l, err = p.mk(&binary{op: t.s, l: l, r: r}); err != nil {
			return nil, err
		}
	}
}

func (p *parser) parseUnary() (node, error) {
	if p.isOp("!") || p.isOp("-") {
		if err := p.enter(); err != nil {
			return nil, err
		}
		defer p.leave()
		op := p.next().s
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return p.mk(&unary{op: op, x: x})
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (node, error) {
	n, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.isOp("."):
			p.next()
			t := p.next()
			if t.kind != tIdent {
				p.i--
				return nil, p.errf("expected field name after '.'")
			}
			if n, err = p.mk(&field{x: n, name: t.s}); err != nil {
				return nil, err
			}
		case p.isOp("["):
			p.next()
			idx, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			if !p.isOp("]") {
				return nil, p.errf("expected ']'")
			}
			p.next()
			if n, err = p.mk(&index{x: n, idx: idx}); err != nil {
				return nil, err
			}
		default:
			return n, nil
		}
	}
}

var funcs = map[string]int{"len": 1, "lower": 1, "upper": 1, "string": 1, "number": 1}

func (p *parser) parsePrimary() (node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return p.mk(&lit{v: t.n})
	case tStr:
		return p.mk(&lit{v: t.s})
	case tIdent:
		switch t.s {
		case "true":
			return p.mk(&lit{v: true})
		case "false":
			return p.mk(&lit{v: false})
		case "null":
			return p.mk(&lit{v: nil})
		case "contains", "startsWith", "endsWith", "in":
			p.i--
			return nil, p.errf("unexpected operator %q", t.s)
		}
		if p.isOp("(") {
			want, ok := funcs[t.s]
			if !ok {
				p.i--
				return nil, p.errf("unknown function %q", t.s)
			}
			p.next()
			var args []node
			for !p.isOp(")") {
				if len(args) > 0 {
					if !p.isOp(",") {
						return nil, p.errf("expected ',' or ')'")
					}
					p.next()
				}
				a, err := p.parseOr()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
			}
			p.next()
			if len(args) != want {
				return nil, fmt.Errorf("position %d: %s takes %d argument(s)", t.pos, t.s, want)
			}
			return p.mk(&call{name: t.s, args: args})
		}
		return p.mk(&ident{name: t.s})
	case tOp:
		if t.s == "(" {
			x, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			if !p.isOp(")") {
				return nil, p.errf("expected ')'")
			}
			p.next()
			return x, nil
		}
	}
	p.i--
	return nil, p.errf("unexpected %s", t.describe())
}

// ---- evaluation ----

type node interface {
	eval(env map[string]any) (any, error)
}

type (
	lit   struct{ v any }
	ident struct{ name string }
	field struct {
		x    node
		name string
	}
	index struct{ x, idx node }
	unary struct {
		op string
		x  node
	}
	binary struct {
		op   string
		l, r node
	}
	logic struct {
		or   bool
		l, r node
	}
	call struct {
		name string
		args []node
	}
)

func (n *lit) eval(map[string]any) (any, error) { return n.v, nil }

func (n *ident) eval(env map[string]any) (any, error) {
	v, ok := env[n.name]
	if !ok {
		return nil, nil
	}
	return normalize(v), nil
}

func (n *field) eval(env map[string]any) (any, error) {
	x, err := n.x.eval(env)
	if err != nil {
		return nil, err
	}
	m, ok := x.(map[string]any)
	if !ok {
		return nil, nil
	}
	return normalize(m[n.name]), nil
}

func (n *index) eval(env map[string]any) (any, error) {
	x, err := n.x.eval(env)
	if err != nil {
		return nil, err
	}
	i, err := n.idx.eval(env)
	if err != nil {
		return nil, err
	}
	switch c := x.(type) {
	case []any:
		f, ok := i.(float64)
		if !ok || f != math.Trunc(f) || f < 0 || f >= float64(len(c)) {
			return nil, nil
		}
		return normalize(c[int(f)]), nil
	case map[string]any:
		s, ok := i.(string)
		if !ok {
			return nil, nil
		}
		return normalize(c[s]), nil
	}
	return nil, nil
}

func (n *unary) eval(env map[string]any) (any, error) {
	x, err := n.x.eval(env)
	if err != nil {
		return nil, err
	}
	if n.op == "!" {
		return !Truthy(x), nil
	}
	f, ok := x.(float64)
	if !ok {
		return nil, fmt.Errorf("cannot negate %s", typeName(x))
	}
	return -f, nil
}

func (n *logic) eval(env map[string]any) (any, error) {
	l, err := n.l.eval(env)
	if err != nil {
		return nil, err
	}
	if n.or == Truthy(l) {
		return n.or, nil // short circuit
	}
	r, err := n.r.eval(env)
	if err != nil {
		return nil, err
	}
	return Truthy(r), nil
}

func (n *binary) eval(env map[string]any) (any, error) {
	l, err := n.l.eval(env)
	if err != nil {
		return nil, err
	}
	r, err := n.r.eval(env)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "==":
		return equal(l, r), nil
	case "!=":
		return !equal(l, r), nil
	case "<", "<=", ">", ">=":
		c, err := order(l, r)
		if err != nil {
			return nil, err
		}
		switch n.op {
		case "<":
			return c < 0, nil
		case "<=":
			return c <= 0, nil
		case ">":
			return c > 0, nil
		}
		return c >= 0, nil
	case "contains":
		return contains(l, r)
	case "in":
		return contains(r, l)
	case "startsWith", "endsWith":
		a, ok1 := l.(string)
		b, ok2 := r.(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%s needs two strings, got %s and %s", n.op, typeName(l), typeName(r))
		}
		if n.op == "startsWith" {
			return strings.HasPrefix(a, b), nil
		}
		return strings.HasSuffix(a, b), nil
	}
	return nil, fmt.Errorf("unknown operator %q", n.op)
}

func (n *call) eval(env map[string]any) (any, error) {
	a, err := n.args[0].eval(env)
	if err != nil {
		return nil, err
	}
	switch n.name {
	case "len":
		switch x := a.(type) {
		case nil:
			return 0.0, nil
		case string:
			return float64(utf8.RuneCountInString(x)), nil
		case []any:
			return float64(len(x)), nil
		case map[string]any:
			return float64(len(x)), nil
		}
		return nil, fmt.Errorf("len of %s", typeName(a))
	case "lower", "upper":
		s, ok := a.(string)
		if !ok {
			return nil, fmt.Errorf("%s needs a string, got %s", n.name, typeName(a))
		}
		if n.name == "lower" {
			return strings.ToLower(s), nil
		}
		return strings.ToUpper(s), nil
	case "string":
		return Stringify(a), nil
	case "number":
		switch x := a.(type) {
		case float64:
			return x, nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("number(%q): not a number", x)
			}
			return f, nil
		case bool:
			if x {
				return 1.0, nil
			}
			return 0.0, nil
		}
		return nil, fmt.Errorf("number of %s", typeName(a))
	}
	return nil, fmt.Errorf("unknown function %q", n.name)
}

// normalize maps Go numeric types and typed containers onto the JSON model so
// callers may hand the evaluator values that did not come from json.Unmarshal.
func normalize(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint:
		return float64(x)
	case uint32:
		return float64(x)
	case uint64:
		return float64(x)
	case float32:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case map[string]string:
		m := make(map[string]any, len(x))
		for k, s := range x {
			m[k] = s
		}
		return m
	case []string:
		a := make([]any, len(x))
		for i, s := range x {
			a[i] = s
		}
		return a
	}
	return v
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "list"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func equal(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(normalize(x[i]), normalize(y[i])) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !equal(normalize(v), normalize(w)) {
				return false
			}
		}
		return true
	}
	return false
}

func order(a, b any) (int, error) {
	switch x := a.(type) {
	case float64:
		if y, ok := b.(float64); ok {
			switch {
			case x < y:
				return -1, nil
			case x > y:
				return 1, nil
			}
			return 0, nil
		}
	case string:
		if y, ok := b.(string); ok {
			return strings.Compare(x, y), nil
		}
	}
	return 0, fmt.Errorf("cannot order %s and %s", typeName(a), typeName(b))
}

func contains(container, item any) (any, error) {
	switch c := container.(type) {
	case nil:
		return false, nil
	case string:
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("string contains needs a string, got %s", typeName(item))
		}
		return strings.Contains(c, s), nil
	case []any:
		for _, e := range c {
			if equal(normalize(e), item) {
				return true, nil
			}
		}
		return false, nil
	case map[string]any:
		s, ok := item.(string)
		if !ok {
			return false, nil
		}
		_, has := c[s]
		return has, nil
	}
	return nil, fmt.Errorf("%s cannot contain values", typeName(container))
}

// Stringify renders a value for templates and string(): null is empty, numbers
// have no exponent, containers are compact JSON.
func Stringify(v any) string {
	switch x := normalize(v).(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
