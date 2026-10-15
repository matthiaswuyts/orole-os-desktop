# Agent sandbox products and architectures (state as of mid-October 2026)

Scope: how agent products and open-source projects give agents their own computer (filesystem, terminal, browser, sometimes a full desktop). The focus is on what a local, Windows-first desktop app (Orole OS: Go daemon + named-pipe IPC + React UI) can copy. Research date is 2026-10-15. Anything older is labelled with its date. Vendor claims are marked as such. Two primary pages could not be fetched (HTTP 403): OpenAI's "Building Codex Windows sandbox" post and the MarkTechPost Aug-2026 benchmark. For those, the notes rely on secondary summaries and say so.

---

## 1. Cloud agent sandboxes: isolation technology and SDK shape (E2B, Daytona, Modal, Northflank, Vercel, Cloudflare, Fly, Runloop, Morph)

### Takeaway
The cloud market has settled on one SDK shape: `create(template/snapshot) -> commands.run / process.exec (+PTY) -> files.read/write/watch -> expose port URL -> pause/snapshot/kill`. Under that shape, the hosted products split by isolation technology:
- Hardware-virtualized microVMs: E2B, Vercel, Runloop, Fly Sprites, Morph, and Cursor's backend.
- gVisor: Modal by default.
- Containers with optional Kata/Sysbox: Daytona.
- Kata/Cloud Hypervisor or gVisor: Northflank.
- Containers on Workers/Durable Objects: Cloudflare.

The products now compete on snapshot/fork/pause semantics and cold-start time. Independent measurements diverge sharply from vendor claims.

### Cited Findings

