# Credentials

`internal/credentials` is the only place secrets are persisted.

```go
type Store interface {
    Set(key string, secret []byte) error
    Get(key string) ([]byte, error)   // ErrNotFound when absent
    Delete(key string) error          // deleting a missing key is not an error
}
func New() Store
```

- **Windows**: Credential Manager generic credentials (`github.com/danieljoos/wincred`), target name
  `"Orole OS/" + key`, persistence `LocalMachine` (survives log off, still per-user and DPAPI-protected).
  Test: `store_windows_test.go` round-trips and deletes a key.
- **Other OS**: `store_other.go` returns an error ("no OS credential store implemented"). Linux Secret Service and
  macOS Keychain are not built.

## Keys in use

| Key | Holder | Content |
|---|---|---|
| `github/token` | `internal/github` | a pasted personal access token |
| `mcp/<server id>` | `internal/mcp` | OAuth/bearer tokens, client secrets (package exists; not yet wired to IPC) |

## Rules

- Never SQLite, logs, environment dumps, bound service return values or the frontend.
- Daemon code reads a secret just-in-time and keeps it in memory only.
- Diagnostics (`orole daemon diagnostics`, Settings > Diagnostics) proves the store works by writing, reading and
  deleting the throwaway key `diagnostics/probe`; it never lists or prints other entries.
- Secrets are listed per user in Windows "Credential Manager" under Generic Credentials, names starting with `Orole OS/`.

## GitHub connection

Connect in GitHub or Settings > GitHub using an existing `gh auth login` session or a pasted PAT.
`gh auth token --hostname github.com` is executed directly; its output is held in daemon memory only.
Pasted tokens are validated against `/user` before saving under `github/token`. The only database
preference is `github.source` (`gh` or `pat`), never the credential itself. Disconnect removes the
stored PAT and source preference without changing the user's gh login.

The typed `GitHub` desktop service forwards to `github.*` daemon IPC. Read responses have 60-second
in-memory caching, ETag revalidation, pagination and rate-limit backoff. Issue/PR bodies, comments,
patches and CI logs are `github.Untrusted` data, rendered without raw HTML. Agent consumers must
preserve that data boundary (for example using `Untrusted.Fenced`).

Creating/editing issues, posting comments and cloning require `confirmed:true` after a UI confirmation
and are audited. Central policy rules are evaluated as writes: a Deny overrides confirmation, while
Ask is answered by the feature's confirmation dialog. `github.Authorize` has no permissive production
default. OAuth device flow and GitHub
App authentication require a project-owned registered client/app ID; none is configured, so those
flows are not offered. Future sources implement `github.TokenSource`.

Read-only live checks: set `OROLE_GITHUB_LIVE=1` and run
`go test -run Live -v ./internal/github` or
`go test -run GitHubLive -v ./internal/daemon`.
Neither check creates an issue or comment. CI log archives are downloaded without forwarding
credentials to the signed download URL, capped at 32 MB compressed and 64 MB processed; displayed
files retain at most 256 KB each.
