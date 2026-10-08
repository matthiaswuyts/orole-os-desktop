// Package github is Orole OS's GitHub integration: token sources, a REST API
// client (pagination, ETag caching, rate-limit awareness) and typed models.
//
// Security model
//   - Tokens live in memory only (see [TokenSource]); they are never logged,
//     never put in errors (see [Redact]) and never persisted by this package.
//     The only persistence is a pasted PAT in the OS credential store, done by
//     [StoreSource] through internal/credentials.
//   - Everything GitHub returns that a human or attacker can author (issue and
//     PR titles/bodies, comments, labels, branch names, diffs, CI logs) is typed
//     [Untrusted]. It is data, never instructions: agent-facing code MUST use
//     [Untrusted.Fenced] or otherwise keep it out of the instruction channel.
//   - Writes (create/edit issue, comment, clone) call the client's [Authorize]
//     hook first. A client with no hook refuses all writes ([ErrNoAuthorizer]);
//     there is deliberately no permissive default outside tests.
//
// Device flow / GitHub App: OAuth device flow and GitHub App installation tokens
// need a registered client ID / app ID owned by the Orole OS project, which does
// not exist yet, so no such source is implemented. Add one by implementing
// [TokenSource] (Token + Invalidate); nothing else in this package changes.
package github
