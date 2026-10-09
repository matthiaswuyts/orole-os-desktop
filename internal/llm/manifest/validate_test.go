package manifest

import (
	"errors"
	"strings"
	"testing"
)

func fixture() ProviderManifest {
	return ProviderManifest{
		SchemaVersion: 1, ID: "ollama", Name: "Ollama", Description: "Local models",
		BaseURL: "http://localhost:11434/v1", Auth: ManifestAuth{Type: AuthNone},
	}
}

func TestValidateDefaultsAndCanonicalDigest(t *testing.T) {
	norm, canonical, digest, err := Validate(fixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if norm.ModelsPath != "/models" || norm.ChatPath != "/chat/completions" {
		t.Fatalf("defaults not applied: %+v", norm)
	}
	if !ValidDigest(digest) || len(canonical) == 0 {
		t.Fatalf("digest %q", digest)
	}
	// Equivalent spellings normalise to the same digest.
	alt := fixture()
	alt.BaseURL = "HTTP://LocalHost:11434/v1/"
	alt.ModelsPath, alt.ChatPath = "/models", "/chat/completions"
	_, _, d2, err := Validate(alt, nil)
	if err != nil || d2 != digest {
		t.Fatalf("normalised digest differs: %v %s vs %s", err, d2, digest)
	}
	changed := fixture()
	changed.Name = "Ollama 2"
	if _, _, d3, _ := Validate(changed, nil); d3 == digest {
		t.Fatal("changed manifest kept the digest")
	}
}

func TestValidateMatrix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*ProviderManifest)
		ok   bool
	}{
		{"loopback http", func(m *ProviderManifest) {}, true},
		{"loopback 127.0.0.1", func(m *ProviderManifest) { m.BaseURL = "http://127.0.0.1:1234/v1" }, true},
		{"loopback ::1", func(m *ProviderManifest) { m.BaseURL = "http://[::1]:1234" }, true},
		{"remote https", func(m *ProviderManifest) {
			m.BaseURL = "https://api.example.com/v1"
			m.Auth = ManifestAuth{Type: AuthBearer}
		}, true},
		{"header auth", func(m *ProviderManifest) {
			m.BaseURL = "https://api.example.com"
			m.Auth = ManifestAuth{Type: AuthHeader, Header: "X-Api-Key"}
		}, true},
		{"remote http", func(m *ProviderManifest) { m.BaseURL = "http://api.example.com/v1" }, false},
		{"lan http", func(m *ProviderManifest) { m.BaseURL = "http://192.168.1.5:8080" }, false},
		{"ftp", func(m *ProviderManifest) { m.BaseURL = "ftp://localhost" }, false},
		{"credentials in url", func(m *ProviderManifest) { m.BaseURL = "https://user:pw@api.example.com" }, false},
		{"query in url", func(m *ProviderManifest) { m.BaseURL = "https://api.example.com/v1?key=1" }, false},
		{"fragment in url", func(m *ProviderManifest) { m.BaseURL = "https://api.example.com/v1#x" }, false},
		{"dot segment", func(m *ProviderManifest) { m.BaseURL = "https://api.example.com/../x" }, false},
		{"escaped path", func(m *ProviderManifest) { m.BaseURL = "https://api.example.com/%2e%2e" }, false},
		{"empty host", func(m *ProviderManifest) { m.BaseURL = "https:///v1" }, false},
		{"bad port", func(m *ProviderManifest) { m.BaseURL = "http://localhost:99999" }, false},
		{"bad schema version", func(m *ProviderManifest) { m.SchemaVersion = 2 }, false},
		{"builtin id", func(m *ProviderManifest) { m.ID = "openai" }, false},
		{"builtin id openrouter", func(m *ProviderManifest) { m.ID = "openrouter" }, false},
		{"uppercase id", func(m *ProviderManifest) { m.ID = "Ollama" }, false},
		{"slash id", func(m *ProviderManifest) { m.ID = "a/b" }, false},
		{"id double hyphen", func(m *ProviderManifest) { m.ID = "a--b" }, false},
		{"empty name", func(m *ProviderManifest) { m.Name = " " }, false},
		{"control in name", func(m *ProviderManifest) { m.Name = "a\nb" }, false},
		{"unknown auth", func(m *ProviderManifest) { m.Auth.Type = "basic" }, false},
		{"bearer with header", func(m *ProviderManifest) { m.Auth = ManifestAuth{Type: AuthBearer, Header: "X-A"} }, false},
		{"header auth without name", func(m *ProviderManifest) { m.Auth = ManifestAuth{Type: AuthHeader} }, false},
		{"bad auth header chars", func(m *ProviderManifest) { m.Auth = ManifestAuth{Type: AuthHeader, Header: "X Key"} }, false},
		{"auth header host", func(m *ProviderManifest) { m.Auth = ManifestAuth{Type: AuthHeader, Header: "Host"} }, false},
		{"auth header content-length", func(m *ProviderManifest) { m.Auth = ManifestAuth{Type: AuthHeader, Header: "content-length"} }, false},
		{"auth header proxy", func(m *ProviderManifest) { m.Auth = ManifestAuth{Type: AuthHeader, Header: "Proxy-Authorization"} }, false},
		{"models path without slash", func(m *ProviderManifest) { m.ModelsPath = "models" }, false},
		{"chat path query", func(m *ProviderManifest) { m.ChatPath = "/chat?x=1" }, false},
		{"chat path absolute url", func(m *ProviderManifest) { m.ChatPath = "//evil.example/x" }, false},
		{"chat path traversal", func(m *ProviderManifest) { m.ChatPath = "/../x" }, false},
		{"static header ok", func(m *ProviderManifest) { m.Headers = map[string]string{"OpenAI-Organization": "org-1"} }, true},
		{"static Authorization", func(m *ProviderManifest) { m.Headers = map[string]string{"Authorization": "Bearer x"} }, false},
		{"static Cookie", func(m *ProviderManifest) { m.Headers = map[string]string{"Cookie": "a=b"} }, false},
		{"static x-api-key", func(m *ProviderManifest) { m.Headers = map[string]string{"X-Api-Key": "k"} }, false},
		{"static Host", func(m *ProviderManifest) { m.Headers = map[string]string{"Host": "evil"} }, false},
		{"static Content-Length", func(m *ProviderManifest) { m.Headers = map[string]string{"Content-Length": "1"} }, false},
		{"static header newline value", func(m *ProviderManifest) { m.Headers = map[string]string{"X-A": "a\r\nB: c"} }, false},
		{"static header duplicate case", func(m *ProviderManifest) { m.Headers = map[string]string{"X-A": "1", "x-a": "2"} }, false},
		{"static header equals auth header", func(m *ProviderManifest) {
			m.Auth = ManifestAuth{Type: AuthHeader, Header: "X-Gateway"}
			m.Headers = map[string]string{"x-gateway": "1"}
		}, false},
		{"too many headers", func(m *ProviderManifest) {
			m.Headers = map[string]string{}
			for i := range 17 {
				m.Headers["X-H"+strings.Repeat("a", i+1)] = "1"
			}
		}, false},
		{"models ok", func(m *ProviderManifest) {
			m.Models = []ManifestModel{{ID: "llama3:8b", Vision: true, Tools: true, ContextWindow: 8192}}
		}, true},
		{"duplicate model", func(m *ProviderManifest) { m.Models = []ManifestModel{{ID: "a"}, {ID: "a"}} }, false},
		{"empty model id", func(m *ProviderManifest) { m.Models = []ManifestModel{{ID: ""}} }, false},
		{"negative context", func(m *ProviderManifest) { m.Models = []ManifestModel{{ID: "a", ContextWindow: -1}} }, false},
		{"too many models", func(m *ProviderManifest) {
			for i := range 65 {
				m.Models = append(m.Models, ManifestModel{ID: "m" + strings.Repeat("x", i)})
			}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := fixture()
			tc.edit(&m)
			_, _, _, err := Validate(m, nil)
			if tc.ok && err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted or wrong error: %v", err)
			}
		})
	}
}

