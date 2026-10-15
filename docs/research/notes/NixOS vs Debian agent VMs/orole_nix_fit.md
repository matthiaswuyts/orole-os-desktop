# Orole OS codebase fit for a NixOS (vs Debian) per-agent sandbox guest

Scope: a read-only review of the Orole repo at `C:/code/projects/orole-os`, on branch `jarvis-mode` with a dirty working tree. Line numbers are from that tree on 2026-10-15. Background: `reports/Local agent micro VM sandboxes.md` proposes a "small Debian-slim Linux image with a shell, Python, Node and Chromium", "reproducible", booting from a read-only base with a per-agent qcow2 overlay ([report:51](../../reports/Local%20agent%20micro%20VM%20sandboxes.md), [report:84](../../reports/Local%20agent%20micro%20VM%20sandboxes.md)). The earlier sandbox notes say reproducible image building ("apko/melange, mkosi, Nix, BuildKit") "was not researched" ([linux_microvms_and_containers.md:340](../Local%20agent%20micro%20VM%20sandboxes/linux_microvms_and_containers.md)). Nothing in the repo refers to a sandbox guest yet. Guest-side code does not exist, so every guest mapping below is an inference.

## Q1. Bot configuration and code-task harness/model/project selection: does it map to a declarative Nix environment?

### Takeaway
Bots today declare **capabilities** (hosted tools, MCP server ids, skill names, code-task permission, an access level). They declare no **software environment**: no package list, runtime or image field exists anywhere in `BotConfig`. Code tasks pick only harness, model, reasoning and permission mode, and only from harnesses *installed on the host*. A per-bot "needs Python, Chromium, ffmpeg" list would be a new field. Its shape (a validated JSON list in `bots.config`) matches how Orole already stores bot settings, and it would map naturally onto a Nix `buildEnv`/profile per bot.

### Cited Findings
- `BotConfig` is JSON stored in `bots.config` and "never contains secrets". Its fields are `Tools []BotTool`, `MCPServers []string`, `Skills []string`, `Executive`, `CodeTasks`, `ToolAccess`, `Tasks BotTaskDefaults`, `Jarvis`, `Memory []ScopeAccess`, `Limits`, `Setup` and `Pinned`. None names packages or an environment. — [internal/chat/api.go:85-109](../../internal/chat/api.go)
- Built-in bot tool IDs are a fixed set: `web_search`, `web_fetch`, `datetime`, `research`, `knowledge_search`. `BotTool.Params` "is kept for stored configs but ignored". — [internal/chat/api.go:56-70](../../internal/chat/api.go)
- `ToolAccess` is one of `ask|read|write|all`. It sets how much runs without asking, and "explicit per-tool or stored ask/deny decisions still win". — [internal/chat/api.go:94-117](../../internal/chat/api.go); validated in [internal/chat/store.go:330-334](../../internal/chat/store.go)
- `BotTaskDefaults{Harness, Model, Reasoning, PermissionMode}` can each be fixed or `"auto"` (the bot chooses). `PermissionMode` "is never chosen by the bot". — [internal/chat/api.go:119-133](../../internal/chat/api.go); field validation (no whitespace/control chars, length cap) at [internal/chat/store.go:335-345](../../internal/chat/store.go)
- The daemon resolves a bot's task against the **installed** harnesses only. `planBotTask` fails with "harness %q is not installed" or "no coding agent is installed; ask the user to install Claude Code or Codex". — [internal/daemon/bot_tasks.go:100-125](../../internal/daemon/bot_tasks.go); `taskChoices` filters to installed harnesses at [internal/daemon/bot_tasks.go:81-98](../../internal/daemon/bot_tasks.go)
- `agents.StartSpec` carries `Kind, ProjectID, Machine, Dir, Prompt, Model, Mode, Command, PermissionMode, NewWorktree, Branch, BaseRef ...`. It has no environment or package field. — [internal/agents/types.go:261-286](../../internal/agents/types.go)
- The bot's start call passes `Machine: p.MachineID`, so the execution location comes from the project. — [internal/daemon/bot_tasks.go:61-67](../../internal/daemon/bot_tasks.go)
- Bot-facing tools live in `internal/chat/tools/`: automations, fetch, hosted, info, jarvis, mcp, research, setup, show, skills, tasks. No shell or exec tool exists, so a bot runs software only through a code task. — directory listing of [internal/chat/tools](../../internal/chat/tools)

