package app

import (
	"context"
	"encoding/json"
	"errors"

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

// NotificationNativeState reports submission, not whether a user saw or heard a toast.
type NotificationNativeState struct {
	ID       string `json:"id"`
	Accepted bool   `json:"accepted"`
	Denied   bool   `json:"denied"`
	Error    string `json:"error,omitempty"`
}

var NotificationsNativeState = mygo.NewEvent[NotificationNativeState]("notifications:nativeState")

func forwardFilesNotifications(event string, data json.RawMessage) bool {
	switch event {
	case "notifications.changed":
		var e daemon.NotificationEvent
		if json.Unmarshal(data, &e) == nil {
			NotificationsChanged.Broadcast(e)
			if e.Kind == "created" && e.Native && e.Notification != nil {
				n := *e.Notification
				go showNativeNotification(n, e.Sound)
			}
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
func showNativeNotification(n notifications.Notification, sound bool) {
	state := NotificationNativeState{ID: n.ID}
	if !mygo.NotificationsSupported() {
		state.Error = "Native notifications are unavailable on this system."
		NotificationsNativeState.Broadcast(state)
		return
	}
	err := mygo.NewNotification(mygo.NotificationOptions{ID: n.ID, Group: n.Source, Title: n.Title, Body: n.Body, Silent: !sound}).Show()
	switch {
	case errors.Is(err, mygo.ErrNotificationsDenied):
		state.Denied = true
		state.Error = "Windows has blocked notifications. Check system notification permissions."
	case err != nil:
		state.Error = "The system could not accept a native notification."
	default:
		state.Accepted = true
	}
	NotificationsNativeState.Broadcast(state)
}

// Notifications forwards persisted center operations; notification creation is daemon-owned.
type Notifications struct{ L *Link }

func (s Notifications) List(ctx context.Context, source string, unreadOnly bool) (daemon.NotificationList, error) {
	var out daemon.NotificationList
	err := s.L.call(ctx, "notifications.list", notifications.ListOptions{Source: source, UnreadOnly: unreadOnly, Limit: notifications.MaxList}, &out)
	return out, err
}
func (s Notifications) MarkRead(ctx context.Context, ids []string, source string, all bool) (int64, error) {
	var out int64
	err := s.L.call(ctx, "notifications.markRead", map[string]any{"ids": ids, "source": source, "all": all}, &out)
	return out, err
}
func (s Notifications) Dismiss(ctx context.Context, ids []string, source string, all bool) (int64, error) {
	var out int64
	err := s.L.call(ctx, "notifications.dismiss", map[string]any{"ids": ids, "source": source, "all": all}, &out)
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
