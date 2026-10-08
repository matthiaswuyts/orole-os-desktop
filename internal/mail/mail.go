// Package mail adapts discovered MCP contracts to plain-text mail models.
package mail

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/mail"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/maildrive"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

var (
	ErrNotConfigured = maildrive.ErrNotConfigured
	ErrUnsupported   = maildrive.ErrUnsupported
	ErrInvalidResult = maildrive.ErrInvalidResult
	ErrBadRequest    = errors.New("mail: invalid request")
)

type Capabilities struct {
	CanFolders bool `json:"canFolders"`
	CanList    bool `json:"canList"`
	CanSearch  bool `json:"canSearch"`
	CanThread  bool `json:"canThread"`
	CanSend    bool `json:"canSend"`
	CanReply   bool `json:"canReply"`
	CanTrash   bool `json:"canTrash"`
}

type Status struct {
	maildrive.Connection
	Capabilities Capabilities `json:"capabilities"`
}

type Folder struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UnreadCount int    `json:"unreadCount"`
}

type Message struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	Subject  string   `json:"subject"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	Date     string   `json:"date"`
	Snippet  string   `json:"snippet"`
	Body     string   `json:"body"`
	Unread   bool     `json:"unread"`
}

type Page struct {
	Messages      []Message `json:"messages"`
	NextPageToken string    `json:"nextPageToken"`
}

type Thread struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
}

type Query struct {
	FolderID  string `json:"folderId"`
	Query     string `json:"query"`
	PageToken string `json:"pageToken"`
	Limit     int    `json:"limit"`
}

type Compose struct {
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

type Reply struct {
	ThreadID string `json:"threadId"`
	Body     string `json:"body"`
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

// ForActor returns an independent adapter for an agent/automation policy identity.
func (a *Adapter) ForActor(actor string) *Adapter {
	return &Adapter{manager: a.manager, policy: a.policy, actor: actor}
}

func field(name, typ string, required bool, aliases ...string) maildrive.Field {
	return maildrive.Field{Name: name, Type: typ, Required: required, Aliases: aliases}
}

var queryFields = []maildrive.Field{
	field("folder", "string", false, "folderId", "folder_id", "folder", "labelId"),
	field("query", "string", false, "query", "q", "search"),
	field("page", "string", false, "pageToken", "page_token", "cursor"),
	field("limit", "integer", false, "limit", "maxResults", "max_results", "pageSize"),
}
var operations = map[string]maildrive.Operation{
	"folders": {Names: []string{"list_folders", "mail_list_folders", "list_labels", "gmail_list_labels"}, Effect: policy.EffectRead},
	"list":    {Names: []string{"list_messages", "mail_list_messages", "gmail_list_messages", "list_emails"}, Fields: queryFields, Effect: policy.EffectRead},
	"search":  {Names: []string{"search_messages", "search_emails", "mail_search", "gmail_search", "gmail_search_messages"}, Fields: []maildrive.Field{field("query", "string", true, "query", "q", "search"), queryFields[0], queryFields[2], queryFields[3]}, Effect: policy.EffectRead},
	"thread":  {Names: []string{"get_thread", "mail_get_thread", "gmail_get_thread", "read_thread"}, Fields: []maildrive.Field{field("id", "string", true, "threadId", "thread_id", "id")}, Effect: policy.EffectRead},
	"send":    {Names: []string{"send_email", "send_message", "mail_send", "gmail_send_email", "gmail_send_message"}, Fields: []maildrive.Field{field("to", "recipients", true, "to", "recipients"), field("subject", "string", true, "subject"), field("body", "string", true, "body", "text", "content")}, Effect: policy.EffectWrite},
	"reply":   {Names: []string{"reply_email", "reply_message", "mail_reply", "gmail_reply", "reply_to_thread"}, Fields: []maildrive.Field{field("id", "string", true, "threadId", "thread_id"), field("body", "string", true, "body", "text", "content")}, Effect: policy.EffectWrite},
	"trash":   {Names: []string{"trash_message", "trash_email", "mail_trash", "gmail_trash_message"}, Fields: []maildrive.Field{field("id", "string", true, "messageId", "message_id", "id")}, Effect: policy.EffectDestructive},
}

func (a *Adapter) Status(ctx context.Context) (Status, error) {
	c, b, err := maildrive.Discover(ctx, a.manager, "mail", operations)
	_, folders := b["folders"]
	_, list := b["list"]
	_, search := b["search"]
	_, thread := b["thread"]
	_, send := b["send"]
	_, reply := b["reply"]
	_, trash := b["trash"]
	return Status{Connection: c, Capabilities: Capabilities{folders, list, search, thread, send, reply, trash}}, err
}

func (a *Adapter) call(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	return maildrive.Call(ctx, a.manager, a.policy, "mail", a.actor, op, operations, args)
}

func (a *Adapter) Folders(ctx context.Context) ([]Folder, error) {
	raw, err := a.call(ctx, "folders", nil)
	if err != nil {
		return nil, err
	}
	rows, _, err := maildrive.Collection(raw, "folders", "labels")
	if err != nil {
		return nil, err
	}
	if len(rows) > 500 {
		return nil, ErrInvalidResult
	}
	out := make([]Folder, 0, len(rows))
	for _, row := range rows {
		m, err := maildrive.Object(row)
		if err != nil {
			return nil, err
		}
		if err := maildrive.Strings(m, "id", "name"); err != nil {
			return nil, err
		}
		id, err := maildrive.ID(maildrive.String(m, "id"))
		if err != nil {
			return nil, err
		}
		name := maildrive.TextField(m, 300, "name")
		if name == "" {
			return nil, ErrInvalidResult
		}
		var unread int
		if v, ok := m["unreadCount"]; ok {
			if json.Unmarshal(v, &unread) != nil || unread < 0 {
				return nil, ErrInvalidResult
			}
		}
		out = append(out, Folder{ID: id, Name: name, UnreadCount: unread})
	}
	return out, nil
}

func queryArgs(q Query) (map[string]any, error) {
	if q.Limit < 0 || q.Limit > 100 || len(q.Query) > 2000 {
		return nil, ErrBadRequest
	}
	args := map[string]any{}
	for key, value := range map[string]string{"folder": q.FolderID, "query": q.Query, "page": q.PageToken} {
		if value == "" {
			continue
		}
		if _, err := maildrive.ID(value); err != nil {
			return nil, ErrBadRequest
		}
		args[key] = value
	}
	if q.Limit > 0 {
		args["limit"] = q.Limit
	}
	return args, nil
}

func (a *Adapter) List(ctx context.Context, q Query) (Page, error) { return a.messages(ctx, "list", q) }
func (a *Adapter) Search(ctx context.Context, q Query) (Page, error) {
	if strings.TrimSpace(q.Query) == "" {
		return Page{}, ErrBadRequest
	}
	return a.messages(ctx, "search", q)
}
func (a *Adapter) messages(ctx context.Context, op string, q Query) (Page, error) {
	args, err := queryArgs(q)
	if err != nil {
		return Page{}, err
	}
	raw, err := a.call(ctx, op, args)
	if err != nil {
		return Page{}, err
	}
	rows, next, err := maildrive.Collection(raw, "messages", "emails", "items")
	if err != nil {
		return Page{}, err
	}
	messages, err := parseMessages(rows)
	if err != nil {
		return Page{}, err
	}
	return Page{Messages: messages, NextPageToken: next}, nil
}
func (a *Adapter) Thread(ctx context.Context, id string) (Thread, error) {
	if _, err := maildrive.ID(id); err != nil {
		return Thread{}, ErrBadRequest
	}
	raw, err := a.call(ctx, "thread", map[string]any{"id": id})
	if err != nil {
		return Thread{}, err
	}
	m, err := maildrive.Object(raw)
	if err != nil {
		return Thread{}, err
	}
	if err := maildrive.Strings(m, "id", "threadId", "thread_id"); err != nil {
		return Thread{}, err
	}
	got := maildrive.String(m, "id", "threadId", "thread_id")
	if got != id {
		return Thread{}, ErrInvalidResult
	}
	rows, _, err := maildrive.Collection(raw, "messages", "emails")
	if err != nil {
		return Thread{}, err
	}
	messages, err := parseMessages(rows)
	if err != nil {
		return Thread{}, err
	}
	for _, message := range messages {
		if message.ThreadID != "" && message.ThreadID != id {
			return Thread{}, ErrInvalidResult
		}
	}
	return Thread{ID: id, Messages: messages}, nil
}

func validBody(body string) bool {
	return strings.TrimSpace(body) != "" && len(body) <= 128<<10 && untrusted.Text(body, 0) == body
}
func (a *Adapter) Send(ctx context.Context, p Compose) (Mutation, error) {
	if len(p.To) == 0 || len(p.To) > 100 || len(p.Subject) > 1000 || untrusted.Line(p.Subject, 0) != p.Subject || !validBody(p.Body) {
		return Mutation{}, ErrBadRequest
	}
	for _, recipient := range p.To {
		if untrusted.Line(recipient, 0) != recipient {
			return Mutation{}, ErrBadRequest
		}
		if _, err := mail.ParseAddress(recipient); err != nil {
			return Mutation{}, ErrBadRequest
		}
	}
	return a.mutate(ctx, "send", "", map[string]any{"to": p.To, "subject": p.Subject, "body": p.Body})
}
func (a *Adapter) Reply(ctx context.Context, p Reply) (Mutation, error) {
	if _, err := maildrive.ID(p.ThreadID); err != nil || !validBody(p.Body) {
		return Mutation{}, ErrBadRequest
	}
	return a.mutate(ctx, "reply", "", map[string]any{"id": p.ThreadID, "body": p.Body})
}
func (a *Adapter) Trash(ctx context.Context, id string) (Mutation, error) {
	if _, err := maildrive.ID(id); err != nil {
		return Mutation{}, ErrBadRequest
	}
	return a.mutate(ctx, "trash", id, map[string]any{"id": id})
}
func (a *Adapter) mutate(ctx context.Context, op, expected string, args map[string]any) (Mutation, error) {
	raw, err := a.call(ctx, op, args)
	if err != nil {
		return Mutation{}, err
	}
	id, err := maildrive.Confirmation(raw, op, expected)
	if err != nil {
		return Mutation{}, err
	}
	return Mutation{ID: id, Confirmed: true}, nil
}

func parseMessages(rows []json.RawMessage) ([]Message, error) {
	if len(rows) > 500 {
		return nil, ErrInvalidResult
	}
	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		m, err := maildrive.Object(row)
		if err != nil {
			return nil, err
		}
		if err := maildrive.Strings(m, "id", "messageId", "message_id", "threadId", "thread_id", "subject", "from", "sender", "date", "receivedAt", "snippet", "preview", "body", "text", "html", "bodyHtml"); err != nil {
			return nil, err
		}
		id, err := maildrive.ID(maildrive.String(m, "id", "messageId", "message_id"))
		if err != nil {
			return nil, err
		}
		threadID := maildrive.String(m, "threadId", "thread_id")
		if threadID != "" {
			if _, err := maildrive.ID(threadID); err != nil {
				return nil, err
			}
		}
		msg := Message{ID: id, ThreadID: threadID, Subject: plain(maildrive.String(m, "subject"), 1000), From: maildrive.TextField(m, 500, "from", "sender"), Date: maildrive.TextField(m, 100, "date", "receivedAt"), Snippet: plain(maildrive.String(m, "snippet", "preview"), 1000), Body: plain(maildrive.String(m, "body", "text", "html", "bodyHtml"), 128<<10), To: []string{}}
		if raw, ok := m["to"]; ok {
			var recipients []string
			if json.Unmarshal(raw, &recipients) != nil {
				var single string
				if json.Unmarshal(raw, &single) != nil {
					return nil, ErrInvalidResult
				}
				recipients = []string{single}
			}
			if len(recipients) > 100 {
				return nil, ErrInvalidResult
			}
			for _, recipient := range recipients {
				msg.To = append(msg.To, untrusted.Line(recipient, 500))
			}
		}
		if raw, ok := m["unread"]; ok && json.Unmarshal(raw, &msg.Unread) != nil {
			return nil, ErrInvalidResult
		}
		out = append(out, msg)
	}
	return out, nil
}

// plain is a conservative HTML-to-text scanner. Tags, comments, and active/raw
// elements are discarded, not executed or loaded. Consumers must render as text.
func plain(s string, limit int) string {
	var out strings.Builder
	blocked := ""
	for i := 0; i < len(s); {
		if s[i] != '<' {
			if blocked == "" {
				out.WriteByte(s[i])
			}
			i++
			continue
		}
		if i+1 == len(s) || !(s[i+1] >= 'a' && s[i+1] <= 'z' || s[i+1] >= 'A' && s[i+1] <= 'Z' || s[i+1] == '/' || s[i+1] == '!') {
			if blocked == "" {
				out.WriteByte('<')
			}
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				break
			}
			i += end + 7
			continue
		}
		j, quote := i+1, byte(0)
		for ; j < len(s); j++ {
			c := s[j]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
			} else if c == '>' {
				break
			}
		}
		if j == len(s) {
			break
		}
		tag := strings.ToLower(strings.TrimSpace(s[i+1 : j]))
		closing := strings.HasPrefix(tag, "/")
		tag = strings.TrimPrefix(tag, "/")
		if k := strings.IndexAny(tag, " \t\r\n/"); k >= 0 {
			tag = tag[:k]
		}
		if blocked != "" {
			if closing && tag == blocked {
				blocked = ""
			}
		} else {
			switch tag {
			case "script", "style", "iframe", "object", "svg", "head", "template":
				if !closing {
					blocked = tag
				}
			case "br", "p", "div", "li", "tr":
				out.WriteByte('\n')
			}
		}
		i = j + 1
	}
	return untrusted.Text(html.UnescapeString(out.String()), limit)
}
