package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

// UpdateStatus describes the framework's checked release, never a guessed GitHub tag.
type UpdateStatus struct {
	Enabled        bool   `json:"enabled"`
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	Version        string `json:"version"`
	Checking       bool   `json:"checking"`
	Installing     bool   `json:"installing"`
	Error          string `json:"error"`
}

// Updates owns only this desktop's transient native update transaction. Release
// verification, download and replacement are exclusively MyGo's implementation.
// It does not generate keys or silently enable unsigned builds.
type Updates struct {
	L      *Link
	mu     sync.Mutex
	state  UpdateStatus
	update *mygo.Update
}

func (u *Updates) Status() UpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.CurrentVersion = mygo.App.Version()
	u.state.Enabled = mygo.Updater.Enabled()
	if !u.state.Enabled {
		u.state.Available = false
		u.state.Version = ""
		u.update = nil
	}
	return u.state
}

func (u *Updates) Check(ctx context.Context) UpdateStatus {
	u.Status()
	u.mu.Lock()
	if !u.state.Enabled || u.state.Checking || u.state.Installing {
		state := u.state
		u.mu.Unlock()
		return state
	}
	u.state.Checking = true
	u.state.Error = ""
	u.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	up, err := mygo.Updater.Check(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state.Checking = false
	u.update = up
	u.state.Available = err == nil && up != nil
	u.state.Version = ""
	if err != nil {
		u.state.Error = "Could not check the signed update feed. Check your connection and try again."
	} else if up != nil {
		u.state.Version = up.Version
	}
	return u.state
}

// Install requires both explicit caller intent and a Go-owned native confirmation.
// The confirmation binds to the cached framework release; no caller-supplied URL
// or version can select executable content. Stopping the daemon ends its sessions.
func (u *Updates) Install(ctx context.Context, confirmed bool) (UpdateStatus, error) {
	if !confirmed {
		return u.Status(), errors.New("update installation needs explicit confirmation")
	}
	u.mu.Lock()
	if u.state.Installing || u.state.Checking || u.update == nil || !u.state.Available || !mygo.Updater.Enabled() {
		state := u.state
		u.mu.Unlock()
		return state, errors.New("no checked update is available, or an update operation is already running")
	}
	up := u.update
	u.state.Installing = true
	u.state.Error = ""
	u.mu.Unlock()
	finish := func(err error) (UpdateStatus, error) {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.state.Installing = false
		if err != nil {
			u.state.Error = err.Error()
		}
		return u.state, err
	}
	choice, err := mygo.Dialog.Message(mygo.MessageOptions{
		Title:   "Update Orole OS",
		Message: "Install Orole OS " + up.Version + " and restart?",
		Detail:  "Save your work first. Updating stops the local daemon: running agents, terminals and automations will end. Remote work may continue on its host. The downloaded release must pass MyGo signature verification before it is installed.",
		Buttons: []string{"Cancel", "Install and restart"},
	})
	if err != nil {
		return finish(fmt.Errorf("could not confirm update: %w", err))
	}
	if choice.Button != 1 {
		return finish(nil)
	}
	if err := ctx.Err(); err != nil {
		return finish(err)
	}
	if u.L == nil {
		return finish(errors.New("desktop daemon link is unavailable"))
	}
	if err := u.L.beginMaintenance(); err != nil {
		return finish(err)
	}
	installed := false
	defer func() {
		if !installed {
			u.L.endMaintenance()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	// Reach the authenticated daemon directly; Link calls are now inhibited.
	health, err := control.Running(ctx, u.L.DataDir)
	if err != nil {
		return finish(errors.New("Cannot verify daemon state. Reconnect the daemon before updating."))
	}
	process, err := trackUpdateProcess(health.PID)
	if err != nil {
		return finish(fmt.Errorf("could not track daemon shutdown: %w", err))
	}
	defer process.Close()
	if err := control.Stop(ctx, u.L.DataDir); err != nil {
		return finish(fmt.Errorf("could not stop the daemon for update: %w", err))
	}
	exitCtx, cancelExit := context.WithTimeout(ctx, 30*time.Second)
	err = process.Wait(exitCtx)
	cancelExit()
	if err != nil {
		return finish(fmt.Errorf("daemon has not exited; update was not installed: %w", err))
	}
	if err := up.Install(ctx, nil); err != nil {
		return finish(errors.New("Signed update installation failed. No restart was requested; check the release signature, connectivity and installation-directory permissions before trying again."))
	}
	installed = true
	u.mu.Lock()
	u.state.Available = false
	u.update = nil
	state := u.state
	u.mu.Unlock()
	mygo.App.Relaunch()
	return state, nil
}
