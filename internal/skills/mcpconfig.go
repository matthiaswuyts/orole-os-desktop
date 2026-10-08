package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// gatewayServerName is the one MCP server Orole installs into harness configurations.
const gatewayServerName = "orole"

// gatewayEntry is the stdio server definition: `<exe> mcp-gateway` with OROLE_HOME set.
type gatewayEntry struct {
	Command string
	Args    []string
	Env     map[string]string
}

func (y *Syncer) entry() gatewayEntry {
	e := gatewayEntry{Command: y.GatewayExe, Args: []string{"mcp-gateway"}}
	if y.DataDir != "" {
		e.Env = map[string]string{"OROLE_HOME": y.DataDir}
	}
	return e
}

func (e gatewayEntry) equal(o gatewayEntry) bool {
	if e.Command != o.Command || len(e.Args) != len(o.Args) || len(e.Env) != len(o.Env) {
		return false
	}
	for i := range e.Args {
		if e.Args[i] != o.Args[i] {
			return false
		}
	}
	for k, v := range e.Env {
		if ov, ok := o.Env[k]; !ok || ov != v {
			return false
		}
	}
	return true
}

func (y *Syncer) gatewayState(d harnessDef) (string, error) {
	if d.MCPTOML {
		return tomlGatewayState(d.MCPFile, y.entry())
	}
	return jsonGatewayState(d.MCPFile, y.entry())
}

func (y *Syncer) installGateway(d harnessDef) error {
	if d.MCPTOML {
		return installTOML(d.MCPFile, y.entry())
	}
	return installJSON(d.MCPFile, y.entry())
}

// ---- shared file plumbing ----

const backupSuffix = ".orole-backup"

// modifyFile applies f to the current content of p (nil when absent) and writes the result
// atomically. The first modification of an existing file keeps a copy at p+".orole-backup". If
// the file changed while we worked (harnesses rewrite their configs), the whole read-modify-write
// repeats. f returning the same bytes means nothing to do.
func modifyFile(p string, f func(cur []byte, exists bool) ([]byte, error)) error {
	for attempt := 0; attempt < 4; attempt++ {
		cur, err := os.ReadFile(p)
		exists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		next, err := f(cur, exists)
		if err != nil {
			return err
		}
		if exists && bytes.Equal(cur, next) {
			return nil
		}
		perm := os.FileMode(0o600)
		if exists {
			if fi, err := os.Stat(p); err == nil {
				perm = fi.Mode().Perm()
			}
			if _, err := os.Lstat(p + backupSuffix); errors.Is(err, fs.ErrNotExist) {
				if err := writeFileAtomic(p+backupSuffix, cur, 0o600); err != nil {
					return fmt.Errorf("backup %s: %w", p, err)
				}
			}
		} else if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if again, err := os.ReadFile(p); (err == nil) != exists || (exists && !bytes.Equal(again, cur)) {
			continue // changed underneath us
		}
		return writeFileAtomic(p, next, perm)
	}
	return fmt.Errorf("%s keeps changing; try again", p)
}

// ---- JSON mcpServers (Claude Code ~/.claude.json, Oh My Pi mcp.json) ----

type kv struct {
	key string
	val json.RawMessage
}

// parseObject splits a JSON object into its members, preserving order and the exact bytes of
// every value. Duplicate keys are kept.
func parseObject(raw []byte) ([]kv, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	var out []kv
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, kv{key, v})
	}
	if _, err := dec.Token(); err != nil { // closing brace
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after the JSON object")
	}
	return out, nil
}

