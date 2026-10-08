package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestUpdateProcessChild(t *testing.T) {
	if os.Getenv("OROLE_UPDATE_PROCESS_CHILD") != "1" {
		return
	}
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
	os.Exit(0)
}

func TestUpdateWaitRequiresProcessExit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestUpdateProcessChild$")
	cmd.Env = append(os.Environ(), "OROLE_UPDATE_PROCESS_CHILD=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	process, err := trackUpdateProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := process.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("live daemon must block replacement, got %v", err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	exited, cancelExited := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelExited()
	if err := process.Wait(exited); err != nil {
		t.Fatalf("exited daemon still blocks replacement: %v", err)
	}
}
