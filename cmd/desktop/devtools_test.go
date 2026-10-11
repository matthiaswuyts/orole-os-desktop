package main

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestDevToolsAreOffUnlessADeveloperAsks(t *testing.T) {
	t.Setenv("OROLE_DEVTOOLS", "")
	if got := devTools(); got != mygo.DevToolsDisabled {
		t.Fatalf("default: %v", got)
	}
	t.Setenv("OROLE_DEVTOOLS", "0")
	if got := devTools(); got != mygo.DevToolsDisabled {
		t.Fatalf("0: %v", got)
	}
	t.Setenv("OROLE_DEVTOOLS", "1")
	if got := devTools(); got != mygo.DevToolsEnabled {
		t.Fatalf("1: %v", got)
	}
}