**E2B (Firecracker; open source)**
- Each sandbox is one Firecracker microVM "in its own cgroup and network namespace," with a per-sandbox nftables egress firewall. The components are: API (REST control plane: lifecycle, placement, auth, quotas), Orchestrator (per node; drives Firecracker create/pause/resume/kill/checkpoint; gRPC from the API), Template manager (builds templates from Docker images), Client proxy (edge router that routes sandbox URLs to nodes and **auto-resumes on traffic**), and **envd** (runs inside every VM and exposes process, PTY, filesystem, file-watcher and port-forwarding APIs "over Connect RPC and REST"). License Apache-2.0. It can be self-hosted as a single KVM Linux host (`docker compose`) or with Terraform for GCP/AWS/Azure. — [e2b-dev/infra README](https://github.com/e2b-dev/infra)
- Pause saves filesystem + memory, so running processes survive. `mode: 'filesystem'` saves disk only, and the sandbox reboots on resume. Connecting to a paused sandbox auto-resumes it. Network clients must reconnect after resume. "A paused sandbox is kept indefinitely." Pause takes ~4 s per GiB RAM, and resume takes ~1 s. `onTimeout: 'pause'` gives auto-pause. If a memory pause is refused for ~2 min past the timeout, the sandbox falls back to a filesystem-only pause. Continuous runtime limits are 24 h (Pro) and 1 h (Hobby), reset by pause/resume. — [E2B persistence docs](https://docs.e2b.dev/sandbox/persistence)
- Snapshots capture filesystem + memory. One snapshot can seed many new sandboxes (fork). They require envd ≥ v0.5.0. — [E2B snapshots docs](https://docs.e2b.dev/sandbox/snapshots)
- Failure mode reported by a third-party E2B-compatible reimplementation (wrenn): envd (a Go binary) crashed with "fatal error: bad summary data" after Firecracker snapshot/restore, because vCPUs froze mid-allocation. Their fix was a pre-pause endpoint to quiesce goroutines and force GC, plus restarting the port subsystem on restore. They also use Firecracker Diff snapshots (dirty pages only, chained up to 10 generations) and UFFD lazy memory restore. These are not E2B internals. — [search summary of wrenn/sandbox commits](https://git.omukk.dev/wrenn/sandbox/pulls/2)
- E2B Desktop Sandbox: an open-source "secure virtual desktop ready for Computer Use" on Linux + **Xfce**. `stream.start()` streams the whole desktop or a single window (one stream at a time). `require_auth=True` produces an auth key embedded in the stream URL, and `get_url(view_only=True)` gives a view-only URL. The SDK covers `screenshot`, `left_click/double_click/right_click/middle_click/scroll/move_mouse/drag`, `write`, `press(["ctrl","c"])`, window enumeration, `launch/open`, `files.write`, and `commands.run`. Apache-2.0. Examples include "Open Computer Use" (open-source LLMs) and "Surf" (OpenAI CUA on E2B). The README does not name the streaming protocol. — [e2b-dev/desktop](https://github.com/e2b-dev/desktop)
- Pricing (Jan 2026 snapshot): $0.000014/vCPU/s + $0.0000045/GiB/s, which is ≈ $0.0828/h for 1 vCPU/2 GB. Cold start "~150 ms" (vendor-style figure). — [Superagent comparison, Jan 2026](https://www.superagent.sh/blog/ai-code-sandbox-benchmark-2026)

**Daytona**
- Third-party comparisons say Daytona uses Docker by default, with optional Kata or Sysbox for stronger isolation. Docker-by-default means weaker isolation unless you configure Kata. — [search summary of 2026 comparisons](https://www.startuphub.ai/ai-news/artificial-intelligence/2026/daytona-vs-e2b-vs-modal-vs-vercel-sandbox-2026); not confirmed in Daytona's official docs.
- VNC + Computer Use: VNC is the human view and Computer Use is the programmatic API (mouse, keyboard, screenshot, display). `sandbox.computerUse.start()` is used in TS. A human-in-the-loop takeover via VNC from the dashboard is documented. — [Daytona VNC docs](https://www.daytona.io/docs/vnc-access/), [Computer Use Linux](https://www.daytona.io/docs/ja/computer-use-linux)
- Windows computer-use sandboxes run "an actual Windows operating system inside an isolated VM." Availability is marketing/early access only, with no Windows SDK reference docs found. — [Daytona Windows sandboxes post](https://www.daytona.io/dotfiles/windows-sandboxes)
- The SDK exposes `sandbox.process.exec(...)` (TS `executeCommand`), `sandbox.fs.upload_file/download_file`, and Git ops. Snapshots are built from image definitions (Python/TS/Ruby/Go/Java SDKs, CLI, API). A third-party source says you cannot snapshot a running sandbox's live state. — [Daytona snapshots docs](https://www.daytona.io/docs/en/snapshots.md), [Daytona SDK docs](https://daytona.io/docs/sdk)
- Cold start is marketed as sub-90 ms (27 ms optimized). An August 2026 independent run reportedly measured a 0.27 s median but only a **37 % success rate**. — [MarkTechPost, 2026-08-27, via search snippet; page returned 403](https://www.marktechpost.com/2026/08/27/best-agent-sandboxes-2026-cold-start-pricing-network-policy/)

**Modal**
- The default runtime is gVisor. "VM Sandboxes" run on a full VM with a real Linux kernel (e.g. for Docker) but are allowlist-only, and only filesystem snapshots work there. — [Modal VM Sandboxes guide](https://modal.com/docs/guide/vm-sandboxes)
- Memory snapshots (`_experimental_snapshot()` / `_experimental_from_snapshot`) are "early preview, pre-beta" for enabled customers. Live TCP streams reset after restore. Retention: filesystem/directory snapshots 30 days, memory snapshots 7 days. — [Modal sandbox memory snapshots](https://modal.com/docs/guide/sandbox-memory-snapshots)
- Modal is the only one of these vendors where a sandbox can hold a GPU. A secondary source says it offers outbound block/allowlists, Connect Tokens, tunnels, and inbound CIDR controls. — [search summary](https://www.marktechpost.com/2026/08/27/best-agent-sandboxes-2026-cold-start-pricing-network-policy/)
- Pricing ≈ $0.1193/h normalized (per physical core = 2 vCPU). — [Superagent, Jan 2026](https://www.superagent.sh/blog/ai-code-sandbox-benchmark-2026)

**Vercel Sandbox**
- `Sandbox.create()` provisions a Firecracker microVM with a dedicated kernel, private filesystem, and network namespace. Runtimes are node22/24/26 and python3.13, with root. By default data is lost on stop. "Persistent sandboxes" auto-save filesystem state on stop and restore on resume, with snapshot retention 30 days after last use. — [Vercel Sandbox concepts](https://vercel.com/docs/vercel-sandbox/concepts), [pricing](https://vercel.com/docs/sandbox/pricing)
- Limits: max duration 45 min (Hobby) / 5 h (Pro/Ent). Concurrency 10 / 2,000. Up to 8 vCPU/16 GB (Pro), 32 vCPU/64 GB (Ent), 32 GB disk. Pricing: Active CPU $0.128/h and $0.60 per 1M creations. Vercel's own pricing pages conflict on memory price and on max open ports (15 vs 4). — [Vercel pricing](https://vercel.com/docs/sandbox/pricing), [examples.vercel.com copy](https://examples.vercel.com/docs/vercel-sandbox/pricing)
- In the Aug-2026 independent run it had the fastest measured median (0.67 s, 100 % success). — [MarkTechPost via search snippet](https://www.marktechpost.com/2026/08/27/best-agent-sandboxes-2026-cold-start-pricing-network-policy/)

**Cloudflare Sandbox SDK / Containers**
- Containers and the Sandbox SDK went GA in April 2026. — [Cloudflare changelog 2026-04-13](https://developers.cloudflare.com/changelog/2026-04-13-containers-sandbox-ga)
- The SDK is a TS API with these parts:
  - Commands: exec with streaming output, background processes, and sessions, where each session has its own shell state, env and cwd.
  - Files: CRUD, plus inotify-based watch.
  - Bucket mounts: R2/S3/GCS mounted as a filesystem.
  - Ports: exposed via preview URLs, or `sandbox.tunnels.get(port)` for `*.trycloudflare.com`.
  - Wiring: a Worker plus a Durable Object binding per sandbox.

  — [Cloudflare Sandbox API](https://developers.cloudflare.com/sandbox/api)
- Feb 2026: `createBackup()` / `restoreBackup()` give point-in-time directory snapshots, restored as copy-on-write overlays and stored in R2. Backups can be forked across sandboxes and given TTLs. — [Cloudflare changelog 2026-02-23](https://developers.cloudflare.com/changelog/post/2026-02-23-sandbox-backup-restore-api/index.md)
- It was the slowest in the Aug-2026 run (~5 s median), and the Jan-2026 comparison cites 2–3 s. Pricing ≈ $0.09/h normalized + $5/month base plan. — [MarkTechPost via snippet](https://www.marktechpost.com/2026/08/27/best-agent-sandboxes-2026-cold-start-pricing-network-policy/); [Superagent](https://www.superagent.sh/blog/ai-code-sandbox-benchmark-2026)

**Northflank**
- Every workload runs in a microVM using Kata Containers (Cloud Hypervisor primary; Firecracker/gVisor depending on workload). It is deployable in the customer's VPC (BYOC), and GPU workloads default to gVisor. These are vendor claims. — [Northflank sandboxes](https://northflank.com/product/sandboxes), [Kata explainer](https://northflank.com/blog/what-are-kata-containers)

**Fly.io Sprites (and Machines)**
- Sprites are "full Linux computers built for agents." Checkpoints are copy-on-write, block-level, and capture the whole filesystem in ~1 s (landing page: ~300 ms), taken while the Sprite runs and backed up to object storage. Restore takes "well under a second" but **rewinds files, not running processes**. Auto-checkpoints can be taken on idle. — [Fly Sprites checkpoint/restore](https://fly.io/sprites/checkpoint-restore.md), [Sprites](https://fly.io/sprites.md)
- Reliability: Apr–May 2026 community threads report restores failing with "failed to start overlay service tree", Sprites resetting to empty, and checkpoints that could not be listed. — [Fly community thread 1](https://community.fly.io/t/unrecoverable-sprites-after-checkpoint-restore-failed-to-start-overlay-service-tree/27918), [thread 2](https://community.fly.io/t/sprites-become-unusable-when-agents-checkpoint/27678)

**Runloop**
- Devboxes are "hardware-isolated microVMs." Blueprints are build-once environment definitions cached with Docker layers. Snapshots are disk-only ("currently only disk snapshots are supported"). The documented pattern is to snapshot, fan out 3 devboxes, and keep the best. — [Runloop snapshots docs](https://docs.runloop.ai/docs/devboxes/snapshots), [blueprints](https://docs.runloop.ai/docs/blueprints)

**Morph Cloud**
- "Infinibranch": snapshot, branch, and restore entire VMs in <250 ms (vendor claim). Every instance starts from a snapshot ID. Devboxes add SSH, tmux, preview URLs, and scale-to-zero. It targets RL environments and test-time scaling. — [Morph Cloud developers](https://cloud.morph.so/developers), [Morph concepts](https://cloud.morph.so/docs/concepts)

**Cross-vendor cold-start reality check (Aug 2026, independent run)**
- Vercel 0.67 s median (100 %), Modal 0.88 s (100 %), E2B 1.61 s, Daytona 0.27 s (37 % success), Cloudflare ~5 s. The vendor-quoted figures are ~150 ms (E2B) and ~90 ms (Daytona). The gap likely reflects "time to ready" vs "time to interactive." — [MarkTechPost via search snippet; direct fetch 403](https://www.marktechpost.com/2026/08/27/best-agent-sandboxes-2026-cold-start-pricing-network-policy/)

### Inferences
- The E2B architecture (control-plane API → per-host orchestrator → in-guest daemon over Connect RPC, plus an auto-resuming proxy) is the reference design. It maps directly onto Orole's daemon → sandbox-backend → in-guest agent layering.
- Memory snapshots of Go/managed runtimes need a quiesce hook (see the wrenn envd bug). Any Orole in-guest agent written in Go should expose a "prepare-for-pause" RPC if Orole ever does memory snapshots.
- Products have converged on "disk snapshot is cheap and reliable; memory snapshot is premium and fragile" (Runloop disk-only, Fly files-only, Modal memory pre-beta, E2B memory default with filesystem fallback). A local app should start with disk/workspace snapshots.

### Gaps
- Exact current (Oct 2026) E2B, Daytona and Modal list prices. The figures above are from Jan 2026.
- Whether Daytona now defaults to a VM runtime. Official isolation docs were not found.
- Cloudflare's container isolation tech (VM vs container) was not confirmed from primary docs.
- Fly Machines (as distinct from Sprites) was not researched in detail.

---

## 2. Agent products: how each gives the agent "its own computer"

### Takeaway
Hosted coding agents (Codex cloud, Claude Code cloud, Cursor, Copilot, Jules, Devin, Manus) use an ephemeral VM or container per task or session, seeded from a cached or snapshotted environment. A setup phase runs with network access, then the agent phase runs with an egress allowlist, and credentials are held outside the VM and injected by a proxy.

Local agents (Codex CLI, Claude Code, Docker Sandboxes) use OS primitives instead:
- Seatbelt on macOS.
- bubblewrap plus network-namespace removal on Linux.
- On Windows: restricted tokens, a separate sandbox user, ACLs, and firewall/WFP rules. As of October 2026, Microsoft's MXC is the newest option.

Computer-use reference stacks are Linux + Xvfb/Xfce + VNC/noVNC.

### Cited Findings

**OpenAI Codex: local CLI / desktop app on Windows (the most relevant precedent)**
- There are three Windows sandbox implementations, chosen via `windows.sandbox`:
  - `mxc`: recommended on compatible devices. Native process isolation, no extra accounts or firewall rules, no admin.
  - `elevated`: "preferred legacy fallback." Needs admin setup. Uses dedicated lower-privilege sandbox users, filesystem boundaries, firewall rules, and local policy changes.
  - `unelevated`: legacy fallback. Uses a restricted token derived from the user, ACL boundaries, and environment-level offline controls. Network isolation is weaker, and it can't deny read paths.

  Preference order is MXC → elevated → unelevated. — [OpenAI Codex Windows sandbox docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md)
- Both legacy modes use a **private desktop** by default for UI isolation. IT can pre-provision elevated mode with `codex sandbox setup --elevated --user 'DOMAIN\alice' ...`. MXC needs Windows 11 24H2 (26100.9278) or 25H2 (26200.9278) with MXC capabilities, and allows loopback to host services. **Detached child processes stop when the foreground command exits**, so dev servers may not survive. Windows 10 is best effort, and 1809+ is required for ConPTY. Secrets live in `CODEX_HOME/.sandbox-secrets/`. — [OpenAI Codex Windows sandbox docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md)
- Elevated architecture, per a third-party summary of OpenAI's 13 May 2026 engineering post (the original returned 403). There are four layers:
  - `codex.exe`: unelevated.
  - `codex-sandbox-setup.exe`: UAC once. Creates users and firewall rules and grants ACEs.
  - `codex-command-runner.exe`: long-lived elevated helper. Accepts `SpawnRequest` over a framed named pipe, validates it against policy, builds a restricted token, and calls `CreateProcessAsUserW`.
  - The child process.

  The two users are `CodexSandboxOffline` (firewall deny-all-outbound) and `CodexSandboxOnline`. `.git`, `.codex` and `.agents` get explicit deny-write ACEs. `audit_everyone_writable` warns about Everyone-writable folders. Paths are normalized through junctions, symlinks and `\\?\` before ACL operations. DPAPI-protected user secrets are unreadable by the sandbox users.

  Rejected prototype: a synthetic `sandbox-write` SID with a write-restricted token. Per-command ACL stamping was slow, re-walking the filesystem on policy changes was costly, and the network block was weak without elevation.

  Field errors: 1385 (policy strips "Log on locally"), 5 (antivirus intercepts `CreateProcessAsUserW`), and `CreateRestrictedToken failed: 87`. — [Daniel Vaughan summary, 2026-05-14 (updated 2026-10-14)](https://codex.danielvaughan.com/2026/05/14/codex-cli-windows-sandbox-engineering-restricted-tokens-acls-elevated-architecture/); original [OpenAI post](https://openai.com/index/building-codex-windows-sandbox/) (403)
- On Oct 9 2026 OpenAI said the MXC-based mode offers faster setup, stronger network enforcement and more granular file control. The ChatGPT desktop app picks MXC automatically for consumer accounts. — [METAL lab news](https://metallab.ai/en/2026/10/openai-codex-windows-mxc-sandbox); [OpenAI docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md)
- Codex also layers a separate "guardian" LLM that scores each tool call 0–100 and blocks calls above 80, per an arXiv source-code study (secondary). — [arXiv 2609.00006](https://arxiv.org/pdf/2609.00006)

**OpenAI Codex cloud**
- Per task, Codex:
  1. creates a container and checks out the repo at a branch/SHA;
  2. runs the setup script, or a maintenance script when resuming a cached container;
  3. applies internet settings, since setup has internet but the agent is offline by default, with "limited" or "unrestricted" options;
  4. runs the agent loop through terminal commands.

  Container state is cached for **up to 12 hours**. The cache is invalidated when setup scripts, env vars or secrets change, and Business/Enterprise caches are shared across users. — [OpenAI Codex cloud environments](https://developers.openai.com/codex/cloud/environments)
- A third-party guide says secrets are available to setup scripts but **removed before the agent phase**. — [Vaughan, 2026-05-31](https://codex.danielvaughan.com/2026/05/31/codex-cloud-environments-setup-scripts-caching-secrets-codex-universal/) (not confirmed in official docs)

**ChatGPT agent / Operator "own computer"**
- The virtual computer provides a visual browser (screenshots and clicks), a text browser, a terminal and API access. "Takeover" lets the user drive the browser for logins and sensitive input. Connectors are read-only. Monitors watch actions in real time. These details come from secondary sources only. Availability per OpenAI help docs was reportedly contradictory as of 2026-08-18. — [CometAPI explainer](https://www.cometapi.com/agent-mode-in-chatgpt-architecture-feature/), [Chatbase](https://chatbase.co/blog/chatgpt-agent-mode)

**Manus**
- Manus runs one full virtual computer per task on E2B Firecracker microVMs. The machine has a Chromium browser, a terminal and a filesystem. Manus needs pause/resume for user input, credentials and CAPTCHAs, and paid users keep sandbox data for up to 14 days. Manus tried Docker first. These are E2B marketing claims. — [E2B blog: How Manus uses E2B](https://www.e2b.dev/blog/how-manus-uses-e2b-to-provide-agents-with-virtual-computers), [Manus sandbox page](https://www.manus.im/blog/manus-sandbox)
- Manus is listed as an "announced" adopter of Microsoft MXC (local containment). — [Windows Developer Blog, 2026-10-07](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/)

**Anthropic computer-use reference implementation**
- It is a single Docker container: the agent loop runs inside the container it controls. A Streamlit chat runs on :8501, a combined UI on :8080, noVNC on :6080 (`/vnc.html`) and raw VNC on :5900. `WIDTH`/`HEIGHT` env vars set the screen, and 1024×768 (XGA) is recommended because larger screens are downscaled and lose accuracy. The latest tool is `computer_toolset_20260801`, a toolset with one member tool per action and no beta header. Earlier versions run `computer_20241022` through `computer_20251124`. The demo "supports one session at a time and must be reset between sessions." Security advice: a dedicated VM/container, keep logins away from the model, allowlist domains, require human confirmation for consequential actions, and expect prompt injection. — [anthropics/claude-quickstarts computer-use-demo](https://github.com/anthropics/claude-quickstarts/tree/main/computer-use-demo)

**Claude Code (local)**
- The Bash sandbox runs on macOS (Seatbelt), Linux and WSL2 (bubblewrap + socat). **"On native Windows, Claude Code runs commands unsandboxed."** With `failIfUnavailable`, Claude Code exits on Windows. — [Claude Code sandboxing docs](https://code.claude.com/docs/en/sandboxing)
- The sandbox covers shell commands only. Built-in file/web tools, hooks, local MCP servers, LSP servers and the status line run outside it. Escape hatches are `excludedCommands` and the `dangerouslyDisableSandbox` retry, which can be disabled with `allowUnsandboxedCommands: false`. Network goes through a host proxy via `HTTP(S)_PROXY`/`ALL_PROXY`. Tools that ignore proxy env vars (ssh, DB drivers, UDP) can't connect. The proxy refuses hostnames resolving to loopback, link-local or metadata addresses. — [Claude Code sandboxing docs](https://code.claude.com/docs/en/sandboxing)
- Credential masking: sandboxed commands see a per-session **sentinel**, and the proxy swaps in the real value only toward `injectHosts` and only when `tlsTerminate` is on. AWS SigV4 is re-signed by the proxy. Masking is honored only from user/managed settings, never from repo settings. — [Claude Code sandboxing docs](https://code.claude.com/docs/en/sandboxing)
- Documented limitations: the proxy doesn't inspect TLS by default, so domain fronting can bypass allowlists ("Allowing broad domains such as github.com can create paths for data exfiltration"). Allowing a Unix socket like `docker.sock` is a full bypass. `enableWeakerNestedSandbox` weakens isolation inside Docker. `docker` itself is incompatible with the sandbox. Computer use runs on the real desktop, not in a sandbox. — [Claude Code sandboxing docs](https://code.claude.com/docs/en/sandboxing)
- Claude Code is "announced" as an MXC adopter (not shipped as of 2026-10-07). — [Windows Developer Blog](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/)

**Claude Code cloud sessions**
- Each session runs in an isolated, Anthropic-managed VM, or in a self-hosted environment. Network is limited by default. Git credentials and signing keys stay outside the sandbox, and a **GitHub proxy** authenticates with scoped credentials. "Network secrets" are attached to matching requests after they leave the VM. The VM is reclaimed after inactivity: on reopen, conversation history is restored but running background processes are not. Setup scripts are cached only if they finish in ~5 min. — [Claude Code cloud docs](https://code.claude.com/docs/en/claude-code-on-the-web)
- Windows-specific leak: when bundling a local repo for the cloud, "Native Windows: uncommitted changes to tracked files upload as they are, whatever the file's name." On macOS/Linux/WSL, `.env`, `*.tfvars`, `id_rsa` and `*.pem` edits are excluded. — [Claude Code cloud docs](https://code.claude.com/docs/en/claude-code-on-the-web)

**Devin**
- "Every Devin session requires a new VM." The DevBox (shell, editor, browser) can run in the customer's VPC while the "brain" stays in Cognition's tenant. On startup a websocket connects to an isolated container in the Devin tenant. Recommended hosts are AWS i3 bare metal and Azure Lasv3. Newer docs say every session boots from a frozen snapshot image, changes don't persist back, and each org has one active snapshot. — [Devin VPC overview](https://docs.devin.ai/enterprise/vpc/overview.md), [Devin environment docs](https://docs.devin.ai/onboard-devin/environment)
- In Devin Outposts, each session runs on an E2B Firecracker microVM, so pause/resume is built in. — [E2B blog: Devin Outposts](https://e2b.dev/blog/devin-outposts)

**OpenHands**
- The classic runtime is client–server over Docker. OpenHands builds a derived image containing an **action execution server**. The backend ships each Action over REST to that server and receives an Observation, on an event-stream architecture. Pluggable runtimes include Docker, Remote, Modal, Daytona, Kubernetes and Local (no isolation). — [OpenHands runtime docs](https://docs.openhands.dev/usage/architecture/runtime)
- The V1 Software Agent SDK has four packages: SDK, Tools, Workspace and Server. `LocalWorkspace` becomes `DockerWorkspace`/`RemoteWorkspace` with no agent-code change. The **agent-server** is FastAPI + WebSocket with endpoints for conversations, bash, files, events, desktop and VSCode, plus per-user sessions and API-key auth. Images bundle VS Code Web, a VNC desktop and a persistent Chromium for human inspection. The minimal targets drop VSCode/VNC. — [OpenHands SDK arch overview](https://docs.openhands.dev/sdk/arch/overview), [arXiv 2511.03690 / MLSys 2026](https://arxiv.org/abs/2511.03690v1), [custom sandbox guide](https://docs.openhands.dev/openhands/usage/advanced/custom-sandbox-guide)

**Cursor cloud (background) agents**
- Each agent gets its own VM on Firecracker-based microVM infrastructure, in a separate AWS account. Environments are configured via agent-led setup, a saved snapshot, or a Dockerfile in `.cursor/environment.json`. Snapshots make later starts faster. The agent records videos and screenshots, and the user can **control the agent's remote desktop**. `/in-cloud` spawns subagents in their own VMs and branches. — [Cursor cloud agent docs](https://cursor.com/docs/cloud-agent), [security overview](https://cursor.com/docs/cloud-agent/security), [agent computer use blog](https://cursor.com/blog/agent-computer-use)
- Failure mode: secrets present during install (e.g. an `.npmrc` token) get frozen into the snapshot. — [Infisical blog](https://infisical.com/blog/secure-secrets-management-for-cursor-cloud-agents)

**GitHub Copilot coding agent**
- It runs in a GitHub Actions runner. `.github/workflows/copilot-setup-steps.yml` must contain a job named `copilot-setup-steps`, only takes effect from the default branch, and honors only a subset of job keys. Larger runners are available via `runs-on`. Setup steps run **before** the firewall applies. The agent firewall is strict by default, blocked hosts are reported in the PR, and the allowlist is set via a repo `copilot` environment variable. Sources disagree on the variable name (`COPILOT_AGENT_FIREWALL_ALLOW_LIST` vs `..._ADDITIONS`). — [GitHub docs](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/customize-the-agent-environment), [MS .NET DevBlog](https://devblogs.microsoft.com/dotnet/maui-team-copilot-tips/), [zenn.dev](https://zenn.dev/tmyymmt/articles/42810886c02ab5)
- GitHub Copilot already supports MXC locally. — [Windows Developer Blog](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/)

**Google Jules**
- Secondary sources say Jules clones the repo into a dedicated Google Cloud VM per task, can reuse a saved "Environment Snapshot," and the VM has network access for installs. — [IT Brief](https://itbrief.news/story/google-launches-jules-globally-an-ai-coding-agent-for-github), [gocodeo](https://www.gocodeo.com/post/jules-inside-googles-asynchronous-coding-agent)

### Inferences
- The dominant hosted pattern is the template: setup phase (network on, secrets available) → snapshot/cache → agent phase (network allowlisted, secrets absent, proxy-injected credentials) → PR/diff out. Orole can reproduce it locally: daemon-owned "environment recipe" + workspace snapshot + allowlisting proxy + Credential Manager-backed injection.
- Codex's Windows journey (unelevated restricted token → elevated two-user + firewall + named-pipe runner → MXC) is a roadmap Orole can shortcut. Target MXC first, keep a Codex-style elevated fallback for Windows 10 or older Windows 11 builds, and never rely on the unelevated mode for network isolation.
- Codex's named-pipe `SpawnRequest` runner is almost identical to Orole's daemon IPC design. Orole's daemon could own a "sandboxed spawn" handler with the same request shape (cmd, args, cwd, env, fs profile, network profile, target identity).

### Gaps
- OpenAI's original Windows sandbox post could not be fetched (403), so some details come from a single third-party summary.
- ChatGPT agent's actual infra (VM tech, streaming protocol), and whether Operator still exists separately in Oct 2026, are not verified from primary sources.
- Google Jules official docs were not retrieved. Its VM tech is unknown.
- Copilot coding agent runner isolation (VM per job is standard for GitHub-hosted runners) is not confirmed from Copilot-specific docs in this session.

---

## 3. Local-first projects and building blocks (Windows relevance first)

### Takeaway
For Windows 10/11 in October 2026, the usable local options are:
- **Microsoft MXC** (GA 2026-10-07, MIT; AppContainer process containers, session containers, WSL containers, experimental microVM/Hyperlight/Windows Sandbox).
- **Docker Sandboxes (`sbx`)**: its own VMM on the Windows Hypervisor Platform. Local use is free.
- **Anthropic sandbox-runtime**: alpha Windows path with a dedicated user, WFP and ACLs.
- **Codex's open-source Windows sandbox**.
- **Windows Sandbox**: disposable Hyper-V desktop, single instance, Pro+ editions.
- **Cua**: Windows Sandbox and QEMU providers plus an in-guest computer-server.
- **microsandbox**: libkrun; WHP support claimed but inconsistent across sources.

For browsers: Playwright MCP and Chrome DevTools MCP locally, Browserbase/Steel/Browser-Use cloud over CDP.

### Cited Findings

**Microsoft Execution Containers (MXC)**
- GA on 2026-10-07 (Windows 365 support also GA). Backends:
  - Process container: Windows 11/macOS/Linux, using **AppContainer**/Seatbelt/Bubblewrap.
  - Session container: Windows 11 only. A separate Windows account and session with its own desktop, clipboard, UI, input and local agent identity.
  - WSL container (WSLc): Windows 11.
  - MicroVM: Windows 11/Linux, experimental, hardware-backed.

  Policy areas: process, filesystem (writable / read-only / no-access), network (in/out, loopback to host services) and UI. Modes: Enforcement; Learning (blocks and writes a JSON activity report); Permissive (allows and records). Activity reports exist only for Windows process containers. Intune and Entra agent identity are "coming soon." Already supported by GitHub Copilot, OpenClaw, OpenAI Codex, Replit, LM Studio, Unsloth and NVIDIA OpenShell. Announced for Claude Code, Manus, Perplexity, Raycast, Simular and others. No performance figures were published. — [Windows Developer Blog, 2026-10-07](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/)
- README details:
  - Windows backends: `processcontainer` (default), `windows_sandbox`, `wslc`, `microvm`, `hyperlight`, `isolation_session`. Windows Sandbox, MicroVM and Hyperlight are experimental.
  - Linux: `bubblewrap` (default), `lxc`, `microvm`, `hyperlight`. macOS: `seatbelt`.
  - Versioned JSON container-request schema.
  - SDKs: Rust `mxc-sdk` (crates.io), .NET `Microsoft.Mxc.Sdk` (NuGet), Node `@microsoft/mxc-sdk` (npm).
  - Lifecycle ops: provision/start/exec/stop/deprovision for persistent containers.
  - **Non-SDK use via platform executors such as `wxc-exec.exe` that accept JSON requests.**
  - `--audit` disables all sandboxing. Expect access-denied errors until rules are tuned. Telemetry is off by default. MIT license.

  — [microsoft/mxc README](https://github.com/microsoft/mxc)
- Caveat: an earlier preview README warned that generated policies were "overly permissive" and that "no MXC profiles should be treated as security boundaries currently." It is unclear whether this still holds at GA. — [search summary quoting the repo](https://www.dsebastien.net/microsoft-execution-container-mxc/); [The Stack, 2026-10-09](https://www.thestack.technology/microsoft-takes-mxc-its-agent-management-layer-ga/)

**Docker Sandboxes (`sbx`)**
- Each agent session runs in a dedicated microVM with its own kernel and a **private Docker daemon** (full build/run/compose, no socket mount). Docker built its own VMM rather than use Firecracker ("no native support for macOS or Windows, full stop"). It runs on Hypervisor.framework (macOS), **Windows Hypervisor Platform** (Windows) and KVM (Linux). "Credentials are injected at runtime and outside the MicroVM boundary." Supported agents: Claude Code, Codex, OpenCode, Copilot, Gemini CLI, Kiro, Docker Agent, OpenClaw, NanoClaw. Install on Windows with `winget install Docker.sbx`. **No Docker Desktop license required**. Startup is described only as "near-instant." — [Docker blog, 2026-04-16](https://www.docker.com/blog/why-microvms-the-architecture-behind-docker-sandboxes/)
- Workspace: **filesystem passthrough** (virtiofs, cache on by default), not sync, so the host's absolute paths are preserved. "Clone mode" mounts the repo read-only at `/run/sandbox/source` and the agent works in a private clone. Network: all outbound TCP goes through a host-side proxy (forward proxy for HTTP/S, transparent for other TCP), which follows the OS system proxy or PAC/SOCKS5 upstream. MCP: one host-side MCP gateway with its own policies, and local stdio MCP servers run **on the host**. Credentials: the agent sees a **sentinel**, and the host proxy swaps in the real value after the policy check. Lifecycle: stop/start keeps the VM, packages and images, and the sandbox persists until `sbx rm`. Disk grows over time. — [Docker Sandboxes architecture docs](https://docs.docker.com/ai/sandboxes/architecture/)
- Local `sbx` CLI and compute are free including commercial use. Cloud sandboxes are pay-as-you-go with an API/SDK, and org-wide policy management is paid. Kits package reusable environments. Editors connect over SSH. GPU passthrough is supported. — [Docker Sandboxes docs](https://docs.docker.com/ai/sandboxes.md)
- Launched January 2026, microVM on macOS/Windows. Claude guide labelled experimental and requires Docker Desktop 4.50+ (older guide). — [Docker Claude Code guide](https://docs.docker.com/ai/sandboxes/claude-code)

**Docker MCP Gateway**
- Runs MCP servers in isolated containers with restricted privileges, network and resources, starting them on demand when a tool is called. Secrets come from Docker Desktop's secrets API (or a `.env`) and are injected by the gateway. Interceptors use `--interceptor before:exec:/bin/path`. Tool allowlists use `server.tool` notation. Built-in call tracing. — [docker mcp gateway run reference](https://docs.docker.com/reference/cli/docker/mcp/gateway/gateway_run), [Docker MCP Gateway docs](https://docs.docker.com/ai/mcp-gateway/)

**Anthropic sandbox-runtime (`srt`)**
- npm `@anthropic-ai/sandbox-runtime`, Apache-2.0, "Beta Research Preview" (releases up to ~v0.0.79). Platforms: macOS (`sandbox-exec` + generated Seatbelt profiles), Linux (bubblewrap with no network namespace, socat, optional seccomp for Unix sockets) and **Windows (alpha)**. — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)
- Windows design (alpha):
  - `npx @anthropic-ai/sandbox-runtime windows-install` (one UAC prompt) provisions a `srt-sandbox` local user (password DPAPI-encrypted in `HKLM\SOFTWARE\sandbox-runtime`), a `sandbox-runtime-users` group, and a machine-wide **WFP** filter pair at `FWPM_LAYER_ALE_AUTH_CONNECT_V4/V6`. The pair permits loopback to the proxy port range (default 60080–60089) and blocks any connect whose token carries the `srt-sandbox` SID.
  - Launch: `srt-win.exe` does a two-hop launch. `CreateProcessWithLogonW` starts a runner as `srt-sandbox`, and the runner spawns the target under a **restricted token inside a job object**.
  - Filesystem: additive inheriting NTFS ACEs for the sandbox SID only, removed on `reset()`.
  - Known limitations: DNS still resolves via the Dnscache service; schannel revocation checks fail (`CRYPT_E_REVOCATION_OFFLINE`); per-user installs (nvm, Scoop, `pip --user`) are unreadable by the sandbox account; grants are session-wide; the proxy auth token is visible on the command line.

  — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)
- Network design on all OSes: an HTTP proxy for domain filtering plus SOCKS5 for other TCP. Default deny. `deniedDomains` takes precedence. Resolved-address checks block loopback, link-local, metadata and host interfaces. Experimental `tlsTerminate` MITM. Reads are deny-then-allow, writes are allow-only. — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)

**Windows Sandbox**
- Hyper-V–isolated disposable Windows desktop with a separate kernel. Everything is discarded on close. Available on Pro/Enterprise/Education as an optional feature. **Only one instance can run at a time.** Networking is on by default, and Microsoft advises disabling it and mapping folders read-only via config. Software must be re-provisioned each session. Not infallible when clipboard, mapped folders, vGPU or networking are enabled. — [Microsoft Learn: Windows Sandbox](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/), [woshub guide](https://woshub.com/how-to-use-windows-sandbox/)

**Cua (trycua)**
- Providers in the older `Computer` API: `lume` (macOS VMs on Apple Virtualization.framework, Apple Silicon), `windows_sandbox` (`os_type="windows"`, `provider_type="windows_sandbox"`, with `cua-computer-server` installed), `docker`, and cloud. — [Cua quickstart](https://cua.ai/quickstart-cli), [trycua/cua](https://github.com/trycua/cua)
- The newer `Sandbox` API: `Sandbox.ephemeral(Image.linux())`, `.windows()`, `.macos()`, `.android()`. `Image.windows()` defaults to `ghcr.io/trycua/windows:2022`, a containerDisk booted as a QEMU VM, UEFI-only. Windows 11 has no canonical image and is installed locally from an evaluation ISO. It accepts qcow2/vhdx/raw/iso. The guest control server is **computer-server on :8000** (or the OSWorld Flask server on :5000). — [Cua QEMU Windows reference](https://cua.ai/docs/cua/reference/desktop-sandbox/qemu-container/windows)
- computer-server is the in-guest HTTP + WebSocket server: REST for commands, screenshots and files, and WebSocket for real-time streaming. The XFCE container `trycua/cua-xfce` (Ubuntu 22.04) exposes TigerVNC :5901, noVNC :6901 and API :8000, defaulting to 1024×768. — [Cua computer-server](https://cua.ai/computer-sdk/computer-server), [Cua XFCE container](https://cua.ai/docs/cua/reference/desktop-sandbox/linux-container/xfce)
- "Cua Driver" does background computer use on macOS and Windows without stealing cursor or focus, and is exposed as an MCP server. — [trycua/cua](https://github.com/trycua/cua); [bytenote summary](https://bytenote.net/article/cua-computer-use-agent-infra) (secondary)

**microsandbox**
- Self-hosted libkrun microVMs with OCI images. SDKs in Rust/Python/TS/Go, Apache-2.0, an MCP server, pre-1.0. Windows support is described inconsistently: one source says WSL2, another says native WHP. DEVELOPMENT.md documents a native MSVC build, but guest and kernel artifacts need a Linux build backend (Docker Desktop or Ubuntu WSL). Boot is ~320 ms on bare-metal KVM (own benchmark). — [microsandbox GitHub](https://github.com/superradcompany/microsandbox), [DEVELOPMENT.md](https://cdn.jsdelivr.net/gh/superradcompany/microsandbox@main/DEVELOPMENT.md), [rywalker research](https://rywalker.com/research/microsandbox)

**container-use (Dagger)**
- An MCP server + CLI (`container-use stdio`, alias `cu`). Each agent gets a fresh container in its own git branch, and review happens by checking out the branch. Supports Claude Code, Cursor, Goose, Codex and Amazon Q. Early development. — [Dagger blog](https://dagger.io/blog/agent-container-use), [container-use README](https://cdn.jsdelivr.net/gh/dagger/container-use@main/README.md)

**Browsers**
- Playwright MCP reads pages via accessibility snapshots (no vision model needed, ~200–400 tokens each). Profile handling:
  - Persistent by default, stored in the `ms-playwright` cache dir.
  - `--isolated` gives an ephemeral profile, which can be seeded with `--storage-state`.
  - `--extension` attaches to the user's running Chrome/Edge via the "Playwright MCP Bridge" extension, using existing logins.
  - It can also connect over CDP (e.g. `localhost:9222`).

  — [Playwright MCP docs](https://playwright.dev/docs/getting-started-mcp), [Playwright MCP concepts](https://www.mintlify.com/microsoft/playwright-mcp/concepts/browser-automation)
- Chrome DevTools MCP is Puppeteer/CDP-based (~26 tools, ~18k tokens of tool context per a third party). Modes are a dedicated profile, an isolated instance, or `--autoConnect` to a running Chrome (M144+). It is strongest for console, network and performance work. — [vibebrowser comparison](https://www.vibebrowser.app/blog/chrome-devtools-mcp-vs-playwright-mcp), [trackingplan comparison](https://www.trackingplan.com/blog/chrome-devtools-mcp-vs-playwright-mcp-digital-analysts) (secondary)
- Browserbase: one isolated Chromium per session via the Sessions API. Clients connect over **CDP** and get a live view, replay and log trace. Nothing persists unless you use **Contexts** (cookies, storage and tokens saved and reused; session cookies are now kept). Stagehand v4 takes a browser from a factory: Browserbase, local launch, or attach to an existing Chromium via CDP. keep-alive requires the Startup plan. — [Browserbase blog](https://browserbase.com/blog/what-is-a-browserbase-browser), [contexts template](https://browserbase.com/templates/context), [Stagehand v4 browser config](https://docs.stagehand.dev/v4/configuration/browser)
- Steel: CDP connect URL, a hosted session viewer with human takeover, **WebRTC live viewer + HLS replay**, persistent contexts, proxies, CAPTCHA solving, and sessions of up to 24 h. — [Steel: Stagehand with Steel](https://llms.steel.dev/articles/stagehand-with-steel/), [MemberJunction Steel provider](https://docs.memberjunction.org/packages/ai/remotebrowser/providers/steel/)
- Browser-Use: `Browser(use_cloud=True)` provisions a cloud browser, with `cloud_profile_id` for saved cloud profiles. Free sessions cap at 15 min and paid at 240 min. It can attach to any remote Chrome via a CDP URL. — [Browser-Use remote browser docs](https://docs.browser-use.com/open-source/customize/browser/remote.md)

### Inferences
- For Orole, the quickest credible local backend on Windows 11 is MXC. It is MIT-licensed and policy-JSON driven. A Go daemon can drive it by shelling out to `wxc-exec.exe` with JSON, avoiding the need for Rust/.NET/Node bindings (to be verified). The **session container** is the only first-party primitive giving an agent its own desktop/clipboard/input on the same machine. That is a natural fit for "agent has its own computer" without a full VM.
- For full Linux toolchains plus Docker-in-sandbox, Docker `sbx` is the most mature local microVM on Windows (WHP) and is free locally. Orole could integrate it as an optional backend rather than build a VMM.
- The sandbox-runtime and Codex-elevated Windows recipes (dedicated user + WFP/firewall keyed on SID + additive ACEs + restricted token + job object + launch broker over a named pipe) can be reimplemented in Go with `golang.org/x/sys/windows`. They make the fallback for Windows 10 and pre-24H2 Windows 11, where MXC is unavailable.
- Windows Sandbox's single-instance limit rules it out for parallel agents. It is only viable as a "one disposable Windows desktop" mode.

### Gaps
- MXC process-container internals beyond "AppContainer," minimum Windows 11 build per backend, startup latency, and whether `wxc-exec.exe` ships with Windows or only with the SDK.
- Daytona self-hosted license and current Windows support; Steel's open-source self-host (`steel-browser`) license; OpenInterpreter's current sandboxing story. None of these were researched in this session.
- microsandbox native-Windows status is conflicting (WSL2 vs WHP).

---

## 4. Common protocols for exposing a sandbox to agents and humans

### Takeaway
Every serious system puts a small **in-guest daemon** inside the sandbox, speaking RPC/REST plus WebSocket or streaming for process, PTY, files and ports: E2B envd (Connect RPC + REST), Cua computer-server (REST + WebSocket), OpenHands action-execution-server/agent-server (REST + WebSocket), and Codex's command-runner (named-pipe framed IPC, host-side). Browsers are exposed over CDP. Desktops are exposed to humans over VNC/noVNC (Anthropic demo, Cua, Daytona, OpenHands) or WebRTC (Steel). MCP sits on top as the agent-facing tool layer, often brokered by a host-side gateway.

### Cited Findings
- E2B envd exposes processes, PTYs, filesystem operations, file watchers and port forwarding over Connect RPC and REST. The client proxy routes `sandbox-id` URLs and auto-resumes paused sandboxes on traffic. — [e2b-dev/infra](https://github.com/e2b-dev/infra)
- Cua computer-server: HTTP REST (commands, screenshots, files) + WebSocket (continuous interaction) on :8000 inside each VM or container. — [Cua computer-server](https://cua.ai/computer-sdk/computer-server)
- OpenHands agent-server: REST + WebSocket for conversations, bash, files, events, desktop and VSCode, with API-key auth. Classic runtime: Action → REST → Observation. — [OpenHands SDK arch](https://docs.openhands.dev/sdk/arch/overview), [OpenHands runtime](https://docs.openhands.dev/usage/architecture/runtime)
- Codex elevated: the host-side `codex-command-runner.exe` receives `SpawnRequest` (cmd, args, target user, permission profile, env, cwd) over a framed named-pipe IPC. — [Vaughan summary](https://codex.danielvaughan.com/2026/05/14/codex-cli-windows-sandbox-engineering-restricted-tokens-acls-elevated-architecture/)
- Devin: the DevBox opens a websocket to an isolated container in the Devin tenant (outbound-initiated, so it works from a customer VPC). — [Devin VPC overview](https://docs.devin.ai/enterprise/vpc/overview.md)
- Human live view:
  - Anthropic demo: x11vnc :5900 + noVNC :6080. — [computer-use-demo](https://github.com/anthropics/claude-quickstarts/tree/main/computer-use-demo)
  - Cua: TigerVNC :5901 / noVNC :6901. — [Cua XFCE](https://cua.ai/docs/cua/reference/desktop-sandbox/linux-container/xfce)
  - E2B Desktop: an authenticated or view-only stream URL. — [e2b-dev/desktop](https://github.com/e2b-dev/desktop)
  - Daytona: VNC plus a Computer Use API. — [Daytona VNC](https://www.daytona.io/docs/vnc-access/)
  - Steel: WebRTC viewer + HLS replay. — [Steel](https://llms.steel.dev/articles/stagehand-with-steel/)
  - Browserbase: live view + replay + screencast frames keyed by session ID. — [Browserbase](https://browserbase.com/blog/what-is-a-browserbase-browser)
  - Cursor: user takeover of the agent's remote desktop. — [Cursor computer use](https://cursor.com/blog/agent-computer-use)
- CDP is the universal browser attachment point: Browserbase, Steel, Browser-Use (`cdp_url`), Stagehand (attach existing Chromium), Playwright MCP (CDP endpoint) and Chrome DevTools MCP (`--autoConnect`). — sources in section 3
- MCP placement:
  - Docker `sbx`: one host-side MCP gateway; local stdio servers run on the host, not in the VM. — [Docker sbx architecture](https://docs.docker.com/ai/sandboxes/architecture/)
  - Docker MCP Gateway: containerizes each MCP server. — [Docker MCP Gateway](https://docs.docker.com/ai/mcp-gateway/)
  - container-use and microsandbox: expose the sandbox itself as an MCP server. — [container-use](https://dagger.io/blog/agent-container-use), [microsandbox](https://github.com/superradcompany/microsandbox)
  - Claude Code: local MCP servers run outside its sandbox. — [Claude Code sandboxing](https://code.claude.com/docs/en/sandboxing)
- Terminals on Windows need ConPTY (Windows 10 1809+). — [OpenAI Codex Windows docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md)

### Inferences
- Orole already has the right shape (daemon owns terminals and agents; GUI is stateless). The missing piece is a per-sandbox in-guest agent, or for host-process containers a host-side broker like Codex's runner. It would speak the same typed IPC and stream PTY/stdout to the daemon. The React UI then renders a noVNC (or WebRTC) view for desktop sandboxes and an xterm view for PTYs, both proxied via the daemon. The UI must never connect directly to a listening port, which is consistent with Orole's no-network-listener rule. This implies VNC/CDP ports bound inside the guest or on loopback only and tunneled over the daemon's pipe.
- MCP-inside-the-sandbox vs MCP-on-host is a real design choice. Docker and Claude Code keep MCP on the host (outside the boundary), while container-use/microsandbox expose the sandbox as an MCP tool. For Orole, the daemon (which already proxies MCP) should stay the broker, and sandboxed tools should run inside the boundary.

### Gaps
- E2B Desktop's streaming protocol (VNC vs other) is not stated in its README.
- No primary source was found on ChatGPT agent's or Manus's human live-view transport.

---

## 5. Lessons learned and failure modes (cost, cold starts, persistence, secrets, prompt injection, egress, logged-in browsers)

### Takeaway
The recurring failures are:
1. **Secrets leaking into the sandbox or its snapshots**. The industry answer is sentinel values swapped in by a host-side proxy.
2. **Egress allowlists that are bypassable** (domain fronting, DNS still resolving, tools ignoring proxy env vars).
3. **Snapshot/restore fragility**, especially with memory state.
4. **Vendor cold-start claims** that don't survive independent measurement.
5. **Prompt injection via browsed content**, which turns logged-in browser sessions into cross-origin data-theft vectors. No vendor claims it is solved.

### Cited Findings
- **Secrets / credentials**
  - Docker `sbx`: the agent sees a sentinel, and the host proxy substitutes the real credential after the policy check. "The real credential never enters the sandbox." — [Docker sbx architecture](https://docs.docker.com/ai/sandboxes/architecture/)
  - Claude Code sandbox: sentinel + proxy substitution only toward `injectHosts`, which requires TLS termination. Mask rules are honored only from user/managed settings, not repo files (anti-injection). — [Claude Code sandboxing](https://code.claude.com/docs/en/sandboxing)
  - Claude Code cloud: GitHub credentials "never enter a session's VM," and a GitHub proxy attaches them server-side. — [Claude Code cloud](https://code.claude.com/docs/en/claude-code-on-the-web)
  - Codex cloud: secrets are reportedly removed before the agent phase. — [Vaughan](https://codex.danielvaughan.com/2026/05/31/codex-cloud-environments-setup-scripts-caching-secrets-codex-universal/)
  - Codex Windows elevated: DPAPI secrets are bound to the user profile, so sandbox users can't read them. — [Vaughan](https://codex.danielvaughan.com/2026/05/14/codex-cli-windows-sandbox-engineering-restricted-tokens-acls-elevated-architecture/)
  - Cursor: `.npmrc` tokens present during install get frozen into snapshots. — [Infisical](https://infisical.com/blog/secure-secrets-management-for-cursor-cloud-agents)
  - Claude Code on native Windows uploads uncommitted changes to tracked files, including `.env`, when bundling for the cloud. — [Claude Code cloud](https://code.claude.com/docs/en/claude-code-on-the-web)
- **Egress control**
  - Claude Code: the proxy decides on the client-supplied hostname without TLS inspection, so domain fronting can bypass it. Broad domains such as github.com are exfil paths. Allowing `docker.sock` is a full escape. — [Claude Code sandboxing](https://code.claude.com/docs/en/sandboxing)
  - srt on Windows: DNS resolves via Dnscache even when `connect()` is blocked, which is a potential DNS exfil channel. — [sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime)
  - Codex unelevated: the network block is "environment-level" (proxy env + stub executables) and therefore weak. — [OpenAI docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md)
  - Claude Code cloud: with network disabled, the Anthropic API is still reachable, "which may allow data to exit the VM." — [Claude Code cloud](https://code.claude.com/docs/en/claude-code-on-the-web)
  - Copilot: setup steps run before the firewall, so dependencies must be fetched there. — [GitHub docs](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/customize-the-agent-environment)
- **Windows-specific breakage**
  - Codex: error 1385 (policy removes "Log on locally" from sandbox users), error 5 (antivirus blocks `CreateProcessAsUserW`), `CreateRestrictedToken failed: 87`, Everyone-writable folders bypass ACL boundaries, and ACL stamping is slow on big trees. — [Vaughan](https://codex.danielvaughan.com/2026/05/14/codex-cli-windows-sandbox-engineering-restricted-tokens-acls-elevated-architecture/)
  - srt: per-user tool installs (nvm, Scoop, `pip --user`) are unreadable by a separate sandbox account, and schannel revocation checks fail offline. — [sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime)
  - Codex MXC: detached child processes (dev servers) die when the foreground command exits. — [OpenAI docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md)
- **State persistence / snapshots**
  - Fly Sprites restores failing ("overlay service tree") and Sprites resetting to empty, Apr–May 2026. — [Fly community](https://community.fly.io/t/unrecoverable-sprites-after-checkpoint-restore-failed-to-start-overlay-service-tree/27918)
  - Firecracker memory restore crashing a Go guest daemon. — [wrenn](https://git.omukk.dev/wrenn/sandbox/pulls/2)
  - Network connections drop across pause/resume on E2B and Modal. — [E2B](https://docs.e2b.dev/sandbox/persistence), [Modal](https://modal.com/docs/guide/sandbox-memory-snapshots)
  - Claude Code cloud VMs are reclaimed on idle and background processes are lost. — [Claude Code cloud](https://code.claude.com/docs/en/claude-code-on-the-web)
  - The Anthropic computer-use demo is single-session and needs a reset between sessions. — [computer-use-demo](https://github.com/anthropics/claude-quickstarts/tree/main/computer-use-demo)
  - Docker `sbx` disk use grows over time. — [Docker sbx architecture](https://docs.docker.com/ai/sandboxes/architecture/)
- **Cold starts / cost**
  - Daytona's measured 37 % success rate vs its sub-90 ms marketing, E2B 1.61 s measured vs ~150 ms claimed, Cloudflare ~5 s. — [MarkTechPost Aug 2026 via snippet](https://www.marktechpost.com/2026/08/27/best-agent-sandboxes-2026-cold-start-pricing-network-policy/)
  - E2B pause cost is ~4 s/GiB RAM. — [E2B](https://docs.e2b.dev/sandbox/persistence)
  - Typical cloud cost is ≈ $0.08–0.15 per vCPU-hour-equivalent (Jan 2026). — [Superagent](https://www.superagent.sh/blog/ai-code-sandbox-benchmark-2026)
- **Prompt injection via browsed content / logged-in browsers**
  - Anthropic's Claude for Chrome launch tests (Aug 2025, older): 123 cases, 29 scenarios; attack success 23.6 % without mitigations and 11.2 % with them in autonomous mode; four browser-specific attacks went from 35.7 % to 0 %. — [Simon Willison on Claude for Chrome, 2025-08-26](https://feeds.simonwillison.net/2025/Aug/26/piloting-claude-for-chrome)
  - A later Anthropic post (Claude Opus 4.5 era) reports ~1 % under an adaptive best-of-N attacker and says 1 % "still represents meaningful risk." — [Anthropic prompt-injection defenses](https://anthropic.com/research/prompt-injection-defenses)
  - UW study (Jan–Feb 2026): in agentic browsers the same-origin policy "is reduced to the strength of an agent's defenses against prompt injections." Researchers demonstrated cross-origin data theft on ChatGPT Atlas Agent Mode, and the preconditions exist for Chrome+Gemini, Claude for Chrome and Comet. — [UW Agentic Browsers and the SOP](https://agent-security.cs.washington.edu/agentic_browsers_sop.html)
  - Brave disclosed "unseeable prompt injections in screenshots" against Comet on 2025-10-21 (older). — [PPC Land](https://ppc.land/comet-browser-faces-multiple-security-vulnerabilities-from-prompt-injection/)
  - Mitigation patterns:
    - Takeover mode: the user types credentials themselves. — [Chatbase](https://chatbase.co/blog/chatgpt-agent-mode)
    - Persisted contexts instead of sharing passwords (Browserbase Contexts). — [Browserbase contexts](https://browserbase.com/templates/context)
    - Isolated Playwright profiles vs extension mode with real logins. — [Playwright MCP](https://www.mintlify.com/microsoft/playwright-mcp/concepts/browser-automation)
    - Keep logins away from the model and require human confirmation for consequential actions. — [computer-use-demo](https://github.com/anthropics/claude-quickstarts/tree/main/computer-use-demo)
    - Monitor or guardian models (OpenAI ChatGPT agent monitors; Codex guardian). — [CometAPI](https://www.cometapi.com/agent-mode-in-chatgpt-architecture-feature/), [arXiv 2609.00006](https://arxiv.org/pdf/2609.00006)

### Inferences
These are concrete design rules for Orole, drawn from the findings above.
1. Credentials stay in Windows Credential Manager. Sandboxes get sentinels, and a daemon-owned loopback proxy (reachable only from the sandbox SID/container) substitutes them per host. This already matches Orole's "secrets never in SQLite, logs or the frontend" rule.
2. Default-deny egress with an explicit allowlist enforced at the OS layer (WFP/MXC network policy), not just via proxy env vars. Block metadata, link-local and LAN ranges. Treat DNS as a leak channel.
3. Run setup with the network on, then snapshot, then run the agent with the network restricted. Never snapshot while secrets are present.
4. Logged-in browsing uses a dedicated, agent-only profile (never the user's main Chrome) plus human takeover for logins. Consequential actions need confirmation decided on the Go side.
5. Expose "Learning mode" activity reports (MXC) in the UI so users can tune policy instead of disabling sandboxing.
6. Say clearly in the UI which sandbox state survives what (daemon restart, reboot, VM reclaim), matching the existing AGENTS.md rule for terminals.

### Gaps
- No 2026 independent attack-success rates specific to logged-in sessions in Atlas or Comet were found.
- No published startup-latency numbers exist for MXC containers or Docker `sbx` microVMs on Windows. Orole would need to benchmark them.
- No primary source covers the cost or overhead of WFP-per-SID filtering or ACL stamping at scale on Windows.

---

## 6. What a local, Windows-first desktop app (Orole OS) could copy: synthesis matrix

### Takeaway
No single product gives a Windows desktop app a ready-made local "own computer." The composable stack as of October 2026 has four tiers:

| Tier | Mechanism | Best for | Fallback / notes |
|---|---|---|---|
| (a) Lightweight | MXC process container (AppContainer) | Shell/tool commands | Codex/srt-style restricted-token + separate-user + WFP for Windows 10 and older Windows 11 |
| (b) Desktop | MXC session container | Agent's own Windows desktop, clipboard and input on the same machine | Windows Sandbox for a single disposable desktop |
| (c) Linux | Docker `sbx`, MXC WSLc, or microsandbox | Linux-heavy / Docker workloads | All are microVM- or WSL-backed |
| (d) Browser | Dedicated Chromium driven over CDP (Playwright/Chrome DevTools MCP) | Agent web browsing | Profile isolation and human takeover |

Across all tiers: a daemon-side broker, an in-guest daemon protocol in the style of envd/computer-server, a sentinel-credential proxy, and noVNC/xterm live views.

### Cited Findings

| Pattern | Who does it | Windows-local applicability | Source |
|---|---|---|---|
| Policy-JSON containment SDK (process/session/WSL/microVM) with Learning mode | Microsoft MXC (GA 2026-10-07, MIT) | Native; Win11 24H2/25H2 builds per Codex docs; Rust/.NET/Node SDK or `wxc-exec.exe` JSON | [Windows Dev Blog](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/), [microsoft/mxc](https://github.com/microsoft/mxc), [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md) |
| Separate sandbox user + firewall/WFP keyed on SID + ACEs + restricted token + named-pipe spawn broker | Codex elevated; Anthropic srt (alpha) | Native Win10/11; one-time UAC; field errors 1385/5/87 | [Vaughan](https://codex.danielvaughan.com/2026/05/14/codex-cli-windows-sandbox-engineering-restricted-tokens-acls-elevated-architecture/), [srt](https://github.com/anthropic-experimental/sandbox-runtime) |
| Private desktop for UI isolation | Codex legacy modes | Native (Win32 desktops) | [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox.md) |
| Local microVM with private Docker daemon, virtiofs passthrough, host proxy, sentinel creds | Docker Sandboxes | Native via WHP; free locally | [Docker blog](https://www.docker.com/blog/why-microvms-the-architecture-behind-docker-sandboxes/), [sbx architecture](https://docs.docker.com/ai/sandboxes/architecture/) |
| Disposable Hyper-V Windows desktop | Windows Sandbox; Cua `windows_sandbox` provider | Native; Pro/Ent/Edu; single instance | [MS Learn](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/), [Cua quickstart](https://cua.ai/quickstart-cli) |
| In-guest daemon (process/PTY/files/ports) over RPC + auto-resume proxy | E2B envd/client-proxy (Apache-2.0) | Pattern; envd is Linux-guest | [e2b-dev/infra](https://github.com/e2b-dev/infra) |
| In-guest computer-server (REST + WS) + noVNC | Cua; Anthropic demo; OpenHands agent-server | Pattern; Cua runs Windows guests via QEMU | [Cua](https://cua.ai/computer-sdk/computer-server), [OpenHands](https://docs.openhands.dev/sdk/arch/overview) |
| Branch-per-agent containers via MCP | container-use | Needs Docker/Dagger | [container-use](https://dagger.io/blog/agent-container-use) |
| Persistent/isolated/extension browser modes over CDP | Playwright MCP, Chrome DevTools MCP | Native | [Playwright MCP](https://playwright.dev/docs/getting-started-mcp) |
| Setup-then-lockdown + env cache (12 h) | Codex cloud; Copilot (setup before firewall) | Pattern | [Codex cloud](https://developers.openai.com/codex/cloud/environments), [GitHub](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/customize-the-agent-environment) |
| Disk snapshot + fan-out (best-of-N) | Runloop, Morph, E2B, Cloudflare backups | Pattern; locally via VHDX diff disks or git worktrees (inference) | [Runloop](https://docs.runloop.ai/docs/devboxes/snapshots), [Morph](https://cloud.morph.so/developers) |

### Inferences
- Recommended layering for Orole (inference):
  - The daemon gets a `sandbox` service with pluggable backends: `mxc` (default on capable Win11), `restricted-user` (Go reimplementation of the Codex/srt recipe, for Win10 and older Win11), `sbx` (optional Linux microVM if installed), and `windows-sandbox` (single disposable desktop).
  - Every backend takes one Orole policy object, translated to MXC JSON, WFP + ACLs, or sbx policy.
  - Live view and terminal streams are proxied over the existing authenticated named pipe.
- Persistence semantics to copy:
  - Per-task ephemeral sandboxes by default.
  - Named persistent sandboxes (like `sbx` and E2B pause) as opt-in.
  - Workspace via git worktree/clone mode rather than direct passthrough when the agent is untrusted (Docker's "clone mode" precedent).
- Don't build a VMM. Docker found Firecracker unusable on Windows and wrote its own on WHP. That is a large investment Orole should avoid by integrating MXC microVM (experimental) or sbx.

### Gaps
- Real-world MXC behavior on Windows 10 (presumably unsupported) and the exact capabilities rollout across Windows 11 SKUs.
- Whether a Go program can link MXC directly (no Go SDK is listed). The `wxc-exec.exe` path needs verification.
- Licensing and redistribution terms for bundling Docker `sbx` or Windows evaluation images (Cua) inside a third-party desktop app.
