package mail

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlainTextMail(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`<p>Hello <b>world</b></p>`, "\nHello world\n"},
		{`<img src="https://tracking.test/pixel" onerror="x()">safe`, "safe"},
		{`<script>secret</script><style>bad</style><svg><text>hide</text></svg>visible`, "visible"},
		{`<!-- comment --><a title=">" href="javascript:x()">link</a>`, "link"},
		{`&lt;script&gt;plain&lt;/script&gt; &amp;`, "<script>plain</script> &"},
		{"a\u202eb\x00c", "abc"},
		{`<script>unclosed`, ""},
		{`<!-- unclosed`, ""},
	} {
		if got := plain(tc.input, 1000); got != tc.want {
			t.Fatalf("plain(%q)=%q; want %q", tc.input, got, tc.want)
		}
	}
	if got := plain(strings.Repeat("x", 2000), 100); len(got) > 104 {
		t.Fatal("plain text unbounded")
	}
}

func TestParseMessagesRejectsInvalidShapes(t *testing.T) {
	for _, raw := range []string{`{}`, `{"id":17}`, `{"id":"x\u202e"}`, `{"id":"x","threadId":"bad\n"}`, `{"id":"x","to":{}}`, `{"id":"x","unread":"yes"}`} {
		if _, err := parseMessages([]json.RawMessage{json.RawMessage(raw)}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	rows, err := parseMessages([]json.RawMessage{json.RawMessage(`{"id":"x","subject":"<b>Hello</b>","from":"a\u202eb","to":["c\u202ed"],"body":"<script>x()</script>safe"}`)})
	if err != nil || len(rows) != 1 || rows[0].Subject != "Hello" || rows[0].From != "ab" || rows[0].To[0] != "cd" || rows[0].Body != "safe" {
		t.Fatalf("parsed: %+v %v", rows, err)
	}
}
