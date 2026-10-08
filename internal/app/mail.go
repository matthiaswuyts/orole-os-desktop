package app

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/mail"
)

// Mail forwards typed mail operations to the daemon. All returned content is
// untrusted plain text; mutations wait for the daemon policy approval.
type Mail struct{ L *Link }

func (s Mail) Status(ctx context.Context) (out mail.Status, err error) {
	err = s.L.call(ctx, "mail.status", nil, &out)
	return
}
func (s Mail) Folders(ctx context.Context) (out []mail.Folder, err error) {
	err = s.L.call(ctx, "mail.folders", nil, &out)
	return
}
func (s Mail) List(ctx context.Context, query mail.Query) (out mail.Page, err error) {
	err = s.L.call(ctx, "mail.list", query, &out)
	return
}
func (s Mail) Search(ctx context.Context, query mail.Query) (out mail.Page, err error) {
	err = s.L.call(ctx, "mail.search", query, &out)
	return
}
func (s Mail) Thread(ctx context.Context, id string) (out mail.Thread, err error) {
	err = s.L.call(ctx, "mail.thread", map[string]string{"id": id}, &out)
	return
}
func (s Mail) Send(ctx context.Context, message mail.Compose) (out mail.Mutation, err error) {
	err = s.L.call(ctx, "mail.send", message, &out)
	return
}
func (s Mail) Reply(ctx context.Context, message mail.Reply) (out mail.Mutation, err error) {
	err = s.L.call(ctx, "mail.reply", message, &out)
	return
}
func (s Mail) Trash(ctx context.Context, id string) (out mail.Mutation, err error) {
	err = s.L.call(ctx, "mail.trash", map[string]string{"id": id}, &out)
	return
}
