package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

func TestSandboxUserActionsRefuseBotsAndAutomations(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		ok   bool
	}{
		{"desktop user", context.Background(), true},
		{"bot", agents.WithActor(context.Background(), "bot:b1"), false},
		{"automation", events.WithChain(context.Background(), []string{"auto-1"}), false},
	}
	for _, c := range cases {
		err := requireDesktopUser(c.ctx, "turn on Windows Hypervisor Platform")
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
		var ie *ipc.Error
		if err != nil && (!errors.As(err, &ie) || ie.Code != ipc.CodeDenied) {
			t.Errorf("%s: want a denied ipc error, got %v", c.name, err)
		}
	}
}
