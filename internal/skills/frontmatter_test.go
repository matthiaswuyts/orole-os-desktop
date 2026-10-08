package skills

import "testing"

func TestParseFrontmatter(t *testing.T) {
	cases := []struct {
		name, in, wantName, wantDesc, wantBody string
		wantErr                                bool
	}{
		{name: "plain", in: "---\nname: my-skill\ndescription: Does a thing\n---\n# Body\n", wantName: "my-skill", wantDesc: "Does a thing", wantBody: "# Body\n"},
		{name: "crlf and bom", in: "\ufeff---\r\nname: a\r\ndescription: b c\r\n---\r\nbody", wantName: "a", wantDesc: "b c", wantBody: "body"},
		{name: "double quoted with colon", in: "---\nname: \"x\"\ndescription: \"Use when: asked \\\"why\\\"\"\n---\n", wantName: "x", wantDesc: `Use when: asked "why"`},
		{name: "single quoted", in: "---\nname: 'x'\ndescription: 'it''s fine'\n---\n", wantName: "x", wantDesc: "it's fine"},
		{name: "folded block", in: "---\nname: x\ndescription: >-\n  first line\n  second line\n\n  new para\nlicense: MIT\n---\n", wantName: "x", wantDesc: "first line second line new para"},
		{name: "literal block collapses", in: "---\nname: x\ndescription: |\n  one\n  two\n---\n", wantName: "x", wantDesc: "one two"},
		{name: "plain continuation", in: "---\nname: x\ndescription: starts here\n  and continues\n---\n", wantName: "x", wantDesc: "starts here and continues"},
		{name: "trailing comment", in: "---\nname: x # the name\ndescription: d\n---\n", wantName: "x", wantDesc: "d"},
		{name: "nested map skipped", in: "---\nname: x\nmetadata:\n  author: me\n  name: not-this\ndescription: d\n---\n", wantName: "x", wantDesc: "d"},
		{name: "case-insensitive keys", in: "---\nName: x\nDescription: d\n---\n", wantName: "x", wantDesc: "d"},
		{name: "missing frontmatter", in: "# just markdown\n", wantErr: true},
		{name: "unterminated", in: "---\nname: x\n", wantErr: true},
		{name: "empty fields ok", in: "---\nfoo: bar\n---\nbody", wantBody: "body"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, body, err := ParseFrontmatter(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if m.Name != c.wantName || m.Description != c.wantDesc {
				t.Fatalf("meta = %+v, want name %q desc %q", m, c.wantName, c.wantDesc)
			}
			if c.wantBody != "" && body != c.wantBody {
				t.Fatalf("body = %q, want %q", body, c.wantBody)
			}
		})
	}
}
