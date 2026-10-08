package machines

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestAgentInputDirectoryRejectsInvalidIdentityAndPaths(t *testing.T) {
	m, _ := testManager(t)
	for _, tc := range []struct {
		machine   string
		workspace string
		session   string
	}{
		{machine: "local", workspace: "/approved", session: uuid.NewString()},
		{machine: "", workspace: "/approved", session: uuid.NewString()},
		{machine: "remote", workspace: "/approved", session: "../escape"},
		{machine: "remote", workspace: "relative/path", session: uuid.NewString()},
		{machine: "remote", workspace: "/approved\ncommand", session: uuid.NewString()},
		{machine: "remote", workspace: "//server/share", session: uuid.NewString()},
	} {
		if _, err := m.CreateAgentInputDirectory(context.Background(), "user", tc.machine, tc.workspace, tc.session); err == nil {
			t.Errorf("accepted invalid input directory request: %+v", tc)
		}
	}
	if _, err := m.CreateAgentInputDirectory(context.Background(), "user", "unknown-machine", "/approved", uuid.NewString()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown machine bypassed access checks: %v", err)
	}
}
