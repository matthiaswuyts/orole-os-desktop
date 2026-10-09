package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// A header name is an RFC 9110 token restricted to letters, digits and hyphens.
	headerPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
	hostPattern    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
)

// reservedHeaders are never settable by a manifest (as the auth header or as a static header):
// the transport owns framing, the daemon owns content negotiation and ambient credentials.
var reservedHeaders = []string{
	"host", "content-length", "content-type", "content-encoding", "accept", "accept-encoding",
	"transfer-encoding", "connection", "keep-alive", "upgrade", "te", "trailer", "expect",
	"cookie", "set-cookie", "origin", "referer", "user-agent", "date", "via", "forwarded",
	"x-forwarded-for", "x-forwarded-host", "x-forwarded-proto", "x-real-ip",
}

func invalid(detail string) error { return fmt.Errorf("%w: %s", ErrInvalid, detail) }

// LoopbackHost reports whether host (no port, no brackets) is a loopback name the manifest
// may reach over plain HTTP.
func LoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// Loopback reports whether the manifest's base URL points at this machine.
func (m ProviderManifest) Loopback() bool {
	u, err := url.Parse(m.BaseURL)
	return err == nil && LoopbackHost(u.Hostname())
}

// Host returns the host[:port] the manifest is pinned to.
func (m ProviderManifest) Host() string {
	u, err := url.Parse(m.BaseURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// Origin returns scheme://host[:port], the unit a reviewed manifest is pinned to.
func (m ProviderManifest) Origin() string {
	u, err := url.Parse(m.BaseURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ChatURL and ModelsURL are the absolute endpoints (valid for a validated manifest).
func (m ProviderManifest) ChatURL() string   { return m.BaseURL + m.ChatPath }
func (m ProviderManifest) ModelsURL() string { return m.BaseURL + m.ModelsPath }

// UnmarshalJSON decodes strictly: duplicate keys, unknown or differently-cased fields,
// excessive nesting and oversize documents are rejected instead of silently dropped.
func (m *ProviderManifest) UnmarshalJSON(raw []byte) error {
	if len(raw) > MaxBytes {
		return invalid("manifest exceeds 32 KiB")
	}
	tree, err := decodeTree(raw)
	if err != nil {
		return err
	}
	if err := checkFields(tree); err != nil {
		return err
	}
	type wire ProviderManifest
	var out wire
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return invalid("manifest contains unknown fields or invalid field types")
	}
	*m = ProviderManifest(out)
	return nil
}

// Parse strictly decodes one manifest document.
func Parse(raw []byte) (ProviderManifest, error) {
	var m ProviderManifest
	if err := m.UnmarshalJSON(raw); err != nil {
		return ProviderManifest{}, err
	}
	return m, nil
}

// decodeTree bounds nesting and rejects repeated keys at every object depth.
func decodeTree(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > MaxBytes || !utf8.Valid(raw) {
		return nil, invalid("JSON must be valid UTF-8 and at most 32 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var value func(int) (any, error)
	value = func(depth int) (any, error) {
		if depth > maxDepth {
			return nil, invalid("JSON nesting is too deep")
		}
		token, err := dec.Token()
		if err != nil {
			return nil, invalid("malformed JSON")
		}
		switch token {
		case json.Delim('{'):
			out := make(map[string]any)
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return nil, invalid("malformed JSON object")
				}
				name, ok := key.(string)
				if !ok {
					return nil, invalid("invalid JSON object key")
				}
				if _, exists := out[name]; exists {
					return nil, invalid("duplicate JSON object key")
				}
				item, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				out[name] = item
			}
			if end, err := dec.Token(); err != nil || end != json.Delim('}') {
				return nil, invalid("malformed JSON object")
			}
			return out, nil
		case json.Delim('['):
			out := []any{}
			for dec.More() {
				item, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
			if end, err := dec.Token(); err != nil || end != json.Delim(']') {
				return nil, invalid("malformed JSON array")
			}
			return out, nil
		case json.Delim('}'), json.Delim(']'):
			return nil, invalid("unexpected JSON delimiter")
		default:
			return token, nil
		}
	}
	out, err := value(0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid("JSON must contain exactly one value")
	}
	return out, nil
}

var (
	topFields   = []string{"schemaVersion", "id", "name", "description", "baseUrl", "auth", "modelsPath", "chatPath", "models", "headers"}
	authFields  = []string{"type", "header"}
	modelFields = []string{"id", "name", "vision", "pdf", "tools", "reasoning", "contextWindow"}
)

// checkFields requires exact (case-sensitive) field names: encoding/json would otherwise
// accept "BaseURL" for "baseUrl", which reviewers would not recognise.
func checkFields(tree any) error {
	top, ok := tree.(map[string]any)
	if !ok {
		return invalid("manifest must be a JSON object")
	}
	if err := exactKeys(top, topFields); err != nil {
		return err
	}
	if auth, ok := top["auth"].(map[string]any); ok {
		if err := exactKeys(auth, authFields); err != nil {
			return err
		}
	}
	if models, ok := top["models"].([]any); ok {
		for _, item := range models {
			if model, ok := item.(map[string]any); ok {
				if err := exactKeys(model, modelFields); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func exactKeys(obj map[string]any, allowed []string) error {
	for k := range obj {
		if !slices.Contains(allowed, k) {
			return invalid("manifest contains unknown fields")
		}
	}
	return nil
}

func validText(s string, limit int, required bool) bool {
	return len(s) <= limit && utf8.ValidString(s) && (!required || strings.TrimSpace(s) != "") && strings.TrimSpace(s) == s &&
		!strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) &&
		untrusted.Line(s, 0) == s
}

// ValidID reports whether id is a manifest slug: lowercase, digits and single hyphens.
// It deliberately has no slash ("provider/model" selections split on the first one).
func ValidID(id string) bool { return len(id) <= 40 && idPattern.MatchString(id) }

// ValidDigest reports whether s is a lowercase hex SHA-256 digest.
func ValidDigest(s string) bool { return digestPattern.MatchString(s) }

func validRef(id, digest string) bool { return ValidID(id) && ValidDigest(digest) }

// Validate normalizes m (defaults, canonical base URL) and returns it with its canonical
// JSON and SHA-256 digest. reserved reports ids that must not be claimed (built-in
// providers); the llm package's own ids are always reserved. reserved may be nil.
func Validate(in ProviderManifest, reserved func(id string) bool) (ProviderManifest, []byte, string, error) {
	out := in
	if out.SchemaVersion != SchemaVersion {
		return in, nil, "", invalid("schemaVersion must be 1")
	}
	if !ValidID(out.ID) {
		return in, nil, "", invalid("id must be a lowercase slug (letters, digits and single hyphens, at most 40 characters)")
	}
	if out.ID == llm.ProviderOpenRouter || out.ID == llm.ProviderOpenAI || (reserved != nil && reserved(out.ID)) {
		return in, nil, "", invalid("id collides with a built-in provider")
	}
	if !validText(out.Name, 100, true) {
		return in, nil, "", invalid("name is required (at most 100 plain-text characters)")
	}
	if !validText(out.Description, 512, false) {
		return in, nil, "", invalid("description must be at most 512 plain-text characters")
	}
	base, err := normalizeBaseURL(out.BaseURL)
	if err != nil {
		return in, nil, "", err
	}
	out.BaseURL = base
	if out.ModelsPath == "" {
		out.ModelsPath = DefaultModelsPath
	}
	if out.ChatPath == "" {
		out.ChatPath = DefaultChatPath
	}
	if !validEndpointPath(out.ModelsPath) || !validEndpointPath(out.ChatPath) {
		return in, nil, "", invalid("modelsPath and chatPath must be plain absolute paths such as /chat/completions")
	}
	if err := validateAuth(out.Auth); err != nil {
		return in, nil, "", err
	}
	if out.Models, err = validateModels(in.Models); err != nil {
		return in, nil, "", err
	}
	if out.Headers, err = validateHeaders(in.Headers, out.Auth); err != nil {
		return in, nil, "", err
	}
	if len(TrustDetail(out, strings.Repeat("0", 64))) > 1900 {
		return in, nil, "", invalid("manifest is too long to display completely in the approval prompt")
	}
	canonical, err := json.Marshal(out)
	if err != nil || len(canonical) > MaxBytes {
		return in, nil, "", invalid("manifest exceeds 32 KiB or cannot be encoded")
	}
	sum := sha256.Sum256(canonical)
	return out, canonical, hex.EncodeToString(sum[:]), nil
}

// normalizeBaseURL accepts https URLs and http only for loopback hosts, and returns the
// canonical scheme://host[:port][/path] form without a trailing slash.
func normalizeBaseURL(raw string) (string, error) {
	const bad = "baseUrl must be an https URL (http is only allowed for localhost, 127.0.0.1 and ::1) without credentials, query or fragment"
	if len(raw) == 0 || len(raw) > 512 || !validText(raw, 512, true) {
		return "", invalid(bad)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" ||
		strings.ContainsAny(raw, "%\\ ") || (u.Scheme != "https" && u.Scheme != "http") {
		return "", invalid(bad)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(host, ".") || strings.Contains(host, "%") {
		return "", invalid(bad)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" || ip.Unmap() != ip {
			return "", invalid(bad)
		}
	} else if !hostPattern.MatchString(host) || len(host) > 253 {
		return "", invalid(bad)
	}
	if u.Scheme == "http" && !LoopbackHost(host) {
		return "", invalid(bad)
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", invalid(bad)
		}
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path != "" && !validPath(path) {
		return "", invalid(bad)
	}
	hostport := host
	if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	if port := u.Port(); port != "" {
		hostport += ":" + port
	}
	return u.Scheme + "://" + hostport + path, nil
}

// validPath requires "/seg/seg" with plain segments (no dot segments, empty segments or escapes).
func validPath(p string) bool {
	if len(p) > 256 || !strings.HasPrefix(p, "/") {
		return false
	}
	for seg := range strings.SplitSeq(p[1:], "/") {
		if seg == "." || seg == ".." || !segmentPattern.MatchString(seg) {
			return false
		}
	}
	return true
}

func validEndpointPath(p string) bool { return validPath(p) }

func validateAuth(a ManifestAuth) error {
	switch a.Type {
	case AuthBearer, AuthNone:
		if a.Header != "" {
			return invalid("auth.header is only valid with auth.type \"header\"")
		}
	case AuthHeader:
		if !headerPattern.MatchString(a.Header) || reservedHeader(a.Header) {
			return invalid("auth.header must be a plain header name that is not reserved (Host, Content-Length, Cookie, ...)")
		}
	default:
		return invalid("auth.type must be bearer, header or none")
	}
	return nil
}

func reservedHeader(name string) bool {
	low := strings.ToLower(name)
	return slices.Contains(reservedHeaders, low) || strings.HasPrefix(low, "proxy-") || strings.HasPrefix(low, "sec-")
}

// credentialHeader flags names that look like they carry secrets; secrets never belong in a manifest.
func credentialHeader(name string) bool {
	low := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(name), "-", ""), "_", "")
	for _, s := range []string{"auth", "cookie", "token", "secret", "apikey", "password", "credential", "session", "signature"} {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

func validateHeaders(in map[string]string, auth ManifestAuth) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxHeader {
		return nil, invalid("at most 16 static headers are allowed")
	}
	out := make(map[string]string, len(in))
	seen := make(map[string]bool, len(in))
	for name, value := range in {
		low := strings.ToLower(name)
		if !headerPattern.MatchString(name) || reservedHeader(name) || credentialHeader(name) || seen[low] ||
			(auth.Type == AuthHeader && strings.EqualFold(name, auth.Header)) {
			return nil, invalid("static headers must have unique plain names that are neither reserved nor credentials (Authorization, Cookie, ...)")
		}
		if len(value) > 256 || !printableASCII(value) {
			return nil, invalid("static header values must be at most 256 printable ASCII characters")
		}
		seen[low] = true
		out[name] = value
	}
	return out, nil
}

func printableASCII(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return strings.TrimSpace(s) == s
}

func validateModels(in []ManifestModel) ([]ManifestModel, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxModels {
		return nil, invalid("at most 64 models may be declared")
	}
	out := make([]ManifestModel, len(in))
	seen := make(map[string]bool, len(in))
	for i, mod := range in {
		if !validText(mod.ID, 128, true) || strings.HasPrefix(mod.ID, "/") || seen[mod.ID] {
			return nil, invalid("models need unique plain-text ids (at most 128 characters)")
		}
		if mod.Name == "" {
			mod.Name = mod.ID
		}
		if !validText(mod.Name, 128, true) {
			return nil, invalid("model names must be at most 128 plain-text characters")
		}
		if mod.ContextWindow < 0 || mod.ContextWindow > 1<<30 {
			return nil, invalid("model contextWindow must be between 0 and 1073741824")
		}
		seen[mod.ID] = true
		out[i] = mod
	}
	return out, nil
}

// TrustDetail is the approval prompt text: it states the exact digest, the host the key is
// pinned to and everything the manifest can make the daemon send.
func TrustDetail(m ProviderManifest, digest string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Enable AI provider %s (%s).\nSHA-256: %s\n", m.ID, m.Name, digest)
	fmt.Fprintf(&b, "Endpoint: %s (chat path %s, models path %s)\n", m.BaseURL, m.ChatPath, m.ModelsPath)
	if m.Loopback() {
		b.WriteString("This host is on this computer (loopback).\n")
	} else {
		b.WriteString("This host is remote: conversation content leaves this computer.\n")
	}
	switch m.Auth.Type {
	case AuthBearer:
		fmt.Fprintf(&b, "Auth: your API key is sent as a Bearer token to %s only.\n", m.Host())
	case AuthHeader:
		fmt.Fprintf(&b, "Auth: your API key is sent in the %s header to %s only.\n", m.Auth.Header, m.Host())
	default:
		b.WriteString("Auth: none; no API key is stored or sent.\n")
	}
	b.WriteString("Capabilities: chat completions with streamed text and function tools; model listing")
	if len(m.Models) > 0 {
		var vision, pdf int
		for _, mod := range m.Models {
			if mod.Vision {
				vision++
			}
			if mod.PDF {
				pdf++
			}
		}
		fmt.Fprintf(&b, "; %d declared model(s), %d with image input, %d with PDF input", len(m.Models), vision, pdf)
	}
	b.WriteString(".\n")
	if len(m.Headers) > 0 {
		names := make([]string, 0, len(m.Headers))
		for name := range m.Headers {
			names = append(names, name)
		}
		slices.Sort(names)
		fmt.Fprintf(&b, "Static headers: %s.\n", strings.Join(names, ", "))
	}
	b.WriteString("This trusts only this manifest. No code is loaded; hosted tools are never used.")
	return b.String()
}
