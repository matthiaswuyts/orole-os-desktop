package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunUsageAndErrors(t *testing.T) {
	t.Setenv("OROLE_HOME", t.TempDir())
	cases := []struct {
		args []string
		code int
		out  string
	}{
		{nil, 0, "usage:"},
		{[]string{"version"}, 0, "orole "},
		{[]string{"bogus"}, 2, ""},
		{[]string{"daemon"}, 2, ""},
		{[]string{"daemon", "status"}, 0, "not running"},
		{[]string{"daemon", "stop"}, 0, "not running"},
		{[]string{"terminal", "list"}, 1, ""}, // needs a daemon
		{[]string{"settings", "set", "k"}, 2, ""},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if code := run(context.Background(), c.args, &out, &errb); code != c.code {
			t.Errorf("%v: code %d want %d (%s %s)", c.args, code, c.code, out.String(), errb.String())
		}
		if !strings.Contains(out.String(), c.out) {
			t.Errorf("%v: output %q lacks %q", c.args, out.String(), c.out)
		}
	}
}
