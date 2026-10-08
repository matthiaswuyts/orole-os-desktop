//go:build windows

package ipc

import (
	"path/filepath"
	"testing"
)

func TestEndpointSameDirectoryRelativeAndAbsolute(t *testing.T) {
	relative := ".orole-endpoint-test"
	absolute, err := filepath.Abs(relative)
	if err != nil {
		t.Fatal(err)
	}
	if Endpoint(relative) != Endpoint(absolute) {
		t.Fatal("relative and absolute names for one data directory must use the same pipe")
	}
	if Endpoint(absolute) == Endpoint(absolute+"-other") {
		t.Fatal("different data directories must retain separate pipes")
	}
}
