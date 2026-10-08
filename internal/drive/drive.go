// Package drive exposes only discovered MCP Drive capabilities. Previews never
// fetch URLs; raster images must arrive inline and are decoded and re-encoded.
package drive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strconv"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/maildrive"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

var (
	ErrNotConfigured = maildrive.ErrNotConfigured
	ErrUnsupported   = maildrive.ErrUnsupported
	ErrInvalidResult = maildrive.ErrInvalidResult
	ErrBadRequest    = errors.New("drive: invalid request")
)

const MaxPreviewText = 128 << 10
const MaxImageBytes = 512 << 10
const MaxImagePixels = 4_000_000

type Capabilities struct {
	CanList    bool `json:"canList"`
	CanSearch  bool `json:"canSearch"`
	CanStat    bool `json:"canStat"`
	CanPreview bool `json:"canPreview"`
	CanTrash   bool `json:"canTrash"`
}
type Status struct {
	maildrive.Connection
	Capabilities Capabilities `json:"capabilities"`
}
type File struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	ParentID     string `json:"parentId"`
	Size         int64  `json:"size"`
	ModifiedTime string `json:"modifiedTime"`
	IsFolder     bool   `json:"isFolder"`
}
type Query struct {
	ParentID  string `json:"parentId"`
	Query     string `json:"query"`
	PageToken string `json:"pageToken"`
	Limit     int    `json:"limit"`
}
type Page struct {
	Files         []File `json:"files"`
	NextPageToken string `json:"nextPageToken"`
}
type Preview struct {
	FileID    string `json:"fileId"`
	MimeType  string `json:"mimeType"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	DataURL   string `json:"dataUrl"`
	Truncated bool   `json:"truncated"`
}
type Mutation struct {
	ID        string `json:"id"`
	Confirmed bool   `json:"confirmed"`
}

type Adapter struct {
	manager maildrive.Manager
	policy  maildrive.Policy
	actor   string
}

func New(manager maildrive.Manager, gate maildrive.Policy) *Adapter {
	return &Adapter{manager: manager, policy: gate, actor: "user"}
}
func (a *Adapter) ForActor(actor string) *Adapter {
	return &Adapter{manager: a.manager, policy: a.policy, actor: actor}
}
func field(name, typ string, required bool, aliases ...string) maildrive.Field {
	return maildrive.Field{Name: name, Type: typ, Required: required, Aliases: aliases}
}

var queryFields = []maildrive.Field{
	field("parent", "string", false, "parentId", "parent_id", "folderId", "folder_id"),
	field("query", "string", false, "query", "q", "search"),
	field("page", "string", false, "pageToken", "page_token", "cursor"),
	field("limit", "integer", false, "limit", "pageSize", "maxResults", "max_results"),
}
var idFields = []maildrive.Field{field("id", "string", true, "fileId", "file_id", "id")}
var operations = map[string]maildrive.Operation{
	"list":    {Names: []string{"list_files", "drive_list_files", "gdrive_list_files"}, Fields: queryFields, Effect: policy.EffectRead},
	"search":  {Names: []string{"search_files", "drive_search", "drive_search_files", "gdrive_search"}, Fields: []maildrive.Field{field("query", "string", true, "query", "q", "search"), queryFields[0], queryFields[2], queryFields[3]}, Effect: policy.EffectRead},
	"stat":    {Names: []string{"get_file", "get_file_metadata", "drive_get_file", "gdrive_get_file"}, Fields: idFields, Effect: policy.EffectRead},
	"preview": {Names: []string{"read_file", "get_file_content", "preview_file", "drive_read_file", "gdrive_read_file"}, Fields: idFields, Effect: policy.EffectRead},
	"trash":   {Names: []string{"trash_file", "drive_trash_file", "gdrive_trash_file"}, Fields: idFields, Effect: policy.EffectDestructive},
}

func (a *Adapter) Status(ctx context.Context) (Status, error) {
	c, b, err := maildrive.Discover(ctx, a.manager, "drive", operations)
	_, list := b["list"]
	_, search := b["search"]
	_, stat := b["stat"]
	_, preview := b["preview"]
	_, trash := b["trash"]
	return Status{Connection: c, Capabilities: Capabilities{list, search, stat, preview, trash}}, err
}
func (a *Adapter) call(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	return maildrive.Call(ctx, a.manager, a.policy, "drive", a.actor, op, operations, args)
}
func (a *Adapter) List(ctx context.Context, q Query) (Page, error) { return a.files(ctx, "list", q) }
func (a *Adapter) Search(ctx context.Context, q Query) (Page, error) {
	if strings.TrimSpace(q.Query) == "" {
		return Page{}, ErrBadRequest
	}
	return a.files(ctx, "search", q)
}
func (a *Adapter) files(ctx context.Context, op string, q Query) (Page, error) {
	if q.Limit < 0 || q.Limit > 100 || len(q.Query) > 2000 {
		return Page{}, ErrBadRequest
	}
	args := map[string]any{}
	for key, value := range map[string]string{"parent": q.ParentID, "query": q.Query, "page": q.PageToken} {
		if value == "" {
			continue
		}
		if _, err := maildrive.ID(value); err != nil {
			return Page{}, ErrBadRequest
		}
		args[key] = value
	}
	if q.Limit > 0 {
		args["limit"] = q.Limit
	}
	raw, err := a.call(ctx, op, args)
	if err != nil {
		return Page{}, err
	}
	rows, token, err := maildrive.Collection(raw, "files", "items")
	if err != nil {
		return Page{}, err
	}
	if len(rows) > 500 {
		return Page{}, ErrInvalidResult
	}
	files := make([]File, 0, len(rows))
	for _, row := range rows {
		file, err := parseFile(row)
		if err != nil {
			return Page{}, err
		}
		files = append(files, file)
	}
	return Page{Files: files, NextPageToken: token}, nil
}
func (a *Adapter) Stat(ctx context.Context, id string) (File, error) {
	if _, err := maildrive.ID(id); err != nil {
		return File{}, ErrBadRequest
	}
	raw, err := a.call(ctx, "stat", map[string]any{"id": id})
	if err != nil {
		return File{}, err
	}
	file, err := parseFile(raw)
	if err != nil {
		return File{}, err
	}
	if file.ID != id {
		return File{}, ErrInvalidResult
	}
	return file, nil
}
func parseFile(raw json.RawMessage) (File, error) {
	m, err := maildrive.Object(raw)
	if err != nil {
		return File{}, err
	}
	if err := maildrive.Strings(m, "id", "fileId", "file_id", "name", "title", "mimeType", "mime_type", "parentId", "parent_id", "modifiedTime", "modified_time"); err != nil {
		return File{}, err
	}
	id, err := maildrive.ID(maildrive.String(m, "id", "fileId", "file_id"))
	if err != nil {
		return File{}, err
	}
	f := File{ID: id, Name: maildrive.TextField(m, 500, "name", "title"), MimeType: maildrive.TextField(m, 150, "mimeType", "mime_type"), ParentID: maildrive.String(m, "parentId", "parent_id"), ModifiedTime: maildrive.TextField(m, 100, "modifiedTime", "modified_time")}
	if f.Name == "" || f.MimeType == "" {
		return File{}, ErrInvalidResult
	}
	if raw, ok := m["parents"]; ok && f.ParentID == "" {
		var parents []string
		if json.Unmarshal(raw, &parents) != nil {
			return File{}, ErrInvalidResult
		}
		if len(parents) > 0 {
			f.ParentID = parents[0]
		}
	}
	if f.ParentID != "" {
		if _, err := maildrive.ID(f.ParentID); err != nil {
			return File{}, err
		}
	}
	if raw, ok := m["size"]; ok {
		if json.Unmarshal(raw, &f.Size) != nil {
			var s string
			if json.Unmarshal(raw, &s) != nil {
				return File{}, ErrInvalidResult
			}
			f.Size, err = strconv.ParseInt(s, 10, 64)
			if err != nil {
				return File{}, ErrInvalidResult
			}
		}
		if f.Size < 0 {
			return File{}, ErrInvalidResult
		}
	}
	f.IsFolder = f.MimeType == "application/vnd.google-apps.folder"
	if raw, ok := m["isFolder"]; ok {
		if json.Unmarshal(raw, &f.IsFolder) != nil {
			return File{}, ErrInvalidResult
		}
	}
	return f, nil
}
func (a *Adapter) Trash(ctx context.Context, id string) (Mutation, error) {
	if _, err := maildrive.ID(id); err != nil {
		return Mutation{}, ErrBadRequest
	}
	raw, err := a.call(ctx, "trash", map[string]any{"id": id})
	if err != nil {
		return Mutation{}, err
	}
	confirmed, err := maildrive.Confirmation(raw, "trash", id)
	if err != nil {
		return Mutation{}, err
	}
	return Mutation{ID: confirmed, Confirmed: true}, nil
}
func (a *Adapter) Preview(ctx context.Context, id string) (Preview, error) {
	if _, err := maildrive.ID(id); err != nil {
		return Preview{}, ErrBadRequest
	}
	raw, err := a.call(ctx, "preview", map[string]any{"id": id})
	if err != nil {
		return Preview{}, err
	}
	return parsePreview(raw, id)
}
func parsePreview(raw json.RawMessage, id string) (Preview, error) {
	m, err := maildrive.Object(raw)
	if err != nil {
		return Preview{}, err
	}
	if err := maildrive.Strings(m, "id", "fileId", "file_id", "mimeType", "mime_type", "text", "content", "data", "base64", "blob"); err != nil {
		return Preview{}, err
	}
	if got := maildrive.String(m, "id", "fileId", "file_id"); got != "" && got != id {
		return Preview{}, ErrInvalidResult
	}
	mime := strings.ToLower(strings.TrimSpace(strings.Split(maildrive.String(m, "mimeType", "mime_type"), ";")[0]))
	p := Preview{FileID: id, MimeType: mime}
	switch mime {
	case "text/plain":
		p.Kind = "text"
	case "text/markdown":
		p.Kind = "markdown"
	case "application/json", "text/json":
		p.Kind = "json"
	case "text/csv":
		p.Kind = "csv"
	case "image/png", "image/jpeg", "image/gif":
		p.Kind = "image"
		encoded := maildrive.String(m, "data", "base64", "blob")
		if len(encoded) == 0 || len(encoded) > base64.StdEncoding.EncodedLen(MaxImageBytes) {
			return Preview{}, ErrUnsupported
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return Preview{}, ErrInvalidResult
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
			return Preview{}, ErrUnsupported
		}
		if "image/"+format != mime {
			return Preview{}, ErrInvalidResult
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return Preview{}, ErrInvalidResult
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return Preview{}, ErrInvalidResult
		}
		if buf.Len() > MaxImageBytes {
			return Preview{}, ErrUnsupported
		}
		p.MimeType = "image/png"
		p.DataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
		return p, nil
	default:
		return Preview{}, ErrUnsupported
	}
	text := maildrive.String(m, "text", "content")
	if _, ok := m["text"]; !ok {
		if _, ok := m["content"]; !ok {
			return Preview{}, ErrUnsupported
		}
	}
	if p.Kind == "json" && !json.Valid([]byte(text)) {
		return Preview{}, ErrInvalidResult
	}
	p.Truncated = len(text) > MaxPreviewText
	if raw, ok := m["truncated"]; ok {
		var truncated bool
		if json.Unmarshal(raw, &truncated) != nil {
			return Preview{}, ErrInvalidResult
		}
		p.Truncated = p.Truncated || truncated
	}
	p.Text = untrusted.Text(text, MaxPreviewText)
	return p, nil
}
