package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

// NotificationEvent is published on "notifications.changed".
type NotificationEvent struct {
	// Kind: created | updated.
	Kind         string                      `json:"kind"`
	Notification *notifications.Notification `json:"notification,omitempty"`
	Unread       int                         `json:"unread"`
	// Native asks the desktop to show an OS notification (per-source preference).
	Native bool `json:"native,omitempty"`
	// Sound permits audio only for newly created notifications.
	Sound bool `json:"sound"`
}

// NotificationList is notifications.list's answer.
type NotificationList struct {
	Items   []notifications.Notification `json:"items"`
	Unread  int                          `json:"unread"`
	Sources []notifications.SourceCount  `json:"sources"`
}

// Notify records a notification from daemon code: it applies the source's
// preferences, persists, and publishes notifications.changed. Other modules
// call this; text derived from external content is stored and shown as plain
// text only. It returns the stored notification (zero ID when the source is
// muted).
func (d *Daemon) Notify(source, title, body string, level notifications.Level) (notifications.Notification, error) {
	return d.notify(context.Background(), source, title, body, level)
}

func (d *Daemon) notify(ctx context.Context, source, title, body string, level notifications.Level) (notifications.Notification, error) {
	res, err := d.notifyResult(ctx, source, title, body, level)
	if err != nil || !res.Stored {
		return notifications.Notification{}, err
	}
	return res.Notification, nil
}

func (d *Daemon) notifyResult(ctx context.Context, source, title, body string, level notifications.Level) (notifications.Result, error) {
	st := notifications.New(d.DB.DB)
	res, err := st.Notify(ctx, source, title, body, level)
	if err != nil || !res.Stored {
		return res, err
	}
	unread, _ := st.Unread(ctx)
	n := res.Notification
	d.IPC.Publish("notifications.changed", NotificationEvent{Kind: "created", Notification: &n, Unread: unread, Native: res.Native, Sound: res.Sound})
	return res, nil
}

func mapNotifErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, notifications.ErrInvalid):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, notifications.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	}
	return err
}

type notifSelection struct {
	IDs    []string `json:"ids"`
	Source string   `json:"source"`
	All    bool     `json:"all"`
}

func (d *Daemon) registerNotifications() {
	st := notifications.New(d.DB.DB)
	updated := func(ctx context.Context) {
		unread, _ := st.Unread(ctx)
		d.IPC.Publish("notifications.changed", NotificationEvent{Kind: "updated", Unread: unread})
	}

	handle(d, "notifications.list", func(ctx context.Context, p notifications.ListOptions) (NotificationList, error) {
		items, err := st.List(ctx, p)
		if err != nil {
			return NotificationList{}, err
		}
		unread, err := st.Unread(ctx)
		if err != nil {
			return NotificationList{}, err
		}
		srcs, err := st.Sources(ctx)
		return NotificationList{Items: items, Unread: unread, Sources: srcs}, err
	})
	handle(d, "notifications.notify", func(ctx context.Context, p struct {
		Source string              `json:"source"`
		Title  string              `json:"title"`
		Body   string              `json:"body"`
		Level  notifications.Level `json:"level"`
	}) (notifications.Notification, error) {
		n, err := d.notify(ctx, p.Source, p.Title, p.Body, p.Level)
		return n, mapNotifErr(err)
	})
	handle(d, "notifications.test", func(ctx context.Context, _ none) (notifications.Result, error) {
		res, err := d.notifyResult(ctx, "system.test", "Orole test notification", "This test uses your saved notification and sound preferences.", notifications.Info)
		return res, mapNotifErr(err)
	})
	handle(d, "notifications.markRead", func(ctx context.Context, p notifSelection) (int64, error) {
		n, err := st.MarkRead(ctx, p.IDs, p.Source, p.All)
		if err == nil && n > 0 {
			updated(ctx)
		}
		return n, mapNotifErr(err)
	})
	handle(d, "notifications.dismiss", func(ctx context.Context, p notifSelection) (int64, error) {
		n, err := st.Dismiss(ctx, p.IDs, p.Source, p.All)
		if err == nil && n > 0 {
			updated(ctx)
		}
		return n, mapNotifErr(err)
	})
	handle(d, "notifications.prefs.get", func(ctx context.Context, _ none) (notifications.Prefs, error) {
		return st.Prefs(ctx)
	})
	handle(d, "notifications.prefs.set", func(ctx context.Context, p notifications.Prefs) (notifications.Prefs, error) {
		if err := st.SetPrefs(ctx, p); err != nil {
			return p, mapNotifErr(err)
		}
		d.IPC.Publish("settings.changed", map[string]any{"key": notifications.PrefsKey, "value": p})
		return p, nil
	})
}
