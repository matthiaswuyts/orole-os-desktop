package skills

import (
	"errors"
	"strings"
)

// Meta is the part of a SKILL.md frontmatter Orole reads.
type Meta struct {
	Name        string
	Description string
}

// ParseFrontmatter splits a SKILL.md into its YAML frontmatter fields and the Markdown body.
// It understands the subset skills use: top-level scalar keys (plain, 'single' and "double"
// quoted, folded > and literal | blocks, multi-line continuations). Nested structures such as
// a metadata map are skipped. It never executes or evaluates anything.
func ParseFrontmatter(text string) (Meta, string, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return Meta{}, "", errors.New("SKILL.md must start with a --- frontmatter block holding name and description")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], " \t")
		if t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return Meta{}, "", errors.New("SKILL.md frontmatter is not closed with ---")
	}
	fm := lines[1:end]
	body := strings.Join(lines[end+1:], "\n")

	fields := map[string]string{}
	for i := 0; i < len(fm); {
		key, rest, ok := splitKey(fm[i])
		if !ok {
			i++
			continue
		}
		j := i + 1
		for j < len(fm) && (fm[j] == "" || fm[j][0] == ' ' || fm[j][0] == '\t') {
			j++
		}
		cont := fm[i+1 : j]
		fields[key] = scalar(rest, cont)
		i = j
	}
	m := Meta{Name: strings.TrimSpace(fields["name"]), Description: collapse(fields["description"])}
	return m, body, nil
}

// splitKey parses a top-level "key: value" line.
func splitKey(line string) (key, rest string, ok bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
		return "", "", false
	}
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", "", false
	}
	key = line[:i]
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return "", "", false
		}
	}
	rest = line[i+1:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", "", false
	}
	return strings.ToLower(key), strings.TrimSpace(rest), true
}

// scalar evaluates the value of a key given the text after the colon and its continuation lines.
func scalar(rest string, cont []string) string {
	if rest != "" && (rest[0] == '|' || rest[0] == '>') {
		// Chomping indicators only change trailing newlines, which callers trim.
		return block(rest[0] == '>', cont)
	}
	// Plain or quoted scalars continue on indented lines, folded with single spaces.
	parts := []string{rest}
	for _, c := range cont {
		if t := strings.TrimSpace(c); t != "" {
			parts = append(parts, t)
		}
	}
	joined := strings.TrimSpace(strings.Join(parts, " "))
	switch {
	case strings.HasPrefix(joined, `"`):
		return unquoteDouble(joined[1:])
	case strings.HasPrefix(joined, "'"):
		return unquoteSingle(joined[1:])
	}
	// A plain scalar ends at " #" (comment). Nested mappings produce an empty rest and are
	// ignored by callers because only name/description are read.
	if i := strings.Index(joined, " #"); i >= 0 {
		joined = joined[:i]
	}
	return strings.TrimSpace(joined)
}

func block(folded bool, cont []string) string {
	indent := -1
	for _, c := range cont {
		if strings.TrimSpace(c) == "" {
			continue
		}
		n := len(c) - len(strings.TrimLeft(c, " \t"))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent < 0 {
		return ""
	}
	lines := make([]string, 0, len(cont))
	for _, c := range cont {
		if len(c) >= indent {
			lines = append(lines, c[indent:])
		} else {
			lines = append(lines, "")
		}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if !folded {
		return strings.Join(lines, "\n")
	}
	var b strings.Builder
	for i, l := range lines {
		switch {
		case strings.TrimSpace(l) == "":
			b.WriteByte('\n')
		case i > 0 && strings.TrimSpace(lines[i-1]) != "":
			b.WriteByte(' ')
			b.WriteString(l)
		default:
			b.WriteString(l)
		}
	}
	return b.String()
}

func unquoteDouble(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return b.String()
		case '\\':
			if i+1 >= len(s) {
				return b.String()
			}
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"', '\\', '/':
				b.WriteByte(s[i])
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func unquoteSingle(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			return b.String()
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// collapse folds runs of whitespace (including newlines) into single spaces.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }
