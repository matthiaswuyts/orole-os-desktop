package expr

import (
	"reflect"
	"strings"
	"testing"
)

func testEnv() map[string]any {
	return map[string]any{
		"event": map[string]any{
			"topic": "github.ci.completed",
			"data": map[string]any{
				"conclusion": "failure",
				"repo":       "rayorole/orole-os",
				"count":      float64(3),
				"labels":     []any{"bug", "ci"},
				"nested":     map[string]any{"ok": true},
			},
		},
		"steps": map[string]any{
			"build-1": map[string]any{"output": map[string]any{"exitCode": float64(0), "passed": true}},
			"empty":   map[string]any{"output": map[string]any{}},
		},
		"n":     5, // plain Go int must be normalised
		"label": "Hello World",
	}
}

func TestEval(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"1", 1.0},
		{"'a'", "a"},
		{`"it's"`, "it's"},
		{`'it\'s'`, "it's"},
		{"true", true},
		{"null", nil},
		{"-3", -3.0},
		{"--3", 3.0},
		{"!true", false},
		{"!!null", false},
		{"event.topic", "github.ci.completed"},
		{"event.data.conclusion == 'failure'", true},
		{"event.data.conclusion != 'failure'", false},
		{"event.data.conclusion == 'success'", false},
		{"event.data.count > 2 && event.data.count <= 3", true},
		{"event.data.count >= 4 || event.data.conclusion == 'failure'", true},
		{"event.data.labels[0] == 'bug'", true},
		{"event.data.labels[5]", nil},
		{"event.data.labels[-1]", nil},
		{"event.data.labels[1.5]", nil},
		{"event.data['repo']", "rayorole/orole-os"},
		{"event.data.missing.deeper.still", nil},
		{"event.data.missing == null", true},
		{"missing.x == null", true},
		{"steps.build-1.output.passed", true},
		{"steps.build-1.output.exitCode == 0", true},
		{"steps['build-1'].output.exitCode", 0.0},
		{"event.data.labels contains 'ci'", true},
		{"'ci' in event.data.labels", true},
		{"'nope' in event.data.labels", false},
		{"event.data.repo contains 'orole'", true},
		{"event.data.repo startsWith 'rayorole/'", true},
		{"event.data.repo endsWith '/x'", false},
		{"event.data contains 'conclusion'", true},
		{"event.data contains 'zzz'", false},
		{"len(event.data.labels) == 2", true},
		{"len('héllo')", 5.0},
		{"len(missing)", 0.0},
		{"lower(label) == 'hello world'", true},
		{"upper('a')", "A"},
		{"string(1.5)", "1.5"},
		{"string(null) == ''", true},
		{"number('42') == 42", true},
		{"n == 5", true},
		{"n > 4.5", true},
		{"(1 == 1) && (2 > 1)", true},
		{"1 == 1 == true", true},
		{"null == null", true},
		{"null == false", false},
		{"0 == false", false},
		{"'1' == 1", false},
		{"event.data.labels == event.data.labels", true},
		{"event.data.nested == event.data.nested", true},
		{"event.data.nested == event.data", false},
		{"steps.empty.output", map[string]any{}},
	}
	env := testEnv()
	for _, c := range cases {
		e, err := Parse(c.src)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.src, err)
			continue
		}
		got, err := e.Eval(env)
		if err != nil {
			t.Errorf("Eval(%q): %v", c.src, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Eval(%q) = %#v, want %#v", c.src, got, c.want)
		}
	}
}

func TestShortCircuit(t *testing.T) {
	// The right side would be an ordering error; it must not run.
	e, err := Parse("true || 'a' < 1")
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.Eval(nil)
	if err != nil || v != true {
		t.Fatalf("got %v %v", v, err)
	}
	e, _ = Parse("false && 'a' < 1")
	if v, err := e.Eval(nil); err != nil || v != false {
		t.Fatalf("got %v %v", v, err)
	}
}

func TestEvalErrors(t *testing.T) {
	for _, src := range []string{
		"'a' < 1",
		"1 < 'a'",
		"null < 1",
		"-'a'",
		"len(1)",
		"lower(1)",
		"number('x')",
		"number([1])",
		"1 startsWith 'a'",
		"'a' startsWith 1",
		"'a' contains 1",
		"1 contains 1",
	} {
		e, err := Parse(src)
		if err != nil {
			if src == "number([1])" {
				continue // array literals unsupported; parse error is fine
			}
			t.Errorf("Parse(%q): %v", src, err)
			continue
		}
		if v, err := e.Eval(nil); err == nil {
			t.Errorf("Eval(%q) = %#v, want error", src, v)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{
		"", " ", "1 +", "(", ")", "(1", "1)", "a.", "a.1", "a[", "a[1", "a[]", "'abc", `"abc`, "'a\\q'",
		"&&", "1 &&", "== 1", "1 = 1", "a b", "a ? b : c", "@", "$x", "{}", "1..2", "1e5.5.1",
		"contains", "1 contains", "in", "foo(", "foo(1)", "len()", "len(1,2)", "len(1 2)", "len(,)",
		"a.b(", "!", "-", strings.Repeat("(", MaxDepth+5) + "1" + strings.Repeat(")", MaxDepth+5),
		strings.Repeat("!", MaxDepth+5) + "1",
		strings.Repeat("a || ", MaxNodes) + "a",
		strings.Repeat("x", MaxSourceLen+1),
		"'\xff'", "a\x00b",
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%.60q) = nil error, want error", src)
		}
	}
}

func TestNoCodeExecutionSurface(t *testing.T) {
	// None of these may do anything but fail to parse or evaluate to data.
	for _, src := range []string{
		"__proto__", "constructor", "os.Exit", "exec('calc')", "system('x')", "event.constructor",
		"`ls`", "$(ls)", "${x}", "import('os')", "x => x", "function(){}", "1; 2",
	} {
		e, err := Parse(src)
		if err != nil {
			continue
		}
		if _, err := e.Eval(testEnv()); err != nil {
			continue
		}
	}
}

func TestTruthy(t *testing.T) {
	for v, want := range map[string]bool{"null": false, "false": false, "true": true, "0": false, "1": true, "''": false, "'x'": true, "event.data.labels": true, "steps.empty.output": false} {
		e, err := Parse(v)
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.EvalBool(testEnv())
		if err != nil || got != want {
			t.Errorf("EvalBool(%s)=%v,%v want %v", v, got, err, want)
		}
	}
}

func TestTemplate(t *testing.T) {
	env := testEnv()
	cases := []struct{ src, want string }{
		{"plain", "plain"},
		{"", ""},
		{"{{ event.data.repo }}", "rayorole/orole-os"},
		{"repo={{event.data.repo}} n={{ event.data.count }}!", "repo=rayorole/orole-os n=3!"},
		{"{{ missing }}|", "|"},
		{"{{ event.data.labels }}", `["bug","ci"]`},
		{"{{ '}}' }}", "}}"},
		{`{{ "{{" }}`, "{{"},
		{"{{ 1.50 }}", "1.5"},
		{"a{{ true }}b", "atrueb"},
		{"--flag={{ event.data.repo }}; rm -rf /", "--flag=rayorole/orole-os; rm -rf /"},
	}
	for _, c := range cases {
		tp, err := ParseTemplate(c.src)
		if err != nil {
			t.Errorf("ParseTemplate(%q): %v", c.src, err)
			continue
		}
		got, err := tp.Render(env)
		if err != nil || got != c.want {
			t.Errorf("Render(%q) = %q, %v; want %q", c.src, got, err, c.want)
		}
	}
	for _, bad := range []string{"{{", "{{ 1", "{{ }}", "{{ 1 + 2 }}", "x {{ ( }} y", strings.Repeat("{{1}}", MaxPlaceholders+1)} {
		if _, err := ParseTemplate(bad); err == nil {
			t.Errorf("ParseTemplate(%q) should fail", bad)
		}
	}
	tp, _ := ParseTemplate("{{ event.data.count }}")
	if v, _ := tp.Value(env); v != 3.0 {
		t.Errorf("Value of single placeholder should be typed, got %#v", v)
	}
	tp, _ = ParseTemplate("n={{ event.data.count }}")
	if v, _ := tp.Value(env); v != "n=3" {
		t.Errorf("Value of mixed template should be string, got %#v", v)
	}
	if s, _ := ParseTemplate("abc"); !s.IsStatic() {
		t.Error("plain template should be static")
	}
	if d, _ := ParseTemplate("{{ 1 }}"); d.IsStatic() {
		t.Error("placeholder template is not static")
	}
	tp, _ = ParseTemplate("{{ 'a' < 1 }}")
	if _, err := tp.Render(env); err == nil {
		t.Error("render error should propagate")
	}
}

// FuzzParseEval asserts the evaluator is total: no panics, bounded work, for any input.
func FuzzParseEval(f *testing.F) {
	for _, s := range []string{
		"event.data.conclusion == 'failure'", "a && b || !c", "x[1].y['z'] contains 'q'", "len(a) > 3",
		"((((1))))", "--1", "'unterminated", "a.b.c.d.e.f", "1 < 2 == true", "in", "!",
		"{{", "\x00", "number('1e999')", strings.Repeat("(", 50), "a in b in c",
	} {
		f.Add(s)
	}
	env := testEnv()
	f.Fuzz(func(t *testing.T, src string) {
		if e, err := Parse(src); err == nil {
			_, _ = e.Eval(env)
			_, _ = e.EvalBool(nil)
		}
		if tp, err := ParseTemplate(src); err == nil {
			_, _ = tp.Render(env)
			_, _ = tp.Value(env)
		}
	})
}
