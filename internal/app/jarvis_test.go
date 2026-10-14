package app

import (
	"encoding/json"
	"testing"
)

func TestForwardJarvis(t *testing.T) {
	if !forwardJarvis("jarvis.changed", json.RawMessage(`{"configured":true}`)) {
		t.Fatal("jarvis.changed must be routed")
	}
	if !forwardJarvis("jarvis.changed", json.RawMessage(`not json`)) {
		t.Fatal("malformed jarvis.changed is still consumed")
	}
	for _, ev := range []string{"notifications.changed", "jarvis.other", ""} {
		if forwardJarvis(ev, json.RawMessage(`{}`)) {
			t.Fatalf("%q must not be routed", ev)
		}
	}
}