func TestReservedIDsFromCaller(t *testing.T) {
	m := fixture()
	_, _, _, err := Validate(m, func(id string) bool { return id == "ollama" })
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("reserved id accepted: %v", err)
	}
}

func TestParseStrictness(t *testing.T) {
	const valid = `{"schemaVersion":1,"id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer"}}`
	if _, err := Parse([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"duplicate key":    `{"schemaVersion":1,"id":"x","id":"y","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer"}}`,
		"nested dup key":   `{"schemaVersion":1,"id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer","type":"none"}}`,
		"duplicate header": `{"schemaVersion":1,"id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer"},"headers":{"X-A":"1","X-A":"2"}}`,
		"unknown field":    `{"schemaVersion":1,"id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer"},"apiKey":"sk-secret"}`,
		"unknown nested":   `{"schemaVersion":1,"id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer","key":"k"}}`,
		"unknown in model": `{"schemaVersion":1,"id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer"},"models":[{"id":"a","price":1}]}`,
		"wrong case field": `{"schemaVersion":1,"id":"x","name":"X","BaseURL":"https://a.example","auth":{"type":"bearer"}}`,
		"wrong type":       `{"schemaVersion":"1","id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer"}}`,
		"trailing data":    valid + `{}`,
		"not an object":    `[]`,
		"empty":            ``,
		"deep nesting":     `{"schemaVersion":1,"id":"x","name":"X","baseUrl":"https://a.example","auth":{"type":"bearer"},"headers":` + strings.Repeat(`{"a":`, 20) + `1` + strings.Repeat(`}`, 20) + `}`,
		"oversize":         `{"schemaVersion":1,"id":"x","name":"` + strings.Repeat("a", MaxBytes) + `","baseUrl":"https://a.example","auth":{"type":"bearer"}}`,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted or wrong error: %v", err)
			}
		})
	}
}

func TestTrustDetailNamesHostAndFitsPrompt(t *testing.T) {
	m := fixture()
	m.BaseURL = "https://gateway.example.com/v1"
	m.Auth = ManifestAuth{Type: AuthHeader, Header: "X-Gateway-Key"}
	m.Headers = map[string]string{"OpenAI-Organization": "org"}
	m.Models = []ManifestModel{{ID: "a", Vision: true}}
	norm, _, digest, err := Validate(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := TrustDetail(norm, digest)
	for _, want := range []string{"gateway.example.com", "X-Gateway-Key", digest, "remote", "OpenAI-Organization", "1 with image"} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
	if len(d) > 1900 {
		t.Fatalf("detail too long: %d", len(d))
	}
}
