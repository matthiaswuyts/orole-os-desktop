// Package ipc implements the versioned, authenticated local RPC between the
// desktop app (or CLI) and orole-daemon. Messages are newline-delimited JSON.
//
// Wire format:
//
//	request : {"id":1,"method":"daemon.health","params":{...}}
//	response: {"id":1,"result":{...}}  or  {"id":1,"error":{"code":"x","message":"y"}}
//	event   : {"event":"agent.updated","data":{...}}
//
// The first request on a connection MUST be "hello" carrying the protocol
// version and the auth token; every other method is refused until then.
package ipc

import "encoding/json"

// ProtocolVersion is bumped on incompatible changes. The server accepts
// clients whose MinVersion..Version range contains ProtocolVersion.
const ProtocolVersion = 1

// MaxMessageBytes bounds a single frame.
const MaxMessageBytes = 4 << 20

// Error codes.
const (
	CodeUnauthenticated = "unauthenticated"
	CodeBadRequest      = "bad_request"
	CodeNotFound        = "not_found"
	CodeCanceled        = "canceled"
	CodeTimeout         = "timeout"
	CodeVersion         = "version_mismatch"
	CodeInternal        = "internal"
	CodeDenied          = "denied"
	CodeTooLarge        = "too_large"
)

// Message is any frame on the wire.
type Message struct {
	ID     uint64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Error is an RPC failure.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// HelloParams opens a session.
type HelloParams struct {
	MinVersion int    `json:"minVersion"`
	Version    int    `json:"version"`
	Token      string `json:"token"`
	Client     string `json:"client"`
}

// HelloResult is the server's reply.
type HelloResult struct {
	Version       int    `json:"version"`
	DaemonVersion string `json:"daemonVersion"`
	PID           int    `json:"pid"`
}

// SubscribeParams selects event names ("*" for all, "prefix.*" for a prefix).
type SubscribeParams struct {
	Topics []string `json:"topics"`
}
