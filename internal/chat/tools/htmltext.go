package tools

import (
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// skippedElements never contribute readable text (and neither do their descendants).
var skippedElements = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true,
	"canvas": true, "iframe": true, "object": true, "embed": true,
	"nav": true, "footer": true, "aside": true, "select": true, "button": true,
}

// rawTextElements have content that is not markup; it runs to the matching close tag.
var rawTextElements = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}

var paragraphElements = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true, "header": true,
	"ul": true, "ol": true, "table": true, "blockquote": true, "pre": true, "figure": true,
	"dl": true, "details": true, "address": true, "form": true,
}

var lineElements = map[string]bool{
	"br": true, "hr": true, "tr": true, "dt": true, "dd": true, "figcaption": true, "summary": true,
}

// textBuilder accumulates whitespace-normalised text.
type textBuilder struct {
	buf       []byte
	lastSpace bool // the buffer ends in whitespace (or is empty)
}

func newTextBuilder(capacity int) *textBuilder {
	return &textBuilder{buf: make([]byte, 0, capacity), lastSpace: true}
}

func (t *textBuilder) text(s string) {
	for _, r := range s {
		if unicode.IsSpace(r) || r == '\u00a0' {
			if !t.lastSpace {
				t.buf = append(t.buf, ' ')
				t.lastSpace = true
			}
			continue
		}
		t.buf = utf8.AppendRune(t.buf, r)
		t.lastSpace = false
	}
}

// breaks ensures the buffer ends with n newlines (at most 2); nothing is added to an empty buffer.
func (t *textBuilder) breaks(n int) {
	for len(t.buf) > 0 && t.buf[len(t.buf)-1] == ' ' {
		t.buf = t.buf[:len(t.buf)-1]
	}
	if len(t.buf) == 0 {
		return
	}
	have := 0
	for i := len(t.buf) - 1; i >= 0 && t.buf[i] == '\n' && have < n; i-- {
		have++
	}
	for ; have < n; have++ {
		t.buf = append(t.buf, '\n')
	}
	t.lastSpace = true
}

// extractHTML turns an HTML document into readable text using only the standard library. Scripts,
// styles, navigation chrome and other non-content elements are dropped; headings, list items and
// link targets survive as light Markdown. base resolves relative links and may be nil.
func extractHTML(src string, base *url.URL) (title, body string) {
	tb := newTextBuilder(len(src) / 4)
	var (
		skip      string // element being skipped
		skipDepth int
		linkHref  string
		linkStart = -1
	)
	i := 0
	for i < len(src) {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			if skip == "" {
				tb.text(html.UnescapeString(src[i:]))
			}
			break
		}
		if lt > 0 {
			if skip == "" {
				tb.text(html.UnescapeString(src[i : i+lt]))
			}
			i += lt
		}
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				i = len(src)
			} else {
				i += 4 + end + 3
			}
			continue
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				i = len(src)
			} else {
				i += end + 1
			}
			continue
		}
		closing := len(rest) > 2 && rest[1] == '/'
		nameStart := 1
		if closing {
			nameStart = 2
		}
		nameEnd := nameStart
		for nameEnd < len(rest) && isTagNameByte(rest[nameEnd]) {
			nameEnd++
		}
		if nameEnd == nameStart {
			// A stray "<" is text.
			if skip == "" {
				tb.text("<")
			}
			i++
			continue
		}
		name := strings.ToLower(rest[nameStart:nameEnd])
		tagEnd := tagClose(rest, nameEnd)
		tag := rest[nameEnd:tagEnd] // attributes and optional "/"
		i += tagEnd + 1
		if tagEnd >= len(rest) {
			i = len(src)
		}

		if closing {
			if skip != "" {
				if name == skip {
					if skipDepth--; skipDepth == 0 {
						skip = ""
					}
				}
				continue
			}
			switch {
			case name == "a" && linkStart >= 0:
				tb.closeLink(linkStart, linkHref)
				linkStart, linkHref = -1, ""
			case len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6':
				tb.breaks(2)
			case name == "li":
				tb.breaks(1)
			case paragraphElements[name]:
				tb.breaks(2)
			case lineElements[name]:
				tb.breaks(1)
			}
			continue
		}

		if skip != "" {
			if name == skip && !strings.HasSuffix(tag, "/") {
				skipDepth++
			}
			continue
		}
		if rawTextElements[name] {
			end := indexCloseTag(src, i, name)
			content := src[i:end]
			if name == "title" && title == "" {
				title = strings.Join(strings.Fields(html.UnescapeString(content)), " ")
			}
			// Skip past the close tag.
			if end < len(src) {
				if gt := strings.IndexByte(src[end:], '>'); gt >= 0 {
					end += gt + 1
				} else {
					end = len(src)
				}
			}
			i = end
			continue
		}
		if skippedElements[name] {
			if !strings.HasSuffix(tag, "/") {
				skip, skipDepth = name, 1
			}
			continue
		}
		switch {
		case name == "a":
			if h := attr(tag, "href"); h != "" {
				linkHref = resolveLink(h, base)
				if linkHref != "" {
					linkStart = len(tb.buf)
				}
			}
		case len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6':
			tb.breaks(2)
			tb.buf = append(tb.buf, strings.Repeat("#", int(name[1]-'0'))...)
			tb.buf = append(tb.buf, ' ')
			tb.lastSpace = true
		case name == "li":
			tb.breaks(1)
			tb.buf = append(tb.buf, "- "...)
			tb.lastSpace = true
		case paragraphElements[name]:
			tb.breaks(2)
		case lineElements[name]:
			tb.breaks(1)
		case name == "td" || name == "th":
			tb.text(" ")
		}
	}
	tb.breaks(0)
	return title, strings.TrimSpace(string(tb.buf))
}