func encodeObject(items []kv) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(it.key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(it.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func indexOf(items []kv, key string) (int, error) {
	idx := -1
	for i, it := range items {
		if it.key == key {
			if idx >= 0 {
				return 0, fmt.Errorf("duplicate %q key", key)
			}
			idx = i
		}
	}
	return idx, nil
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// jsonServers locates mcpServers and the orole member inside a parsed document.
type jsonDoc struct {
	top        []kv
	serversIdx int // -1 when absent
	servers    []kv
	entryIdx   int // -1 when absent
}

func parseJSONDoc(data []byte) (*jsonDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	if !json.Valid(data) {
		return nil, errors.New("not valid JSON")
	}
	top, err := parseObject(data)
	if err != nil {
		return nil, err
	}
	doc := &jsonDoc{top: top, serversIdx: -1, entryIdx: -1}
	if doc.serversIdx, err = indexOf(top, "mcpServers"); err != nil {
		return nil, err
	}
	if doc.serversIdx >= 0 && !isNull(top[doc.serversIdx].val) {
		if doc.servers, err = parseObject(top[doc.serversIdx].val); err != nil {
			return nil, errors.New(`"mcpServers" is not an object`)
		}
		if doc.entryIdx, err = indexOf(doc.servers, gatewayServerName); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

func (doc *jsonDoc) entry() (gatewayEntry, bool) {
	if doc.entryIdx < 0 {
		return gatewayEntry{}, false
	}
	var w struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if json.Unmarshal(doc.servers[doc.entryIdx].val, &w) != nil || (w.Type != "" && w.Type != "stdio") {
		return gatewayEntry{Command: "<unrecognized>"}, true
	}
	return gatewayEntry{Command: w.Command, Args: w.Args, Env: w.Env}, true
}

func jsonGatewayState(path string, want gatewayEntry) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return GatewayMissing, nil
	}
	if err != nil {
		return GatewayMissing, err
	}
	doc, err := parseJSONDoc(data)
	if err != nil {
		return GatewayMissing, fmt.Errorf("%s: %v (left untouched)", path, err)
	}
	got, ok := doc.entry()
	switch {
	case !ok:
		return GatewayMissing, nil
	case got.equal(want):
		return GatewayInstalled, nil
	}
	return GatewayOutdated, nil
}

func installJSON(path string, want gatewayEntry) error {
	err := modifyFile(path, func(cur []byte, exists bool) ([]byte, error) {
		doc, err := parseJSONDoc(cur)
		if err != nil {
			return nil, fmt.Errorf("%s: %v (refusing to modify it)", path, err)
		}
		if got, ok := doc.entry(); ok && got.equal(want) {
			return cur, nil
		}
		obj := []kv{{"type", json.RawMessage(`"stdio"`)}, {"command", mustJSON(want.Command)}, {"args", mustJSON(want.Args)}}
		if len(want.Env) > 0 {
			obj = append(obj, kv{"env", mustJSON(want.Env)})
		}
		entry := encodeObject(obj)
		if doc.entryIdx >= 0 {
			doc.servers[doc.entryIdx].val = entry
		} else {
			doc.servers = append(doc.servers, kv{gatewayServerName, entry})
		}
		servers := encodeObject(doc.servers)
		if doc.serversIdx >= 0 {
			doc.top[doc.serversIdx].val = servers
		} else {
			doc.top = append(doc.top, kv{"mcpServers", servers})
		}
		out := encodeObject(doc.top)
		if !exists || bytes.IndexByte(cur, '\n') >= 0 {
			var ind bytes.Buffer
			if err := json.Indent(&ind, out, "", "  "); err != nil {
				return nil, err
			}
			out = ind.Bytes()
			if !exists || bytes.HasSuffix(cur, []byte("\n")) {
				out = append(out, '\n')
			}
		}
		return out, nil
	})
	return err
}

func mustJSON(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSpace(b.Bytes())
}

// ---- Codex config.toml (managed block) ----

const (
	tomlBegin = "# >>> orole managed block (written by Orole OS; edits inside are overwritten) >>>"
	tomlEnd   = "# <<< orole managed block <<<"
)

func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func renderTOMLBlock(e gatewayEntry, eol string) string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = tomlQuote(a)
	}
	lines := []string{tomlBegin, "[mcp_servers." + gatewayServerName + "]",
		"command = " + tomlQuote(e.Command), "args = [" + strings.Join(args, ", ") + "]"}
	if len(e.Env) > 0 {
		keys := make([]string, 0, len(e.Env))
		for k := range e.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = tomlQuote(k) + " = " + tomlQuote(e.Env[k])
		}
		lines = append(lines, "env = { "+strings.Join(parts, ", ")+" }")
	}
	lines = append(lines, tomlEnd)
	return strings.Join(lines, eol)
}

