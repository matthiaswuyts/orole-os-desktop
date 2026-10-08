package app

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/drive"
)

// Drive forwards typed file operations; no credentials or durable state live in
// the desktop process. Preview text is untrusted; dataUrl is a re-encoded PNG.
type Drive struct{ L *Link }

func (s Drive) Status(ctx context.Context) (out drive.Status, err error) {
	err = s.L.call(ctx, "drive.status", nil, &out)
	return
}
func (s Drive) List(ctx context.Context, query drive.Query) (out drive.Page, err error) {
	err = s.L.call(ctx, "drive.list", query, &out)
	return
}
func (s Drive) Search(ctx context.Context, query drive.Query) (out drive.Page, err error) {
	err = s.L.call(ctx, "drive.search", query, &out)
	return
}
func (s Drive) Stat(ctx context.Context, id string) (out drive.File, err error) {
	err = s.L.call(ctx, "drive.stat", map[string]string{"id": id}, &out)
	return
}
func (s Drive) Preview(ctx context.Context, id string) (out drive.Preview, err error) {
	err = s.L.call(ctx, "drive.preview", map[string]string{"id": id}, &out)
	return
}
func (s Drive) Trash(ctx context.Context, id string) (out drive.Mutation, err error) {
	err = s.L.call(ctx, "drive.trash", map[string]string{"id": id}, &out)
	return
}

func (s Drive) Recipients(ctx context.Context) (out []drive.Recipient, err error) {
	err = s.L.call(ctx, "drive.recipients", nil, &out)
	return
}
func (s Drive) Share(ctx context.Context, request drive.ShareRequest) (out drive.ShareResult, err error) {
	err = s.L.call(ctx, "drive.share", request, &out)
	return
}