// closeLink rewrites the text emitted since start as a Markdown link.
func (t *textBuilder) closeLink(start int, href string) {
	if start > len(t.buf) {
		return
	}
	label := strings.TrimSpace(string(t.buf[start:]))
	t.buf = t.buf[:start]
	if label == "" {
		return
	}
	t.buf = append(t.buf, '[')
	t.buf = append(t.buf, label...)
	t.buf = append(t.buf, "]("...)
	t.buf = append(t.buf, href...)
	t.buf = append(t.buf, ')')
	t.lastSpace = false
}

func resolveLink(h string, base *url.URL) string {
	h = strings.TrimSpace(h)
	if h == "" || h[0] == '#' || len(h) > 400 {
		return ""
	}
	u, err := url.Parse(html.UnescapeString(h))
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func isTagNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == ':'
}

// tagClose returns the index of the ">" ending the tag that starts at s[0], honouring quoted
// attribute values. When the tag never closes it returns len(s).
func tagClose(s string, from int) int {
	var quote byte
	for i := from; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i
		}
	}
	return len(s)
}

// indexCloseTag finds "</name" (case-insensitive) at or after from, or len(s).
func indexCloseTag(s string, from int, name string) int {
	pat := "</" + name
	for j := from; j < len(s); {
		k := strings.IndexByte(s[j:], '<')
		if k < 0 {
			return len(s)
		}
		j += k
		if j+len(pat) <= len(s) && strings.EqualFold(s[j:j+len(pat)], pat) {
			return j
		}
		j++
	}
	return len(s)
}

// attr returns the value of the named attribute in an attribute string, or "".
func attr(tag, name string) string {
	i := 0
	for i < len(tag) {
		for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\n' || tag[i] == '\r' || tag[i] == '/') {
			i++
		}
		start := i
		for i < len(tag) && tag[i] != '=' && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\n' && tag[i] != '\r' && tag[i] != '/' {
			i++
		}
		key := tag[start:i]
		for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\n' || tag[i] == '\r') {
			i++
		}
		val := ""
		if i < len(tag) && tag[i] == '=' {
			i++
			for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\n' || tag[i] == '\r') {
				i++
			}
			if i < len(tag) && (tag[i] == '"' || tag[i] == '\'') {
				q := tag[i]
				i++
				end := strings.IndexByte(tag[i:], q)
				if end < 0 {
					val, i = tag[i:], len(tag)
				} else {
					val, i = tag[i:i+end], i+end+1
				}
			} else {
				vs := i
				for i < len(tag) && tag[i] != ' ' && tag[i] != '\t' && tag[i] != '\n' && tag[i] != '\r' {
					i++
				}
				val = tag[vs:i]
			}
		}
		if strings.EqualFold(key, name) {
			return val
		}
	}
	return ""
}
