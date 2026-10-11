# CLI account runtimes

Claude, ChatGPT (Codex) and Grok can be used with the user's own plan through the vendor's CLI. The CLI signs in
and keeps the credentials; Orole never reads, stores or logs them. `internal/cliruntime` owns the state, the daemon
exposes it as `cli.providers.{list,install,login,logout}`, and Settings > AI providers > Accounts shows it.

## Which CLI runs
In order: `OROLE_CLAUDE_PATH` / `OROLE_CODEX_PATH` / `OROLE_GROK_PATH` (a missing override means "not installed",
never a silent fallback), the newest managed copy in `<data>/cli/<id>/<version>/`, then the first match on `PATH`.

## Status
Each provider reports `installed`, `path`, `source`, `version`, `latest`, `updateAvailable`, `auth`, `account` and
`installable`. A lookup that fails (offline, rate limit) is reported in `problem` and never hides the rest.

| Provider | Version | Latest release | Sign-in |
| --- | --- | --- | --- |
| Claude | `claude --version` | npm `@anthropic-ai/claude-code` `latest` | `claude auth status` (JSON) |
| ChatGPT | `codex --version` | GitHub `openai/codex` latest release | `codex login status` |
| Grok | `grok --version` | `https://x.ai/cli/stable` | not askable: Sign in is always offered |

Latest versions are cached for 30 minutes.

## Managed install and update
Install downloads from the vendor's own channel into a staging directory, checks the hash where the channel
publishes one (Claude: npm `dist.integrity` sha512; Codex: the GitHub asset sha256; Grok: none, TLS only), runs
`--version` and requires the expected version, then moves it into place. A copy that fails any step never replaces a
working one. The two newest versions are kept. Update is the same operation: it adds Orole's own copy and leaves a
CLI the user installed themselves untouched. Install and sign-in are audited (`cli.install`, `cli.login`,
`cli.logout`).

## Terms
Anthropic limits Claude subscription sign-in to ordinary individual use of the unmodified CLI. The Accounts page says so
next to the Claude row.
