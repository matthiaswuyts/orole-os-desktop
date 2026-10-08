# GitHub and issue-to-agent workflow

The daemon owns GitHub API access; `internal/app/github.go` and `github_agents.go`
are stateless typed bridges. React uses generated MyGo bindings. Authentication
uses the existing GitHub CLI login or a PAT stored in Windows Credential Manager;
tokens are not returned to the frontend or persisted in SQLite.

## Reviewed issue launch

Issue details expose the agent workflow. The user selects an installed built-in
agent and a registered local project whose current GitHub origin matches the
issue repository, reviews the source, and supplies an explicit task. Go checks
confirmation, live origin, installed harness, and the issue's update timestamp.
Issue text is fenced as untrusted reference material, not execution authority.
The agent manager separately authorizes launch through central policy.

The daemon creates an owned branch/worktree and persists the issue URL, exact
reviewed title/body/timestamp, project, user task, session ID, worktree, branch,
and base commit in `github_issue_runs` (migration 0091). A durable request key
prevents retries from starting a second session. Interrupted linkage can be
recovered from the agent session's idempotency key. Worktrees isolate Git changes,
not OS permissions. Agents survive GUI closure, not daemon termination.

APIs: `github.issue.agent.projects`, `github.issue.agent.start`,
`github.issue.agent.runs`, `github.issue.pr.prepare`, `github.issue.pr.publish`.
Existing agent detail/review controls inspect execution, diff and checks.

## Publication is separate from review

The user reviews and commits changes with existing Git tools and stops the agent.
Preparation requires a clean, committed, daemon-owned branch and a local base
matching GitHub's target base. It freezes the exact commit, base, diff, title and
body into a review token. Publication requires explicit confirmation, the same
reviewed snapshot, and both Go policy approvals: `github.branch.push` and
`github.pull.create`. Neither approval may be replaced by issue text or agent
output.

Publication pushes the exact approved SHA to canonical GitHub HTTPS using isolated
Git configuration, without force, hooks, tags or submodule recursion, then creates
a draft PR. It does not automatically commit or merge. Existing-PR lookup avoids
duplicate creation on retry. Unsupported index/filter/submodule configurations
fail closed. API authentication and Git push credentials are independent; a PAT
entered for API access does not configure Git's credential helper.

External editors and remote branch updates cannot be transactionally locked with
the local review. Exact commit pinning and base rechecks protect the reviewed
snapshot; already-dispatched remote effects cannot be rolled back by cancellation.

## Verification

The Go race suite passed issue provenance/recovery, origin mismatch, cancellation,
confirmation, stale/dirty preparation, changed bases, denied publication, local Git
publication/config isolation, and GitHub HTTP publication behavior tests.

On 2026-10-08, an isolated real daemon used existing GitHub CLI authentication to
read repository issue #54. A temporary registered Git repository with the matching
origin was selected. Denying launch created no agent session; explicit approval
launched installed Codex in a daemon-owned worktree with recorded issue provenance.
The smoke stopped that process, retried the same request key, and observed the
same session and persisted source snapshot. An unconfirmed publication request
was rejected. Temporary smoke data was removed after shutdown.

This smoke did not request an external push or PR creation and does not claim a
successful model response for that issue. A separate installed Home composer
smoke received a real Codex response; see `windows-terminals.md`. Live publication
requires user-reviewed real changes, working Git credentials and explicit approval.
