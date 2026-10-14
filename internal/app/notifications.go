package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

var NotificationsChanged = mygo.NewEvent[daemon.NotificationEvent]("notifications:changed")
var FilesChanged = mygo.NewEvent[daemon.FilesChanged]("files:changed")

type FileRootsChange struct {
	Provider string `json:"provider"`
}

var FileRootsChanged = mygo.NewEvent[FileRootsChange]("files:rootsChanged")

func forwardFilesNotifications(event string, data json.RawMessage) bool {
	switch event {
	case "notifications.changed":
		var e daemon.NotificationEvent
		if json.Unmarshal(data, &e) == nil {
			NotificationsChanged.Broadcast(e)
		}
	case "files.changed":
		var e daemon.FilesChanged
		if json.Unmarshal(data, &e) == nil {
			FilesChanged.Broadcast(e)
		}
	case "files.roots.changed":
		var e FileRootsChange
		if json.Unmarshal(data, &e) == nil {
			FileRootsChanged.Broadcast(e)
		}
	default:
		return false
	}
	return true
}

// Notifications forwards notification center operations; notification creation is daemon-owned.
type Notifications struct{ L *Link }

// List returns the newest notifications, or only open approvals when needsYou is set.
func (s Notifications) List(ctx context.Context, needsYou bool) (daemon.NotificationList, error) {
	var out daemon.NotificationList
	err := s.L.call(ctx, "notifications.list", notifications.ListOptions{NeedsYou: needsYou, Limit: notifications.DefaultList}, &out)
	return out, err
}
func (s Notifications) MarkRead(ctx context.Context, ids []string, all bool) (int64, error) {
	var out int64
	err := s.L.call(ctx, "notifications.markRead", map[string]any{"ids": ids, "all": all}, &out)
	return out, err
}
func (s Notifications) Prefs(ctx context.Context) (notifications.Prefs, error) {
	var out notifications.Prefs
	err := s.L.call(ctx, "notifications.prefs.get", nil, &out)
	return out, err
}
func (s Notifications) SetPrefs(ctx context.Context, prefs notifications.Prefs) (notifications.Prefs, error) {
	var out notifications.Prefs
	err := s.L.call(ctx, "notifications.prefs.set", prefs, &out)
	return out, err
}

// Test creates a real notification through the daemon's saved preferences.
func (s Notifications) Test(ctx context.Context) (notifications.Result, error) {
	var out notifications.Result
	err := s.L.call(ctx, "notifications.test", nil, &out)
	return out, err
}
