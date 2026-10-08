package machines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNativeCommandHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--orole-machine-test-helper" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	args := os.Args[index+1:]
	if len(args) == 2 && args[0] == "write-marker" {
		if err := os.WriteFile(args[1], []byte("executed"), 0600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if len(args) > 0 && args[0] == "sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	data, err := json.Marshal(args)
	if err != nil {
		os.Exit(2)
	}
	fmt.Print(string(data))
	os.Exit(0)
}
func TestNativeCommandRunsWithoutShellAndHonorsTimeout(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local", AllowCommands: true}); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	values := []string{"space here", "a\"b", "a'b; $(not-a-command)", "", "héllo", `C:\trailing\`}
	args := append([]string{"-test.run=^TestNativeCommandHelper$", "--", "--orole-machine-test-helper"}, values...)
	result, err := m.Execute(ctx, "user", Command{ID: "local", OperationID: uuid.NewString(), Program: exe, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(values)
	if strings.TrimSpace(result.Stdout) != string(expected) || result.ExitCode != 0 {
		t.Fatalf("literal native args: %+v", result)
	}
	_, err = m.Execute(ctx, "user", Command{ID: "local", OperationID: uuid.NewString(), Program: exe, Args: []string{"-test.run=^TestNativeCommandHelper$", "--", "--orole-machine-test-helper", "sleep"}, TimeoutSeconds: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}
