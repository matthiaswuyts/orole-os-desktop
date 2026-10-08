package machines

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/google/uuid"
)

// CreateAgentInputDirectory creates an exclusive, private input directory under an
// approved remote workspace. It uses the same pinned, revocable, authorized SFTP
// access as WriteFile; it neither executes a remote shell nor overwrites a path.
// The caller must install a .gitignore before transferring any task input bytes.
func (m *Manager) CreateAgentInputDirectory(ctx context.Context, actor, machineID, workspace, sessionID string) (string, error) {
	id, err := uuid.Parse(sessionID)
	if err != nil || id.String() != sessionID || id.Version() != 4 || machineID == "local" || machineID == "" {
		return "", ErrInvalid
	}
	workspace, err = remotePath(workspace)
	if err != nil {
		return "", err
	}
	dir := path.Join(workspace, ".orole-attachments-"+sessionID)
	op, fs, close, err := m.fileSession(ctx, actor, FileRequest{MachineID: machineID, Path: dir}, true)
	if err != nil {
		return "", err
	}
	defer close()
	if fs == nil {
		return "", ErrInvalid
	}
	unlock, err := op.guard()
	if err != nil {
		return "", err
	}
	defer unlock()
	target, err := checkedRemote(fs, op.machine, dir, true)
	if err != nil {
		return "", err
	}
	if _, err := fs.Lstat(target); err == nil {
		return "", errors.New("attachment input directory already exists")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := fs.Mkdir(target); err != nil {
		return "", fmt.Errorf("create remote input directory: %w", err)
	}
	if err := fs.Chmod(target, 0o700); err != nil {
		return "", fmt.Errorf("protect remote input directory: %w", err)
	}
	// Return the requested logical path (including Windows drive syntax), not a
	// server-specific canonical spelling that may not be valid for the CLI.
	if len(dir) >= 4 && dir[0] == '/' && dir[2] == ':' && dir[3] == '/' {
		return dir[1:], nil
	}
	return dir, nil
}
