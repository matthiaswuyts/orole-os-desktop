package expr

import (
	"fmt"
	"strings"
)

// MaxPlaceholders bounds `{{ }}` segments per template string.
const MaxPlaceholders = 32

// Template is a string with `{{ expression }}` placeholders. Rendering a
// template only ever produces a string (or, via Value, one typed value); the
// result is data. Callers must pass it as an argv element, JSON value or text,
// never to a shell. Use `{{ "{{" }}` to emit a literal opening brace pair.
type Template struct {
	src   string
	parts []tplPart
}

type tplPart struct {
	text string
	expr *Expr // nil for literal text
}

// ParseTemplate parses s.
func ParseTemplate(s string) (*Template, error) {
	t := &Template{src: s}
	rest := s
	n := 0
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			if rest != "" {
				t.parts = append(t.parts, tplPart{text: rest})
			}
			return t, nil
		}
		if i > 0 {
			t.parts = append(t.parts, tplPart{text: rest[:i]})
		}
		rest = rest[i+2:]
		j := indexClose(rest)
		if j < 0 {
			return nil, fmt.Errorf("unterminated '{{' in template")
		}
		if n++; n > MaxPlaceholders {
			return nil, fmt.Errorf("more than %d placeholders in one template", MaxPlaceholders)
		}
		e, err := Parse(strings.TrimSpace(rest[:j]))
		if err != nil {
			return nil, fmt.Errorf("placeholder %d: %w", n, err)
		}
		t.parts = append(t.parts, tplPart{expr: e})
		rest = rest[j+2:]
	}
}

// indexClose finds the "}}" that ends a placeholder, skipping quoted strings.
func indexClose(s string) int {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == '\\' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '\'' || c == '"':
			q = c
		case c == '}' && i+1 < len(s) && s[i+1] == '}':
			return i
		}
	}
	return -1
}

// IsStatic reports whether the template has no placeholders.
func (t *Template) IsStatic() bool {
	for _, p := range t.parts {
		if p.expr != nil {
			return false
		}
	}
	return true
}

// Render evaluates every placeholder and concatenates the stringified values.
func (t *Template) Render(env map[string]any) (string, error) {
	var b strings.Builder
	for _, p := range t.parts {
		if p.expr == nil {
			b.WriteString(p.text)
			continue
		}
		v, err := p.expr.Eval(env)
		if err != nil {
			return "", fmt.Errorf("{{ %s }}: %w", p.expr.src, err)
		}
		b.WriteString(Stringify(v))
	}
	return b.String(), nil
}

// Value is Render, except that a template consisting of exactly one placeholder
// returns the typed value (so JSON arguments can carry numbers and objects).
func (t *Template) Value(env map[string]any) (any, error) {
	if len(t.parts) == 1 && t.parts[0].expr != nil {
		return t.parts[0].expr.Eval(env)
	}
	return t.Render(env)
}
