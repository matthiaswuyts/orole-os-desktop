# AI coding agents and Nix/NixOS: evidence, tooling, failure modes, benefits (as of Oct 2026)

Scope note: research run 2026-10-15. Evidence quality is labelled throughout. **Measured** means a benchmark or controlled run. **Anecdote** means a blog post, a README or a forum report. Most of what exists is anecdote. The only Nix-specific measured evaluation found is the small, single-author NixBench harness. No source compares agent performance on NixOS with apt-based systems head to head.

## 1. Benchmarks / evaluations: are agents worse, equal or better on Nix vs apt-based systems?

### Takeaway
There is no published head-to-head measurement of agent success on NixOS versus Debian/Ubuntu. The one Nix-specific benchmark (NixBench, a single-author GitHub harness, not peer reviewed) shows frontier agents passing roughly 65–85% of small, deliberately tricky Nix repair tasks. Some task types fail across every model. Mainstream agent benchmarks such as Terminal-Bench run in Docker images and report no per-distro breakdown. Thin evidence either way.

### Cited Findings
- **NixBench** is a harness "for evaluating AI-written Nix code". Each task gives the agent a broken or incomplete Nix repo, and hidden evaluators grade the result against a content-hash-pinned nixpkgs. Starters evaluate cleanly and the defect only appears at runtime. Prompts describe symptoms, not the mechanism. "vm" tasks build and boot NixOS VM tests. Agents run in bubblewrap with a clean home. — [NixBench README](https://github.com/maximilianpw/nixbench)
- NixBench corpus 3.2.0 (21 tasks, 15 of them NixOS modules) has a calibration run dated 2026-10-04 that is still "pending". The README says "3.2.0 results are calibration evidence, not leaderboard claims". Configurations listed: Codex CLI with gpt-6.1-sol, gpt-6-luna and gpt-6-astra, and Claude Code with claude-opus-5-5 and claude-sonnet-5-5. No scores yet. — [NixBench README](https://github.com/maximilianpw/nixbench)
- **NixBench corpus 2.0 results (2026-06-24, single run per row, 240 s per-task timeout).** These are older than the current corpus. — [NixBench 2026-06-24 model comparison](https://github.com/maximilianpw/nixbench/blob/main/docs/runs/2026-06-24-model-comparison.md)
  - Best historical rows: gpt-5.4 high and gpt-5.5 xhigh at 22/26 (85%).
  - Claude Code with claude-opus-4-8: 19/26 at low effort, 21/26 at high and at xhigh.
  - On the 29-task set, the GPT-5.6 variants scored between 19/29 and 22/29 (about 66–76%).
  - Effort did not improve results monotonically. For example, gpt-5.5 scored 21 at low and 19 at medium and high.
  - The tasks `container-native-vs-oci`, `debug-network-false-lead` and `issue-report-quality` failed for all four xhigh/default baselines. For `debug-network-false-lead`, "No recorded row in this set passed this task".
  - `string-escaping-systemd` failed for gpt-5.4-mini and Opus 4.8.
- **Terminal-Bench 2.0** (ICLR 2026) has 89 tasks, each packaged with its own Docker environment. Frontier agents score below 65%. No source found breaks results down by OS or distro, and no NixOS-based tasks were found. — [Terminal-Bench paper (arXiv 2601.11868)](https://arxiv.org/abs/2601.11868v1); [Epoch AI Terminal-Bench page](https://epoch.ai/benchmarks/terminal-bench)
- **Anecdote, older (Sep 2025).** Claude Code issue #7954 says Opus's Nix/NixOS output was "most often wrong and hallucinated" when it generated a Rust flake.nix that Codex then critiqued. The thread itself shows Opus accepting a Codex review that contained errors. For example, Codex claimed cargo-tarpaulin is not packaged, but it is. Opus later retracted, calling the flake "well-written". The reporter closed the issue on 2025-11-01 as "no longer happening". — [anthropics/claude-code#7954](https://github.com/anthropics/claude-code/issues/7954)
- **Anecdote.** An HN commenter on the March 2026 Birkey piece said NixOS documentation is fragmented and often outdated, which limits how well agents can reason about Nix without grounding. — [Agent Wars summary of HN thread](https://agent-wars.com/news/2026-03-24-nixos-as-the-ideal-substrate-for-llm-coding-agents); [original article](https://www.birkey.co/2026-03-22-why-i-love-nixos.html)
- **Related study, not Nix-specific.** An arXiv study of dependency gaps in LLM-agent-generated code found that projects need on average 37 packages at runtime versus the 3 typically declared. This is the class of hidden dependency that hermetic Nix environments expose and apt-based hosts mask. — [arXiv 2512.22387](https://arxiv.org/pdf/2512.22387) (seen only as a search snippet, not read in full)

### Inferences
- Writing correct Nix is still error-prone for frontier models. A pass rate of roughly 15–35% failure on small NixBench tasks, with some task types failing universally, suggests subtle NixOS module and runtime semantics (systemd escaping, networking, OCI vs native containers) are the weak spot more than basic syntax.
- *Using* Nix (`nix shell`, `nix develop`, `nix run`) as a tool provider is a different skill from *authoring* NixOS modules. The anecdotes are positive on the former and mixed on the latter. A VM design where the agent consumes a pre-built Nix toolset, rather than editing system config, avoids most of the measured weakness.
- On a bare NixOS host with no hints, agents are plausibly worse at "install X and run it" tasks than on Debian, because their default reflexes (`apt`, `pip install`, downloaded binaries) fail. No source measures this; it is inferred from the failure-mode reports in section 4.

### Gaps
- No benchmark compares the same tasks on NixOS and Debian/Ubuntu VMs.
- No public measurement of whether mcp-nixos, skills or AGENTS.md hints improve pass rates. NixBench does not report a with/without-tools ablation.
- The NixBench 3.2 calibration results and the 2026-09-10 "Astra via isolated Pi" run were not read.
- NixBench model names are taken as written in the repo and were not independently verified.

## 2. Agent tooling for Nix (MCP servers, CLIs, vendor features)

### Takeaway
A mature tool ecosystem exists for grounding agents in real Nix data, and almost all of it is community-built. mcp-nixos is the de facto standard. devenv ships an official MCP server, Flox has an agent repo with an MCP server and skills plus a FLOX.md context-file pattern, and numtide/llm-agents.nix packages the agents themselves. None of these publishes a measured accuracy gain.

### Cited Findings
- **mcp-nixos** (utensils) is a Model Context Protocol (MCP) server whose tagline is "Because Your AI Assistant Shouldn't Hallucinate About Packages". — [mcp-nixos GitHub](https://github.com/utensils/mcp-nixos)
  - It consolidates 17 tools into 2. `nix` covers search, info, stats, browse, channels, flake-inputs, cache and store. `nix_versions` returns historical versions with nixpkgs commit hashes. The README says it uses about 1,030 tokens of context.
  - Data sources: search.nixos.org packages and options, Home Manager, nix-darwin, Nixvim, FlakeHub, Noogle, NixOS Wiki, nix.dev, NixHub and cache.nixos.org.
  - About 855 stars at the time of reading. No measured accuracy claims.
- **devenv** has a built-in `devenv mcp` server (stdio, or HTTP on port 8080) with `search_packages` (searches the nixpkgs input) and `search_options` (searches devenv options), plus a Claude Code setup guide. — [devenv MCP docs](https://devenv.sh/mcp/)
- **Flox**
  - The Sep 2025 blog post (older) describes agents using `flox search`, `flox show` and `flox install` driven by a `FLOX.md` context file. It gives anecdotal setup times: Python under a minute, C/C++ under two minutes, ComfyUI 4–8 minutes. It admits the agent "won't get everything exactly right the first time". No benchmarks. — [Flox blog](https://flox.dev/blog/next-level-agentic-development-with-flox/)
  - A `flox/flox-agentic` repo provides `flox-mcp`, so environment management goes through MCP tools without bash access, plus seven skills. This was seen only via a mirror and should be verified upstream. — [flox-agentic mirror](https://backiee.wasmer.app/https_git_codeproxy_net/flox/flox-agentic)
- **Agent packaging**
  - numtide `llm-agents.nix` packages coding agents and is updated daily. — [gittrend listing](https://gittrend.io/repo/numtide/llm-agents.nix)
  - `ryoppippi/nix-claude-code` provides an overlay of official Claude Code binaries. — [nix-claude-code](https://github.com/ryoppippi/nix-claude-code)
  - `kissgyorgy/coding-agents` provides Nix packages and skills for Claude Code, Gemini CLI, Pi and Codex. — [coding-agents](https://github.com/kissgyorgy/coding-agents)
  - Claude Code is in nixpkgs as an unfree package. — [NixOS search](https://search.nixos.org/packages?channel=unstable&show=claude-code&from=0&size=50&sort=relevance&type=packages&query=claude-code)
- **Skills and plugins**
  - A NixOS Discourse thread offers portable Nix/NixOS agent skills targeting Nix 2.35.2. A commenter there uses Nix mostly via devenv and "lets Claude Code do the work". — [Discourse: NixOS and Nix skill for LLMs](https://discourse.nixos.org/t/nixos-and-nix-skill-for-llms/80199)
  - A `nix-dev` Claude Code plugin bundles Nix knowledge with an LSP and mcp-nixos. — [skillselion listing](https://skillselion.com/skills/Jylhis/claude-marketplace/plugins/nix-dev)
  - `mcp-omnienv-nix` builds disposable `nix shell` toolchains and runs commands in them over MCP. — [glama listing](https://glama.ai/mcp/servers/@StealthBadger747/mcp-omnienv-nix/blob/03c262bf8f464e9b6ff468a3e09fcd9fed611d39/README.md)
  - `nixai` is a NixOS AI helper. — [Discourse: nixai](https://discourse.nixos.org/t/introducing-nixai-your-ai-powered-nixos-companion/65168)
- **Determinate Systems** publishes `llms.txt`, `llms-small.txt` and `llms-full.txt` documentation manifests for AI tools. No agent-specific product or blog post was found. — [determinate.systems/llms.txt](https://determinate.systems/llms.txt); [docs llms.txt](https://docs.determinate.systems/llms.txt)
- **Replit** uses Nix for its development environments. A March 2025 Tvix-store post (older) describes a large Nix store disk attached to containers. Replit's own docs say the Agent handles environment setup and dependencies, but do not describe how it drives Nix internally. The third-party claim of "tight integration" could not be verified. — [Replit infra blog](https://replit.com/blog/category/infra); [SCALE 22x talk](https://www.socallinuxexpo.org/scale/22x/presentations/tvix-store-production); [Replit Agent docs](https://docs.replit.com/replitai/agent)

### Inferences
- For an agent VM, the low-cost grounding stack is: mcp-nixos (or `devenv mcp`) for name and option lookup, `nix shell nixpkgs#x` or `nix run` for ad-hoc tools, and a short AGENTS.md or skill. Every piece of it is open source and already packaged.

### Gaps
- No usable sources were found on comma (`,`) or nix-index being used specifically by agents, though they are obvious candidates for "command not found" recovery.
- Nothing found on devbox/Jetify agent features.
- Nothing found on official Anthropic or OpenAI docs covering Nix setups.

## 3. Projects running AI agents in Nix-based sandboxes / VMs

### Takeaway
The leading pattern by 2026 is an ephemeral microvm.nix guest that shares the host's `/nix/store` read-only and gives the agent a writable workspace, with the agent run in no-prompt mode because the VM is the boundary. Several public projects implement it. Lighter alternatives (jail.nix, separate user, docker-nixuser) and a "NixOS host + Ubuntu container for mutability" hybrid also exist.

### Cited Findings
- **Michael Stapelberg (2026-02-01)** — [Coding Agent VMs on NixOS with microvm.nix](https://michael.stapelberg.ch/posts/2026-02-01-coding-agent-microvm-nix/)
  - Each VM runs on cloud-hypervisor with 8 vCPUs, 4 GB RAM and an 8 GB `/var`.
  - The host `/nix/store` is shared read-only over virtiofs, with a tmpfs store overlay so nothing persists. A NAT bridge provides networking.
  - Claude Code runs with `--dangerously-skip-permissions`.
  - A Claude skill generates new VM definitions: "Claude always got the VM creation correct". The skill builds but does not deploy.
  - "I can just throw away the VM and start over." Updating the host updates the VMs.
- **nixbox (razvanz)** is a microvm.nix sandbox on Cloud Hypervisor that isolates agents behind the KVM boundary plus egress filtering, with up to 64 concurrent VMs on slot-based networking. — [nixbox](https://github.com/razvanz/nixbox)
- **Keystone** documents agent MicroVMs as a lightweight, ephemeral boundary that lets agents run with full autonomy. — [Keystone agent-microvms](https://ncrmro.github.io/keystone/agent-microvms.html)
- **devctrl blog** calls microvm.nix "maximum security confinement" and recommends jail.nix when microVMs are too heavy or slow to start. — [devctrl blog](https://devctrl.blog/posts/maximum-security-confinement-for-your-ai-agents-with-microvm-nix/)
- **kohane27/agents-in-microvm-nix** is a wrapper that mounts per-run directories, for Claude Code or OpenCode. — [microvm search results](https://daily.dev/posts/coding-agent-vms-on-nixos-with-microvm-nix-srrlmefyj) (repo not read directly)
- **Hermes Agent (Nous Research)** ships two NixOS modes. — [Hermes Nix setup docs](https://hermes-agent.nousresearch.com/docs/getting-started/nix-setup)
  - "Native" mode is a hardened systemd service (NoNewPrivileges, ProtectSystem=strict). The agent can only use Nix-provided tools and "cannot install new packages at runtime".
  - "Container" mode is a persistent **Ubuntu** container with the Nix-built binary bind-mounted read-only. apt, pip and npm installs persist, and it is recommended for "Agents that need self-modification". Runtime installs are lost if the image, volumes or options change.
  - Nix/NixOS is Tier 2, best-effort support.
- **docker-nixuser** runs OpenCode on NixOS via Docker. — [grigio.org guide](https://grigio.org/vibe-coding-safely-the-ultimate-guide-to-ai-development-with-opencode-and-nixos-via-docker-nixuser/)
- **Separate-user sandboxing.** A Discourse user runs Claude Code under a separate restricted account: "not bulletproof, but forces me to always deliberately grant it access". — [Discourse: Packaging Claude Code on NixOS](https://discourse.nixos.org/t/packaging-claude-code-on-nixos/61072)

### Inferences
- The Hermes design is direct evidence that a Nix-native project still sees a need for an apt-based mutable layer, because agents want to install things at runtime. A hybrid (NixOS or Nix-built base plus a mutable escape hatch, or `nix shell` on demand) is a reasonable middle ground for agent VMs.

### Gaps
- No performance or startup-time numbers for microvm.nix agent VMs compared with Debian VMs were found.
- No production-scale (multi-tenant SaaS) write-up of NixOS agent VMs was found, other than Replit's general Nix infrastructure.

## 4. Reported failure modes and mitigations

### Takeaway
Reported failure modes:
- hallucinated package or attribute names and options
- precompiled binaries failing on NixOS because of FHS and dynamic-linker assumptions (including the agent's own binary)
- pip and compiled wheels failing
- agents using imperative `nix-env`
- slow flake evaluation hitting agent timeouts

Mitigations are well established: mcp-nixos grounding, AGENTS.md rules, a per-project shell.nix or devShell used via `nix-shell --run` or `nix develop -c`, nix-ld or buildFHSEnv, and pre-building. All the evidence is anecdotal.

### Cited Findings
- **Hallucinated names and options.** "AI models hallucinate Nix package names … a single wrong attribute path cascades into a broken build". Without verification, Codex "will guess attribute paths from training data". This is opinion, with no error rates given. — [Daniel Vaughan, Codex CLI + Nix (May 2026)](https://codex.danielvaughan.com/2026/05/30/codex-cli-nix-development-mcp-nixos-flakes-reproducible-agent-environments/); see also [claude-code#7954](https://github.com/anthropics/claude-code/issues/7954)
- **Other operational failures** from the same article: flake evaluation of heavy overlays exceeding Codex's default command timeout, and nvm/asdf switches dropping a globally installed Codex from PATH. — [Vaughan](https://codex.danielvaughan.com/2026/05/30/codex-cli-nix-development-mcp-nixos-flakes-reproducible-agent-environments/)
- **Mitigations from the same article** — [Vaughan](https://codex.danielvaughan.com/2026/05/30/codex-cli-nix-development-mcp-nixos-flakes-reproducible-agent-environments/)
  - AGENTS.md rules: "Never use `nix-env`", route packages through the devShell or overlay, pin nixpkgs in flake.lock, and run `nix flake check` before committing.
  - Use mcp-nixos for live data.
  - Pre-build with `nix build` before invoking the agent, or raise `--exec-timeout`.
- **FHS and dynamic linker.** The official Claude Code installer drops a prebuilt binary into `~/.local/bin` that does not run on NixOS because it expects a standard dynamic-linker path. Fixes are a patchelf shell.nix, an overlay, or the nixpkgs package. — [claude-nixos](https://github.com/ducks/claude-nixos); [Running Claude Code on NixOS (Goldsborough, 2026)](https://jakegoldsborough.com/blog/2026/running-claude-code-on-nixos/)
- **pip and C-extension wheels.** The NixOS Wiki says pip installs needing C libraries "will sometimes fail", with libstdc++.so.6 errors fixed by nix-ld, and suggests buildFHSEnv for a typical-Linux sandbox. — [NixOS Wiki: Python](https://nixos.wiki/wiki/Python)
- **nix-ld** provides a shim at the standard linker path, reading `NIX_LD_LIBRARY_PATH`, and is enabled via `programs.nix-ld`. — [running an external binary on NixOS](https://l-lin.github.io/nix/running-an-external-binary-on-NixOS)
- **Prompt-hint pattern.** Every project gets a shell.nix, Claude runs commands through `nix-shell --run`, and CLAUDE.md says so. On the NixOS server, `nixos-rebuild switch` is the whole deploy, which Claude can run. — [Goldsborough: Nix shells, SSH config and Claude Code](https://jakegoldsborough.com/blog/2026/nix-shells-ssh-claude-code/)
- **Mutation pressure.** Hermes forbids runtime pip, venv and `npm install` in native mode ("don't repair a Nix store path with pip"), and provides the Ubuntu container precisely for agents that need runtime installs. — [Hermes docs](https://hermes-agent.nousresearch.com/docs/getting-started/nix-setup)
- **Flox anecdote.** Claude Code missed a simpler way to change Python versions. — [Flox blog](https://flox.dev/blog/next-level-agentic-development-with-flox/)

### Inferences
- Most of the friction comes from the agent's defaults, not from Nix itself. A system prompt line ("this is NixOS; use `nix shell nixpkgs#pkg -c cmd` / `nix run`; never apt or nix-env; look names up with mcp-nixos"), together with nix-ld enabled, removes the bulk of the reported breakage.
- The likely payoff of grounding is high because the dominant error (a wrong attribute name) is cheap to check against a live index. This is unmeasured.

### Gaps
- No source found for an `apt install x` → nix wrapper or shim aimed at agents.
- No direct reports found of agents confusing flakes and channels, though it is plausible given that training data mixes both.
- No quantified failure rates for apt or pip attempts by agents on NixOS.

## 5. Benefits reported for agents on Nix

### Takeaway
The reported benefits are per-task reproducible toolchains without polluting the host, disposable VMs, file-based, reviewable and rollback-able system changes, and turning an agent session into a checkable flake artifact. These benefits are consistent across many anecdotes and have no controlled measurement behind them.

### Cited Findings
- **Birkey (March 2026).** The agent ran `nix develop` to get a Rust toolchain for a voice-to-text tool, leaving no `~/.cargo`, `~/.rustup` or PATH changes on the host. A session can be captured as `flake.nix` and checked with `nix flake check`. — [Birkey: Why I love NixOS](https://www.birkey.co/2026-03-22-why-i-love-nixos.html); [Agent Wars summary](https://agent-wars.com/news/2026-03-24-nixos-as-the-ideal-substrate-for-llm-coding-agents)
- **HN commenter alembic_fumes**: NixOS "may be the only OS where such reconfiguration is trustworthy at all". Changes are file-based, reviewable before they apply, and reversible with one rollback. The commenter contrasts this with letting an agent migrate an audio stack on mutable Ubuntu. — [Agent Wars summary of HN thread](https://agent-wars.com/news/2026-03-24-nixos-as-the-ideal-substrate-for-llm-coding-agents)
- **Ephemerality and maintenance.** "I can just throw away the VM and start over". Host updates propagate to the VMs. A read-only shared store keeps per-VM disk small. — [Stapelberg](https://michael.stapelberg.ch/posts/2026-02-01-coding-agent-microvm-nix/)
- **Pinned toolchains.** Inside a Nix-managed environment "every tool version, library path, and system dependency is pinned", so the agent's environment is identical across sessions and teammates. — [Vaughan, Codex CLI on NixOS (May 2026)](https://codex.danielvaughan.com/2026/05/13/codex-cli-nix-flakes-reproducible-agent-environments-declarative-toolchains/)
- **Declarative control.** Configuration lives in configuration.nix, secrets go through sops-nix or agenix, and CLI config commands are blocked to prevent drift. — [Hermes docs](https://hermes-agent.nousresearch.com/docs/getting-started/nix-setup)
- **Flox.** Conflicting dependency versions can coexist side by side, environments can be shared via FloxHub with `flox activate -r`, and agents can pin versions. — [Flox blog](https://flox.dev/blog/next-level-agentic-development-with-flox/)
- **Replit** runs Nix at scale for user and agent environments: a shared Nix store disk with a tvix-store cache, and a reported 90% storage-cost reduction (older, March 2025). This is infrastructure evidence, not evidence about agent accuracy. — [Replit infra blog](https://replit.com/blog/category/infra); [SCALE 22x talk](https://www.socallinuxexpo.org/scale/22x/presentations/tvix-store-production)

### Inferences
- For agent VMs specifically, Nix's strongest, best-supported advantages are:
  - a shared read-only store, which makes disposable VMs cheap and fast to provision
  - `nix shell` / `nix run`, which give per-task tools without mutating the base image
  - a declarative diff of anything the agent persists
  - generation rollback when an agent breaks system config
- These have to be weighed against the agent's weaker Nix authoring and its apt and pip reflexes. A good fit is a hybrid where the base image is NixOS-built and immutable, the agent gets `nix shell` plus nix-ld, and optionally a mutable Debian/Ubuntu container for runtime installs (the Hermes pattern).

### Gaps
- No quantitative data on time or token cost per task for Nix vs apt environments.
- No incident reports quantifying recovery via rollback after an agent broke a system.
- No data on cold-start latency when `nix shell` must fetch large closures, compared with apt.