### Inferences
- A new `BotConfig.Environment` (for example `{packages: ["python3", "ffmpeg", "chromium"], nixpkgs: "<pinned rev>"}`) would follow the existing pattern: a JSON list validated in `store.validate` like `Skills`/`MCPServers`. It would map 1:1 onto a Nix `pkgs.buildEnv { paths = [...]; }` or a per-bot profile. Debian could express the same list as `apt install`, but not hermetically and not with a content hash.
- `BotExport` exports settings but strips several fields on import ([internal/chat/store_test.go:160-175](../../internal/chat/store_test.go) shows `MCPServers`, `ToolAccess`, `Tasks` and `Jarvis` reset). A Nix package list is portable and secret-free, so it could survive export/import. Whether it should (it grants capability) is a policy decision.
- The "installed harnesses" check (`HarnessOption` list) would have to become per-location ("installed in the guest image or closure"). With Nix, "installed" could be computed from the guest closure rather than probed with `--version`.

### Gaps
- No design doc or issue in the repo discusses per-bot runtime environments; I found no evidence of intent either way.
- Whether bots should choose packages themselves (as with `"auto"` harness choice) or only the user should is unspecified.

## Q2. `internal/cliruntime`: how CLI harnesses are downloaded, versioned and pinned, and would Nix replace or complement this in the guest?

### Takeaway
`cliruntime` is a small self-updating package manager for exactly three vendor CLIs (Claude, Codex, Grok). It tracks **latest** (not a pin), verifies vendor hashes where they are published, keeps 2 versions for manual rollback, and verifies a download by **executing it on the host**. Oh My Pi and other harnesses are PATH-only. Inside a NixOS guest, a pinned nixpkgs (or an Orole flake overlay) could replace download, hash check and rollback. The vendor login/auth half of `cliruntime` would still be needed. The hard conflict is that vendor Linux binaries are generally dynamically linked against FHS loader paths, which NixOS lacks without `nix-ld` or patching.