func detectEOL(b []byte) string {
	if bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// findBlock locates the managed block: byte offsets of the begin line start and end line end
// (excluding its line break). found is false when neither marker exists; a lone marker is an error.
func findBlock(s string) (start, end int, found bool, err error) {
	bi := strings.Index(s, tomlBegin)
	ei := strings.Index(s, tomlEnd)
	switch {
	case bi < 0 && ei < 0:
		return 0, 0, false, nil
	case bi < 0 || ei < 0 || ei < bi:
		return 0, 0, false, errors.New("has an incomplete Orole managed block (one marker is missing)")
	}
	if strings.Contains(s[bi+len(tomlBegin):], tomlBegin) || strings.Count(s, tomlEnd) != 1 {
		return 0, 0, false, errors.New("has more than one Orole managed block")
	}
	return bi, ei + len(tomlEnd), true, nil
}

var (
	tomlHeaderRE   = regexp.MustCompile(`^\s*\[\s*([^\[\]]+?)\s*\]\s*(#.*)?$`)
	tomlOroleTable = regexp.MustCompile(`^mcp_servers\s*\.\s*(?:orole|"orole"|'orole')\s*(?:\..*)?$`)
	tomlOroleKey   = regexp.MustCompile(`^\s*(?:orole|"orole"|'orole')\s*[.=]`)
	tomlRootDotted = regexp.MustCompile(`^\s*mcp_servers\s*\.\s*(?:orole|"orole"|'orole')\s*[.=]`)
)

// unmanagedOrole reports whether the TOML text outside the managed block already defines an
// mcp_servers.orole entry.
func unmanagedOrole(s string) bool {
	table := ""
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := tomlHeaderRE.FindStringSubmatch(line); m != nil && !strings.HasPrefix(strings.TrimSpace(line), "[[") {
			table = m[1]
			if tomlOroleTable.MatchString(table) {
				return true
			}
			continue
		}
		switch {
		case table == "" && tomlRootDotted.MatchString(line):
			return true
		case strings.ReplaceAll(table, " ", "") == "mcp_servers" && tomlOroleKey.MatchString(line):
			return true
		}
	}
	return false
}

func tomlGatewayState(path string, want gatewayEntry) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return GatewayMissing, nil
	}
	if err != nil {
		return GatewayMissing, err
	}
	s := string(data)
	start, end, found, err := findBlock(s)
	if err != nil {
		return GatewayMissing, fmt.Errorf("%s %v (left untouched)", path, err)
	}
	if !found {
		if unmanagedOrole(s) {
			return GatewayOutdated, fmt.Errorf("%s already defines [mcp_servers.orole] outside Orole's managed block (left untouched)", path)
		}
		return GatewayMissing, nil
	}
	if unmanagedOrole(s[:start] + s[end:]) {
		return GatewayOutdated, fmt.Errorf("%s defines [mcp_servers.orole] twice (left untouched)", path)
	}
	if s[start:end] == renderTOMLBlock(want, detectEOL(data)) {
		return GatewayInstalled, nil
	}
	return GatewayOutdated, nil
}

func installTOML(path string, want gatewayEntry) error {
	return modifyFile(path, func(cur []byte, exists bool) ([]byte, error) {
		s := string(cur)
		eol := detectEOL(cur)
		block := renderTOMLBlock(want, eol)
		start, end, found, err := findBlock(s)
		if err != nil {
			return nil, fmt.Errorf("%s %v (refusing to modify it)", path, err)
		}
		if found {
			if unmanagedOrole(s[:start] + s[end:]) {
				return nil, fmt.Errorf("%s defines [mcp_servers.orole] twice (refusing to modify it)", path)
			}
			return []byte(s[:start] + block + s[end:]), nil
		}
		if unmanagedOrole(s) {
			return nil, fmt.Errorf("%s already defines [mcp_servers.orole]; remove it or rename it so Orole can manage its own entry (refusing to modify it)", path)
		}
		var b strings.Builder
		b.WriteString(s)
		if s != "" && !strings.HasSuffix(s, "\n") {
			b.WriteString(eol)
		}
		if s != "" {
			b.WriteString(eol)
		}
		b.WriteString(block)
		b.WriteString(eol)
		return []byte(b.String()), nil
	})
}