### Cited Findings
- Package doc: "A managed copy lives in Orole's own store and is downloaded from the vendor's official release channel (hash-checked where the channel publishes a hash)." — [internal/cliruntime/spec.go:1-5](../../internal/cliruntime/spec.go)
- Providers are limited to `claude`, `codex` and `grok`, with auth/login/logout args per CLI. — [internal/cliruntime/spec.go:7-41](../../internal/cliruntime/spec.go)
- Release channels: npm registry, the GitHub `openai/codex` latest release, and `https://x.ai/cli`. — [internal/cliruntime/spec.go:52-57](../../internal/cliruntime/spec.go)
- Linux mappings already exist. Claude uses the npm platform package `linux-x64`/`linux-arm64`. Codex uses `x86_64-unknown-linux-musl`, a statically linked musl target. Grok uses `linux-x86_64`. — [internal/cliruntime/spec.go:59-94](../../internal/cliruntime/spec.go)
- `Install` "downloads the latest release" into `<StoreDir>/<id>/<version>/`. It stages, chmods `0o755`, then requires that the binary "has been started once (`--version`)" and reports exactly the expected version. — [internal/cliruntime/install.go:40-108](../../internal/cliruntime/install.go) (version check at :97-98)
- Hashes: Claude and Codex-on-Windows check npm `sha512` integrity ([install.go:135-160](../../internal/cliruntime/install.go), [:163-187](../../internal/cliruntime/install.go)). Codex on other OSes checks GitHub's `sha256` digest ([:241-277](../../internal/cliruntime/install.go)). Grok: "x.ai publishes no hash, so it is trusted on TLS alone" ([:279-286](../../internal/cliruntime/install.go)).
- `keepVersions = 2` "so a bad release can be rolled back by hand"; `prune` removes the rest. — [internal/cliruntime/install.go:22-23,110-133](../../internal/cliruntime/install.go)
- Resolution order is the `OROLE_<ID>_PATH` override, then the newest managed copy, then `PATH`. — [internal/cliruntime/runtime.go:214-229](../../internal/cliruntime/runtime.go); doc [docs/architecture/cli-runtimes.md:9](../../docs/architecture/cli-runtimes.md)
- Latest version lookups are cached for 30 min (`latestTTL`). — [internal/cliruntime/runtime.go:77-78,380-400](../../internal/cliruntime/runtime.go)
- The version probe runs the binary: `m.cfg.Exec(ctx, path, "--version")`. — [internal/cliruntime/runtime.go:285-297](../../internal/cliruntime/runtime.go). The prior report already flags that "`Install` currently executes the binary on the host to verify, so add a guest-side version check" ([report:76](../../reports/Local%20agent%20micro%20VM%20sandboxes.md)).
- Harness detection outside `cliruntime` is plain `exec.LookPath(bin)` plus `--version` on the host. — [internal/agents/adapter.go:151-172](../../internal/agents/adapter.go). Oh My Pi is "`omp` from PATH" only. — [internal/agents/omp.go:39-57](../../internal/agents/omp.go)
- `cliruntime` is consumed by `internal/app/chat.go`, `internal/daemon/{daemon,svc_agents,svc_cliruntime}.go`, `internal/dictation/engine/process.go` and `internal/llm/cli/{claude,codex,grok}.go`. — grep of `cliruntime.` usages
- nixpkgs `claude-code` currently packages the **native binary** from `downloads.claude.ai/claude-code-releases`. It applies `autoPatchelfHook` on ELF hosts and wraps the binary with `DISABLE_AUTOUPDATER 1`, `DISABLE_INSTALLATION_CHECKS 1`, and `PATH` additions for `procps`, `ripgrep`, plus `bubblewrap` and `socat` on Linux. — [nixpkgs claude-code/package.nix](https://raw.githubusercontent.com/NixOS/nixpkgs/master/pkgs/by-name/cl/claude-code/package.nix)
- nixpkgs also packages `codex`, built from the openai/codex repo as a Rust build. Mirrored commits show bumps such as 0.80.0 → 0.84.0; these are undated, so the current version is unknown. — [code.ornl.gov nixpkgs mirror commit](https://code.ornl.gov/nix/nixpkgs/-/commit/8deb8ae4be11f639f47968d33353d553de08486e) (mirror, not primary)
- Precompiled binaries "hardcode a link loader such as `/lib64/ld-linux-x86-64.so.2`", and NixOS "cannot run these binaries" without a shim. `programs.nix-ld.enable = true` installs a shim at that path, and extra libraries come via `NIX_LD_LIBRARY_PATH`. — [nix-community/nix-ld](https://github.com/nix-community/nix-ld)

### Inferences
- **Complement, not replace.** Login/logout/auth status (`authArgs`, [spec.go:28-41](../../internal/cliruntime/spec.go)) is vendor behaviour that Nix does not touch. Download, hash, versioned dirs and the 2-version rollback are exactly what a pinned nixpkgs gives for free, with stronger guarantees: content-addressed store paths and the whole closure pinned, not just one binary.
- **Version lag conflict.** `cliruntime` tracks vendor *latest* within 30 min. nixpkgs lags vendor releases, and the nixpkgs Claude wrapper disables auto-update. To keep "latest Claude Code" semantics, Orole would maintain its own flake/overlay that bumps the version and hash (the data `cliruntime` already fetches: npm sha512 or GitHub sha256), or keep downloading into the guest and run under `nix-ld`.
- **Codex is the easy case.** The Linux asset is musl-static ([spec.go:81](../../internal/cliruntime/spec.go)), so it runs on NixOS unchanged. Claude's Linux native binary and Grok's are most likely dynamically linked against glibc and would need `nix-ld` or `autoPatchelf`; nixpkgs patches Claude this way. On Debian, all three run as-is.
- The guest needs a guest-side `version()`/`Ready()` either way. With Nix, a build-time check (the binary is in the closure, its version fixed by the derivation) could replace the runtime `--version` probe for pinned versions.
- Claude Code's own Linux sandbox wants `bubblewrap` and `socat` on PATH (per the nixpkgs wrapper). A Debian image must add them explicitly; the Nix package declares them.

### Gaps
- I did not verify whether Grok's and Claude's Linux release binaries are dynamic glibc or static; this is inferred from general vendor practice plus nixpkgs using `autoPatchelfHook`.
- The current nixpkgs versions of `claude-code`/`codex` and their typical lag behind vendor releases were not measured.
- Whether the Claude native binary's self-update writes outside its directory (it would fail on the read-only `/nix/store`) was not checked; nixpkgs disables it.

## Q3. `internal/skills`: format and resources; could a skill declare required packages?

### Takeaway
A skill is `<DataDir>/skills/<name>/SKILL.md` plus resource files. The parser reads **only `name` and `description`** and deliberately skips nested maps such as `metadata`. Skills cannot declare dependencies today. Adding a `requires`/`packages` key is a small parser change. The skill would then be a natural unit for "Nix provides on demand" (`nix shell nixpkgs#ffmpeg`-style), but packages fetched on demand cross the no-NIC egress proxy and need a policy decision.

### Cited Findings
- "A skill is a directory `<DataDir>/skills/<name>/` holding SKILL.md (YAML frontmatter with name and description, then Markdown instructions) and optional resource files." The `Skill` struct has `Name, Description, Body, Files []string, Enabled, Source, AddedAt, UpdatedAt`. — [internal/skills/types.go:1-19](../../internal/skills/types.go)
- `ParseFrontmatter` understands only "top-level scalar keys"; "Nested structures such as a metadata map are skipped. It never executes or evaluates anything." It returns `Meta{Name, Description}` only. — [internal/skills/frontmatter.go:14-56](../../internal/skills/frontmatter.go)
- Skills are synced as copies into each harness's native dir: `~/.claude/skills` (or `CLAUDE_CONFIG_DIR`), `~/.agents/skills` / `CODEX_HOME/skills`, and `~/.omp/agent/skills`. — [internal/skills/discover.go:60-75](../../internal/skills/discover.go); sync/install logic in [internal/skills/sync.go:181-299](../../internal/skills/sync.go)
- Skills also reach structured harnesses through the gateway (`orole_list_skills`/`orole_get_skill`), and the enabled-skills index is appended to the system prompt via `SkillsPrompt`. — [internal/agents/gateway.go:1-21](../../internal/agents/gateway.go), [internal/agents/adapter.go:58-65](../../internal/agents/adapter.go)
- The only built-in skill is `skill-creator` (SKILL.md plus `references/examples.md`). — [internal/skills/builtin/skill-creator](../../internal/skills/builtin)

### Inferences
- The `Files` resource list means skills can already ship scripts (for example a `.py` helper). Those scripts implicitly assume an interpreter on PATH. On Debian this is "whatever the image has". On NixOS a declared `packages:` list could be realised per skill (`nix shell`, or added to the agent's profile when the skill is enabled), which makes the dependency explicit and reviewable.
- Scripts in resource files using `#!/bin/bash` break on NixOS (only `/bin/sh` and `/usr/bin/env` exist; see Q7). Orole cannot control third-party skill scripts, so this is a real compatibility tax unless the guest provides an FHS shim (`nix-ld`, `envfs` or `buildFHSEnv`).
- Syncing skills into `~/.claude/skills` etc. writes mutable files under `$HOME`. That is fine on NixOS if `$HOME` is a normal writable dir. It conflicts with a home-manager setup that manages `~/.claude` as read-only store symlinks, so the guest should not use home-manager for those dirs.
- The guest has no NIC, so "on demand" Nix fetches would go through the egress proxy to `cache.nixos.org`. That is one well-known allowlist host and is cache-signed, a better audit story than arbitrary `pip`/`npm` hosts. An offline alternative is to pre-seed the closure into the base image.

### Gaps
- No spec or issue proposes skill dependency metadata. The Agent Skills ecosystem's own conventions for declaring dependencies were not researched here.

## Q4. Project-level dev environments: detection of flake.nix, shell.nix, devenv, devcontainer, .tool-versions, mise

### Takeaway
**None are detected anywhere.** A repo-wide search for `flake.nix`, `shell.nix`, `devenv`, `devcontainer`, `.tool-versions`, `mise`, `direnv`, `asdf`, `nixpkgs` and `NixOS` across Go, TS, TSX, MD and JSON (excluding `reports/` and `research_notes/`) returned zero hits. Projects know only path, machine, git-ness and worktree root, and the repo itself has no Nix/devcontainer files.

### Cited Findings
- `Project` fields: `ID, Name, MachineID, Path, RemoteURL, LastOpened, CreatedAt, WorktreeRoot, WorktreeRootCustom, Missing, IsGit`. The only filesystem probe is `.git`. — [internal/projects/projects.go:49-64,144](../../internal/projects/projects.go)
- `TaskDirectory` resolves a project's execution dir. "project tasks require the project's local machine". Folderless projects get a daemon-owned workspace, and symlink/junction swaps are rejected ("project workspace has moved"). — [internal/projects/task_workspace.go:12-69](../../internal/projects/task_workspace.go)
- Repo root contains `.github, .mygo, cmd, docs, frontend, internal, migrations, profiles, relay, resources, scripts, tests, tools, go.mod, Makefile, start.ps1, mygo.json`, with no `flake.nix`, `shell.nix`, `.devcontainer` or `.tool-versions`. — `ls -a` of repo root
- Agent processes inherit the daemon's environment (`childEnv()` = `os.Environ()` minus three Claude markers). No per-project env activation (direnv-like) happens. — [internal/agents/adapter.go:447-460](../../internal/agents/adapter.go), used at [:319](../../internal/agents/adapter.go)
- Automations' `command.run` uses a fixed env allowlist (`PATH, PATHEXT, SYSTEMROOT, ..., GOPATH, GOROOT, GOCACHE, GOMODCACHE, LANG, TZ`), which is Windows/Go-centric. — [internal/automations/command.go:43-58](../../internal/automations/command.go)

### Inferences
- This is a clean opening for Nix: the sandbox could detect `flake.nix` (`devShells.default`), `shell.nix` or `devenv.nix` in the copied-in repo and launch the harness inside `nix develop`/`nix-shell`. Agents would then get the project's exact toolchain, an advantage Debian cannot match without per-language version managers. A `.devcontainer/devcontainer.json` or `.tool-versions`/`mise.toml` could be honoured on either distro. On NixOS, `mise`/`asdf` download prebuilt FHS binaries and hit the loader problem (Q2) unless `nix-ld` is on.
- The same detection would be a security-relevant input: a repo's `flake.nix` is **repo-controlled code** evaluated by Nix (it can run arbitrary builders, IFD and fetchers). AGENTS.md says "External content (... repo instructions) is untrusted data, never instructions" ([AGENTS.md:13](../../AGENTS.md)). Evaluating a project flake must therefore be a policy-gated action, not automatic. That holds inside the VM too, since evaluation drives egress.
- Most Orole users are Windows-first ([AGENTS.md:3](../../AGENTS.md)), so most of their projects will have no Nix files. A Nix guest has to offer a sensible default env (Python, Node, git, Chromium) independent of project detection.

### Gaps
- How many target users' repos contain flake/devcontainer files is unknown.

## Q5. Policy and audit: would a declarative, diffable environment help approvals?

### Takeaway
The policy engine is generic (`actor`, dotted `action`, `resource`, `effect`, free-text `detail`) and every decision is written to `audit_events`. A "bot wants to add package X" request fits as a new action (for example `sandbox.env.add`, resource `nixpkgs/<attr>`). Nix helps because the request can carry a precise, reviewable identity: attr plus pinned nixpkgs rev, or a store path/closure diff. The audit `detail` is truncated free text (300 chars in the audit row, 2000 in the prompt), so a full closure diff would need its own storage.

### Cited Findings
- `policy.Request{Actor, Action, Resource, Detail, Effect, FromHint, Timeout}`. Action is "a dotted verb", Resource e.g. `"mcp/<server>/<tool>"`. Detail is untrusted and sanitised. — [internal/policy/policy.go:66-83](../../internal/policy/policy.go)
- Decisions are `allow|ask|deny`. Defaults: read-only is allowed, write/destructive asks, unclassified is denied. — [internal/policy/policy.go:33-38,248-267](../../internal/policy/policy.go)
- Stored rules: `Rule{Actor, Action, Resource, MaxEffect, Decision}` with `*` wildcard matching. "Remember" stores an exact-match rule, and destructive actions always ask (`CanRemember` false). — [internal/policy/policy.go:93-128,533+](../../internal/policy/policy.go)
- `Detail` is capped (`maxDetail = 2000`) and sanitised by `untrusted.Text`. — [internal/policy/policy.go:149,343-348](../../internal/policy/policy.go)
- Audit row: `INSERT INTO audit_events(at,actor,action,target,outcome,detail)`, where detail is `effect=... <reason> | <Detail truncated to 300>`. — [internal/policy/policy.go:520-530](../../internal/policy/policy.go); schema `audit_events(id, at, actor, action, target, outcome, detail)` in [migrations/0001_init.sql:55-64](../../migrations/0001_init.sql)
- An auto-approve hook can raise "ask" to "allow" up to a max effect per actor; this is how `ToolAccess` levels work. — [internal/policy/policy.go:144-146,233-246](../../internal/policy/policy.go)
- Agent restarts go through `m.authorize(ctx, "agent.restart", id, ...)`. — [internal/agents/manager.go:1160-1163](../../internal/agents/manager.go)

### Inferences
- With Nix, the approval prompt can show "add `nixpkgs#ffmpeg` @ rev abc123 → +N store paths, +M MB", and a remembered rule can be exact (`resource = nixpkgs/<rev>/ffmpeg`). With apt, the equivalent is a mutable package name whose resolved version depends on mirror state at install time, which is weaker for "Remember" semantics.
- The environment definition (a per-bot or per-sandbox package list) could be stored as data. A diff between the previous and new list is then trivially reviewable and auditable, and rejection means just not building it. Imperative `apt install` inside a running VM leaves no such record unless the guest agent logs it.
- A bot running `pip install`/`npm install` inside its own VM bypasses all of this on either distro. The real control point stays the egress proxy (host allowlist), not the package manager. Nix narrows the legitimate need for broad egress (one cache host) but does not remove it.
- Because `audit_events.detail` is short free text, a closure diff should live in a sandbox table with only a hash or reference in the audit row. This would need a new forward-only migration ([AGENTS.md:20](../../AGENTS.md)).

### Gaps
- There is no existing `sandbox.*` action vocabulary; the naming above is a suggestion.

## Q6. Rollback and snapshots: sessions, worktrees and resume today vs NixOS generations

### Takeaway
Orole's "rollback" today is **git-level plus CLI-level**: each task can get its own worktree and branch (`orole/<slug>`), work is reviewed as a diff against `BaseRef`, and resume means re-launching the CLI with its own session id. Orole has no environment snapshot. NixOS generations would version the *software environment* (which packages and CLIs), which is orthogonal to the qcow2 overlay (mutable disk state) and to git (code). A natural mapping is "sandbox environment generation ↔ an Orole-recorded env revision on the session". Generations do not replace disk snapshots, because `/home`, the repo copy, browser profiles and CLI transcripts are mutable state outside the Nix store.

### Cited Findings
- `Session` stores `Kind, Machine, Dir` ("a worktree when one was created"), `Branch, BaseRef` ("commit the work is reviewed against"), `WorktreeOwned, Model, Status, AgentSessionID` ("the CLI's own session id, used to resume") and `TranscriptRef`. — [internal/agents/types.go:76-110](../../internal/agents/types.go)
- `Restart(ctx, id, fresh, prompt)`: "Without fresh, a CLI that supports it is resumed in its own session; otherwise (or with fresh) the task is started over." It is gated by policy `agent.restart` and refuses to restart a live remote process. — [internal/agents/manager.go:1158-1182](../../internal/agents/manager.go)
- `LaunchSpec.Resume` / `NewAgentSessionID` carry the CLI session id. — [internal/agents/adapter.go:54-57](../../internal/agents/adapter.go)
- The worktree runtime: `Create` makes a worktree on a new branch `orole/<slug>` from base; `Remove` "deletes a worktree the runtime created, discarding uncommitted changes"; `Diff`/`DiffStat` serve review. — [internal/agents/worktrees.go:15-105](../../internal/agents/worktrees.go)
- Project-level worktrees are reconciled with `git worktree list` and stored in the DB. — [internal/projects/worktrees.go:55-371](../../internal/projects/worktrees.go)
- `cliruntime` keeps two CLI versions "so a bad release can be rolled back by hand". — [internal/cliruntime/install.go:22-23](../../internal/cliruntime/install.go)
- Terminal sessions "survive GUI restarts, not daemon exit or reboot". — [AGENTS.md:16](../../AGENTS.md)
- The prior report recommends "Disk snapshots come before memory snapshots" and per-agent qcow2 overlays on a read-only base. — [report:51,88](../../reports/Local%20agent%20micro%20VM%20sandboxes.md)
- A Nix profile "is a sequence of user environments called generations", and `nix-env --rollback` switches to "the highest numbered generation lower than the current generation". — [Nix manual: nix-env --rollback](https://nix.dev/manual/nix/2.28/command-ref/nix-env/rollback)
- In NixOS, each configuration change adds a boot-menu entry, which "allows you to easily roll back to a previous configuration if something goes wrong". — [NixOS manual](https://nixos.org/manual/nixos/stable/#sec-rollback)

### Inferences
- **Mapping proposal.** (a) Base image = a NixOS system closure built from Orole's pinned flake. (b) Per-bot or per-task additions = a Nix *profile* in the guest (`/nix/var/nix/profiles/per-user/agent/...`), whose generation number Orole records on the `Session` beside `AgentSessionID`/`BaseRef`. (c) Mutable state = the qcow2 overlay. Rolling back a bad package addition is then a profile switch (cheap, no reboot), and rolling back a trashed VM is an overlay discard. On Debian, (b) collapses into (c): an `apt install` is only undone by discarding the overlay or snapshot, which also loses work.
- Resume semantics change little. `Restart` without `fresh` relaunches the CLI with `--resume`, but the env must be the same generation, or the CLI binary version may differ from the transcript's. Recording the generation and store path of the harness binary per session makes resume reproducible; nothing today records the harness version on the session.
- The NixOS *system* generation (boot menu) is less useful in a micro VM booted directly by QEMU with a kernel and initrd. Orole would pick the system closure itself, and in-guest `nixos-rebuild` would need the Nix daemon and store writes in the overlay. Per-user profiles are the practical granularity.
- A read-only shared `/nix/store` base plus per-agent overlay deduplicates well, because packages are content-addressed and identical across agents. On Debian, per-agent `apt install`s are duplicated in each overlay.

### Gaps
- Overlay growth from Nix store GC/optimise inside a qcow2 overlay, and whether `nix-store --optimise` hardlinks interact badly with overlay snapshots, were not researched.
- There is no measurement of NixOS minimal guest closure size vs Debian-slim with Python, Node and Chromium.

## Q7. FHS/Debian path assumptions in code that would run in (or talk to) the guest

### Takeaway
Orole's existing Linux code paths (Linux terminals, SSH "posix" machines) mostly use `/bin/sh` and `/usr/bin/env`, the **only** two FHS paths NixOS keeps, so they would work. Two concrete breakages exist: the SSH host-memory probe hard-codes `/bin/hostname`, `/bin/uname` and `/bin/cat`, and third-party `#!/bin/bash` or prebuilt-glibc binaries need `nix-ld`. The bigger mismatch is that hooks and the gateway assume they run on the **host** with Windows paths and the named pipe, so they need a guest-side relay whatever the distro.

### Cited Findings
- The Linux terminal backend runs `exec.Command("/bin/sh", "-c", s.Command)` and passes the caller's env explicitly. Shells are found via `exec.LookPath` (bash, zsh, fish, sh). — [internal/terminals/backend_linux.go:25-60](../../internal/terminals/backend_linux.go)
- tmux sessions run `/usr/bin/env -i <env...> /bin/sh -c <cmd>`. — [internal/terminals/tmux_linux.go:80-88](../../internal/terminals/tmux_linux.go). The documented Linux requirements include "`/bin/sh`, and standard `stty`/`sleep` utilities", "tmux 3.2+ on `PATH`, `/usr/bin/env`". — [docs/architecture/linux-terminals.md:12,95-96](../../docs/architecture/linux-terminals.md)
- SSH machine host-memory probe: ``linuxMemoryProbe = `LC_ALL=C; export LC_ALL; /bin/hostname; /bin/uname -m; /bin/cat /proc/meminfo` ``, run via `/bin/sh -c`. — [internal/machines/host_memory.go:16-41](../../internal/machines/host_memory.go)
- The SSH agent probe uses `command -v claude|codex` and requires an absolute path. — [internal/machines/agent_process.go:46-58](../../internal/machines/agent_process.go). Remote execution requires "an absolute clean path" because "Remote PATH lookup cannot bind approval to an exact executable". — [internal/machines/execute.go:150-158](../../internal/machines/execute.go)
- Claude Code hooks are written as `"<hookExe>" agent-hook <session> <Event>`, where `hookExe` is the host daemon executable with forward slashes "so it survives bash, cmd and PowerShell". The settings file is written into the host session dir and passed via `--settings`. — [internal/agents/claude.go:218-249](../../internal/agents/claude.go). The hook client connects to the daemon over `control.NewClient(dataDir)` (pipe IPC) and always exits 0. — [internal/agents/hook/hook.go:1-40](../../internal/agents/hook/hook.go)
- The gateway is launched as `<GatewayExe> mcp-gateway` with env `OROLE_GATEWAY_SESSION` and `OROLE_HOME`. "Remote machines cannot reach the daemon's pipe and never get it." — [internal/agents/gateway.go:3-20](../../internal/agents/gateway.go)
- Data dir: `%LOCALAPPDATA%\Orole` on Windows, `os.UserConfigDir()/orole` (`~/.config/orole`) elsewhere. — [internal/paths/paths.go:11-31](../../internal/paths/paths.go)
- Pipe-mode agents: `startPipe` runs `exec.Command(bin, ...)` with `cmd.Dir = dir` and the inherited env, inside a job object. — [internal/agents/adapter.go:316-340](../../internal/agents/adapter.go)
- Codex-on-Windows layout requires `.exe` helpers (`codex-windows-sandbox-setup.exe` etc.). On Linux a single binary is used. — [internal/cliruntime/install.go:162-238](../../internal/cliruntime/install.go), [internal/cliruntime/runtime.go:263-275](../../internal/cliruntime/runtime.go)
- MCP servers in Orole are **remote URL** servers only (`ServerConfig{URL, Transport, ...}`; no command or stdio field), so Orole itself never spawns `npx`/`uvx` MCP servers that would need Node or Python in the guest. — [internal/mcp/types.go:46-58](../../internal/mcp/types.go)
- NixOS: `#!/bin/sh` is fine but `#!/bin/bash` is not; Playwright switched its scripts to `/usr/bin/env bash` for NixOS. — [CodeCrafters forum](https://forum.codecrafters.io/t/shebangs-suppose-fhs-compliant-system/9473); [nixpkgs/playwright commit mirror](https://code.ornl.gov/mdf/nixpkgs/-/commit/67b5c21b5853a9dc70a69e11a1044791354ebf99) (mirror, secondary)
- Playwright's `playwright install` browsers "will not function due to missing dependencies" on NixOS. The fix is nixpkgs `playwright-driver.browsers` via `PLAYWRIGHT_BROWSERS_PATH`, and the npm Playwright version must match the nixpkgs one. — [NixOS Wiki: Playwright](https://wiki.nixos.org/wiki/Playwright); [Discourse version sync thread](https://discourse.nixos.org/t/synchronize-versions-of-playwright-driver-browsers-and-npm-package/66267)

### Inferences
- **Works on NixOS as-is:** Linux terminal backend (`/bin/sh`, `/usr/bin/env`, LookPath'd shells, explicit env so PATH survives `env -i`), SSH `command -v` agent probe (absolute `/run/current-system/sw/bin/...` or `/nix/store/...` paths pass the absolute-path check), Codex musl binary.
- **Breaks on NixOS:** `linuxMemoryProbe` (no `/bin/hostname`, `/bin/uname`, `/bin/cat`) if a NixOS guest is ever reached via the SSH machines path. Agents' `pip install`ed wheels with native deps, `playwright install` browsers, `mise`/`asdf` tool downloads and npm packages with prebuilt binaries all need `nix-ld`. The prior report's plan for "the distro or Playwright Chromium" ([linux_microvms_and_containers.md:332](../Local%20agent%20micro%20VM%20sandboxes/linux_microvms_and_containers.md)) must become `playwright-driver.browsers` on NixOS, with a version pin coupled to any agent-installed npm Playwright.
- **Approval binding is stronger with Nix.** `execute.go` refuses PATH lookups so an approval binds to an exact executable. A `/nix/store/<hash>-claude-code-X/bin/claude` path binds to exact content, while `/usr/bin/claude` on Debian binds only to a mutable location. The profile symlinks (`/run/current-system/sw/bin`, `~/.nix-profile/bin`) do not have this property, so approvals should record the resolved store path.
- **Distro-neutral work.** The hook command, settings file path and gateway exe are host Windows paths, and the hook and gateway talk to the host pipe. In a guest, the guest agent must provide its own `agent-hook`/`mcp-gateway` relay binary at a guest path, as the prior report proposes ("host-side `orole-daemon mcp-gateway` per session, its stdio relayed into the guest", [report:74](../../reports/Local%20agent%20micro%20VM%20sandboxes.md)). On NixOS that binary would be part of the system closure with a stable `/run/current-system/sw/bin/orole-guest` path, or baked as a store path into the generated `claude-settings.json`.
- **Build-host conflict.** Nix does not run natively on Windows; the mainstream route is WSL2. — [NLnet libnix](https://nlnet.nl/project/libnix/) (native port in progress); [NixOS Discourse "Nix on Windows"](https://discourse.nixos.org/t/nix-on-windows/1113/75). Orole's dev and build flow is Windows-first (`go tool mygo build` produces an NSIS installer, [AGENTS.md:27-29](../../AGENTS.md)). So a NixOS guest image must be built in Linux CI (`.github/`) and shipped as a prebuilt qcow2/kernel/initrd, or built inside a guest VM. Debian-slim images have the same Windows-host build problem with `debootstrap`/`mkosi`, so this is roughly neutral. Nix's advantage is that the build is bit-reproducible from a pinned flake lock.
- **Debian conflicts are fewer but softer.** Debian matches every vendor binary, Playwright download and pip wheel, which suits "agent installs whatever it wants". Its weakness is the lack of a pinned, diffable, deduplicated environment (Q1, Q5, Q6).

### Gaps
- I did not test whether Claude Code, Codex or Oh My Pi spawn subprocesses with hard-coded `/bin/bash` or `/usr/bin/*` (for example Claude Code's Bash tool shell selection) that would fail on NixOS without `nix-ld`/`envfs`.
- Oh My Pi's distribution (npm/bun package with native deps?) on Linux was not investigated; Orole treats it as PATH-only ([internal/agents/omp.go:39-57](../../internal/agents/omp.go)).
- The repo has no guest-side code, so every guest mapping above is design inference, not observed behaviour.
