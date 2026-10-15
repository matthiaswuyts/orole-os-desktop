# Process-level (non-VM) sandboxing on Windows for AI-agent commands (state as of 2026-10-15)

Scope: how a Go desktop app on Windows 10/11 can confine agent-run processes (shell commands, scripts, a browser) to a private filesystem area with controlled network egress, without a VM. Developer target: Windows 11 Pro build 22000 (21H2). Primary sources were read directly where possible: Microsoft Learn, Chromium design docs, `openai/codex` source code, `microsoft/mxc` docs, `anthropic-experimental/sandbox-runtime` README, Project Zero. Third-party blogs are labelled as such.

**The biggest change in 2026:** Microsoft shipped **Microsoft Execution Containers (MXC)**. It is an MIT-licensed SDK for policy-driven agent containment, and it went generally available on 2026-10-07. Its strongest Windows tier uses a **new OS API (PSEC / `CreateProcessSecurityEnvironment`, "BaseContainer")**. That API applies per-container WFP egress filters and a per-container WinHTTP proxy **without admin or a UAC prompt for each launch**. It exists only on Windows 11 24H2/25H2 build 26100/26200.9278+ (August 2026 update) or newer. **Build 22000 does not have it.** On older builds MXC falls back to plain AppContainer plus DACL changes. Codex already uses MXC where it is available.

---

## Q1. AppContainer and LPAC: creating them from Go/Win32, filesystem ACL grants, network capabilities, loopback, what breaks, admin needs

### Takeaway
AppContainer and LPAC are the strongest *non-admin* process boundary on Windows, and both are available on build 22000. They do not need admin to create. They are default-deny for files and registry, and they have no network access unless you grant capabilities. They work badly for general developer tooling, for two reasons. First, every readable or writable path must be ACL-granted to the container SID, and that grant mutates host DACLs. Second, common shells and runtimes (`cmd`, PowerShell, `node`) fail on access checks against `C:\` and `\Device\Null`. Network control is all-or-nothing by capability (internet, private network). Per-destination filtering and "only reach my loopback proxy" need WFP or an exemption, and both are admin-gated unless you have the new 24H2+ PSEC API.

### Cited Findings
- `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES` takes a `SECURITY_CAPABILITIES` struct. "If this attribute is set the new process will be created as an AppContainer process." Supported since Windows 8. — [Microsoft Learn: UpdateProcThreadAttribute](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute)
- The same page documents `PROC_THREAD_ATTRIBUTE_CHILD_PROCESS_POLICY` (Win10+). `PROCESS_CREATION_CHILD_PROCESS_RESTRICTED` "is only effective in sandboxed applications (such as AppContainer) which ensure privileged process handles are not accessible". It also documents `PROC_THREAD_ATTRIBUTE_JOB_LIST` (Win10+) and the Win32k system-call-disable mitigation. — [Microsoft Learn: UpdateProcThreadAttribute](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute)
- Microsoft's AppContainer isolation pillars are credential, device, file, network, process and window isolation. On files: "Read-write access can be granted to specific persistent files and registry keys. Read-only access is less restricted." On input: "Some resources, such as keyboard and mouse, are always available to the AppContainer." — [Microsoft Learn: AppContainer isolation](https://learn.microsoft.com/en-us/windows/win32/secauthz/appcontainer-isolation)
- **Go bindings:** `golang.org/x/sys/windows` (master, checked 2026-10-15) has `NewProcThreadAttributeList`/`Update`, `CreateJobObject`, `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, all `JOB_OBJECT_UILIMIT_*` flags, `PROC_THREAD_ATTRIBUTE_MITIGATION_POLICY`, `PROC_THREAD_ATTRIBUTE_HANDLE_LIST`, `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` and `TokenIntegrityLevel`. It does **not** define `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES`, `SECURITY_CAPABILITIES`, `CreateAppContainerProfile` (userenv.dll) or `CreateRestrictedToken`. A Go app must declare these itself with `windows.NewLazySystemDLL`. — [golang/sys types_windows.go](https://github.com/golang/sys/blob/master/windows/types_windows.go) (grepped types/zsyscall/security files)
- **LPAC** (Win10 RS2 / 1703+): access is "granted by default to only those kernel, filesystem and registry objects" ACL'd for `ALL RESTRICTED APPLICATION PACKAGES` or the specific package SID. Opt-in capabilities include `lpacCom`, `registryRead`, `lpacWebPlatform` and `lpacClipboard`. ACLs "must be set by the installer, icacls, or a similar tool". — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- Chromium adds "Low Box" (AppContainer) attributes to its already restricted token. This "blocks network access unless INTERNET_CLIENT is present." — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- **What breaks.** MXC documents that `cmd.exe`, `powershell.exe`, `pwsh.exe` and `node.exe` call `GetFileAttributesW("C:\\")`, `_stat("C:\\")` or `GetAccessControl` at startup, and these "fail with `ERROR_ACCESS_DENIED` inside an AppContainer". The well-known AppContainer SIDs have no rights on the system-drive root. MXC fixes this with a one-time admin step: `wxc-host-prep prepare-system-drive` adds non-inheriting `FILE_READ_ATTRIBUTES|READ_EA|READ_CONTROL|SYNCHRONIZE` ACEs on `C:\` for `S-1-15-2-1` and `S-1-15-2-2`. — [MXC host-prep.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/host-prep.md)
- **`\Device\Null` breaks too.** It "resets to an AppContainer-hostile default at every boot". MXC needs an elevated `prepare-null-device` step on its AppContainer + DACL tier. — [MXC host-prep.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/host-prep.md)
- **MXC's three-tier Windows design:**
  - Tier 1 is BaseContainer/PSEC, an OS-native API.
  - Tier 2 is AppContainer + BFS (an OS-side filesystem broker / bind-filter helper).
  - Tier 3 is AppContainer + DACL mutation.
  - On Tier 3, denied paths are enforced by "host-filesystem DENY ACEs, applied before the run and removed on exit". This needs `WRITE_DAC` on each path and "temporarily modifies host security descriptors". Because the ACEs are keyed on the derived AppContainer SID, "two concurrent runs sharing the same `containerId` can revoke each other's ACEs".
  - `fallback.allowDaclMutation` gates Tier 3.

  — [MXC schema.md](https://github.com/microsoft/mxc/blob/main/docs/schema.md)
- **BaseContainer quirk:** tools that search upward for a project marker (for example `.git`) get `ACCESS_DENIED` when they reach a parent directory that is not in the allowlist. Granting a volume root (`C:\`) does not cascade to children. — [MXC schema.md](https://github.com/microsoft/mxc/blob/main/docs/schema.md)
- **Network capabilities:**
  - Windows exposes `privateNetworkClientServer` as one **bidirectional** capability, so private-network outbound cannot be allowed while private-network inbound is denied.
  - `internetClient` enables public outbound access.
  - With no capabilities, internet and private-network traffic are both denied.
  - "Both WFP filter writes and per-container WinHTTP proxy configuration require a privileged context." On hosts without PSEC, the AppContainer fallback "does not install directional WFP filters". It uses capabilities only, and runtime-proxy requests "do not fall back when AppContainer cannot preserve their peer or host-loopback requirements."

  — [MXC process-container networking.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/networking.md)
- **Firewall internals (Forshaw, 2021):**
  - The "InternetClient Default Rule" permits any AppContainer that holds `S-1-15-3-1` under the Public profile.
  - Because the Public profile treats all addresses alike, internetClient also reaches the LAN.
  - `privateNetworkClientServer` (`S-1-15-3-3`) only has filters for the Private profile.
  - A "Block Outbound Default Rule" catches everything else.

  — [Project Zero: Understanding Network Access in Windows AppContainers](https://projectzero.google/2021/08/understanding-network-access-windows-app.html)
- **Loopback:**
  - Loopback to non-AppContainer processes is blocked by an "AppContainerLoopback" filter in the receive/accept layer, so blocked connections *time out* rather than fail fast.
  - The documented exemption (`Add-AppModelLoopbackException` → `NetworkIsolationSetAppContainerConfig`) is **admin-only**.
  - Forshaw also describes an undocumented "generic loopback exemption capability", derived from the package SID, that works on Windows 8 through 11.

  — [Project Zero](https://projectzero.google/2021/08/understanding-network-access-windows-app.html)
- **DNS:** the DNS Client service accepts RPC from callers holding any of the three network capabilities, so AppContainers can resolve names without direct network access. — [Project Zero](https://projectzero.google/2021/08/understanding-network-access-windows-app.html)
- **Admin needs:** creating AppContainer processes needs no admin. The MXC AppContainer tiers do need *one-time* admin host prep (system-drive root ACEs, `\Device\Null` SD) for common shells to work. PSEC applies WFP "with no UAC prompt per launch". — [MXC host-prep.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/host-prep.md); [MXC networking.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/networking.md)

### Inferences
- On build 22000 a hand-rolled Go AppContainer launcher is feasible: `CreateAppContainerProfile` + `SECURITY_CAPABILITIES` + a job object, with declarations written by hand. It would deliver deny-by-default file access, but every tool and runtime directory (Git for Windows, Python, Node, `%USERPROFILE%` caches) needs an explicit ACL grant to the container SID. Those grants persist on the host and must be cleaned up.
- An AppContainer with no capabilities and no loopback exemption cannot even reach a host-side egress proxy. On 22000 the choices are: (a) admin-installed exemption or WFP rules, (b) the undocumented loopback capability trick (fragile), or (c) no network at all.
- AppContainer is attractive for an "offline compute" profile (scripts and builds with no network) on 22000. It is weak for a "proxy-mediated network" profile unless you accept an install-time admin step.

### Gaps
- No primary Microsoft page states the exact current behaviour of `CreateAppContainerProfile` on 21H2 versus 24H2. Its behaviour is long-standing, but it was not re-verified this session.
- Patch status of Forshaw's issue 2207 (default WFP permit rules for `dmcertinst.exe`/`omadmclient.exe`/`deviceenroller.exe` let AppContainer children reach the network) is unconfirmed. Microsoft marked it WontFix in 2021, and secondary coverage says Microsoft later reconsidered. — [Security Affairs](https://securityaffairs.com/posts/microsoft-wfp-appcontainer-bypass)
- I found no measured startup-cost numbers for AppContainer process creation or ACL application.

---

## Q2. Restricted tokens, integrity levels, job objects, desktop isolation, WFP, Windows Firewall per-program rules

### Takeaway
- **Restricted tokens:** write-restricted tokens with synthetic restricting SIDs (the Codex approach) give a no-admin **write** boundary that is compatible with real tools, because reads stay as the user. They provide no read confidentiality and no network control.
- **Low/Untrusted integrity:** a coarse "no write-up" defense-in-depth measure only.
- **Job objects:** no admin needed. They cover kill-on-close, process count, memory/CPU limits and UI restrictions.
- **Private desktops:** isolate UI and input.
- **WFP filters and Firewall rules:** the only built-in way to enforce per-process egress policy. **Both need admin to install by default.** Persistent filters scoped by user SID or AppContainer SID can be installed once, so later launches need no admin.

### Cited Findings
- **Codex's restricted token:**
  - Calls `CreateRestrictedToken` with `DISABLE_MAX_PRIVILEGE | LUA_TOKEN | WRITE_RESTRICTED`.
  - Restricting SIDs, in exact order: capability SIDs, then optionally the user SID, then the logon SID, then Everyone.
  - Sets the token's default DACL to the logon SID so launches cannot touch each other's processes or IPC, and enables only `SeChangeNotifyPrivilege`.

  — [openai/codex windows-sandbox-rs/src/token.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/token.rs)
- **Codex's capability SIDs:** random SIDs of the form `S-1-5-21-{rand}-{rand}-{rand}-{rand}`, persisted in `CODEX_HOME/cap_sid`. There is a workspace SID, a read-only SID, per-workspace (CWD) SIDs and per-write-root SIDs. Per-root SIDs are included "only when the root is currently allowed, so stale ACLs from earlier extra roots do not expand later workspace sandboxes". — [openai/codex windows-sandbox-rs/src/cap.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/cap.rs)
- **Restricted-token weakness:** folders writable by `Everyone` "can't be fully protected by the sandbox", and Codex warns about them. This follows from `Everyone` being in the restricting-SID list. — [Codex docs: Windows sandbox](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- **Chromium's strictest token:** logon SID mandatory, all other SIDs deny-only, restricted SID `S-1-0-0`, no privileges, Untrusted integrity (`S-1-16-0x0`). "It is near impossible to find an existing resource that the OS will grant access" to with this token. An Untrusted-IL process "can only write to resources which have a null DACL" or an explicit Untrusted label. Chromium treats IL as defense-in-depth. — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- **Chromium's job object** blocks `SystemParametersInfo` changes, desktop creation and switching, display changes, clipboard, message broadcasts, global hooks, global atoms and USER handles from outside the job. It also limits the target to one active process. It "can (but currently does not)" enforce CPU/memory/IO limits. — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- **Chromium's alternate desktop:** a never-visible third desktop isolates sandboxed processes from input snooping and from messaging more privileged windows. It costs about 4 MB from a separate pool. — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- **Codex desktops:** Codex creates private desktops with `CreateDesktopW` and reuses one "only for the same sandbox account and effective permissions", so "GUI hooks do not automatically cross those policies". Both Codex modes use a private desktop by default. — [openai/codex windows-sandbox-rs/src/desktop.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/desktop.rs); [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- **MXC's UI policy:**
  - `ui.disable` maps to the Win32k system-call-disable mitigation ("cannot create windows, use GDI, or make NtUser*/NtGdi* calls").
  - It also applies job-object UI restrictions.
  - Clipboard and `SendInput` injection are controlled separately.

  — [MXC schema.md](https://github.com/microsoft/mxc/blob/main/docs/schema.md)
- **WFP needs admin.** The default filter-engine security descriptor:
  - grants `GENERIC_ALL` to Administrators;
  - grants `GENERIC_READ`/`GENERIC_WRITE`/`GENERIC_EXECUTE` to Network Configuration Operators and to specific service SIDs (MpsSvc and others);
  - grants **only `FWPM_ACTRL_OPEN` and `FWPM_ACTRL_CLASSIFY` to Everyone**.

  `FwpmFilterAdd0` needs `FWPM_ACTRL_ADD`/`ADD_LINK`, so standard users cannot add filters. — [Microsoft Learn: WFP access control](https://learn.microsoft.com/en-us/windows/win32/fwp/access-control)
- **Windows Firewall is a WFP front end.** MPSSVC converts firewall rules into WFP filters, and "all access to the kernel firewall needs to be mediated through the service". If "any sublayer returns a Block result then the packet will be blocked." — [Project Zero](https://projectzero.google/2021/08/understanding-network-access-windows-app.html)
- **Codex's WFP filters:**
  - Installed from the "already-elevated setup helper".
  - Persistent, under a Codex-owned provider and sublayer with stable GUIDs.
  - Scoped to one sandbox account with `FWPM_CONDITION_ALE_USER_ID`, plus protocol and remote-port conditions.

  — [openai/codex windows-sandbox-rs/src/wfp.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/wfp.rs)
- **Job-object flags available in Go:** `x/sys/windows` exposes `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` and the UI-limit flags (`DESKTOP`, `DISPLAYSETTINGS`, `EXITWINDOWS`, `GLOBALATOMS`, `HANDLES`, `READCLIPBOARD`, `WRITECLIPBOARD`, `SYSTEMPARAMETERS`). — [golang/sys types_windows.go](https://github.com/golang/sys/blob/master/windows/types_windows.go)
- **Memory and CPU limits** (background, not re-fetched this session): `JOBOBJECT_EXTENDED_LIMIT_INFORMATION` provides `ProcessMemoryLimit`/`JobMemoryLimit`/`ActiveProcessLimit`, and `JOBOBJECT_CPU_RATE_CONTROL_INFORMATION` provides CPU caps. — [Microsoft Learn: JOBOBJECT_EXTENDED_LIMIT_INFORMATION](https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_extended_limit_information); [JOBOBJECT_CPU_RATE_CONTROL_INFORMATION](https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_cpu_rate_control_information)

### Inferences
- **No-admin baseline that works on build 22000 today:**
  - a write-restricted token with a per-agent synthetic SID;
  - ACL grants of that SID on the agent's private workspace;
  - a job object (kill-on-close, active-process and memory limits, UI limits);
  - a private desktop;
  - `PROC_THREAD_ATTRIBUTE_HANDLE_LIST` to stop handle leaks;
  - mitigation policies.

  This protects the user's files from writes and keeps the process tree killable. It does **not** stop reads of `%USERPROFILE%` secrets (`.ssh`, browser profiles, `.aws`), and it does **not** stop network exfiltration.
- **Pattern that adds read denial and real egress control:** a one-time admin install that creates a dedicated local user, plus persistent WFP filters keyed on that user's SID. Codex elevated mode and Anthropic srt both use it. After install, the daemon launches with no admin.
- The daemon (not the GUI) should own job handles, so the kill-on-close semantics match Orole's rule that "terminals live in the daemon". This is an architecture inference.

### Gaps
- No primary source in this session confirms whether a WFP filter conditioned on an AppContainer package SID (`FWPM_CONDITION_ALE_PACKAGE_ID`), installed once by admin for a fixed profile, gives a robust per-launch no-admin egress allowlist on 21H2. Plausible, but unverified.
- Exact Windows Firewall rule definitions used by Codex's older "offline user" setup were not retrieved; the OpenAI blog returned HTTP 403.

---

## Q3. Chromium's Windows sandbox, and running Chrome/Edge headless inside a constrained token

### Takeaway
Chromium's broker/target sandbox is the reference design. It stacks a lockdown restricted token, Untrusted IL, a job object, an alternate desktop, AppContainer/LPAC for some processes, and process mitigations. Chromium itself must be the *broker*, so running a whole browser inside an outer sandbox means nesting sandboxes. I found no authoritative 2026 source documenting Chrome/Edge headless working inside an AppContainer or a write-restricted token on Windows.

### Cited Findings
- **Broker and targets.**
  - The broker (the browser process) "should always outlive all the target processes".
  - Interception plus IPC to the broker "does not provide security". The token, job, desktop and IL do.
  - Rules "can only be added before each target process is spawned".
  - `LowerToken()` drops the initial impersonation token irreversibly.

  — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- **Mitigations used:**
  - Win32k lockdown (`DisallowWin32kSystemCalls`) for renderers.
  - Extension-point disable, which blocks AppInit DLLs, LSPs, global hooks and legacy IMEs.
  - CIG (blocks unsigned code), ACG, font-loading disable, image-load restrictions, and child-process restriction.

  — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- **Stated non-goals:** OS/kernel bugs ("There is no practical way to prevent code in the sandbox from calling a system service"), third-party injected DLLs (antivirus) and non-securable resources. — [Chromium sandbox design doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md)
- **Proxying Chromium in MXC:** MXC's PSEC path sets a per-container WinHTTP proxy so "WinHTTP-stack clients (e.g., the WinHTTP/Chromium stack)" are pointed at a loopback proxy. It also sets `HTTP(S)_PROXY` for runtimes that read env vars, and blocks direct egress with WFP. — [MXC networking.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/networking.md)
- **Linux analogue in srt:** sandboxed commands "cannot create user namespaces by default, which breaks Chromium's sandbox" unless `allowNestedUserNamespaces` is enabled. — [anthropic-experimental/sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)

### Inferences
- The most robust no-VM way to run an agent-driven browser on Windows is a **separate local user account** (srt/Codex elevated style, or MXC session isolation on 24H2+). Under that account Chromium's own sandbox keeps working normally. Egress is enforced by WFP filters on that user's SID plus `--proxy-server` pointing at an allowlisting proxy.
- Running Chromium *inside* an AppContainer, a write-restricted token, or an `ActiveProcessLimit=1` job will likely break it. Its broker needs to spawn children and create its own desktops and jobs (job UI limits block desktop creation). Expect to need `--no-sandbox`, which removes Chromium's inner layer. This is an inference, not verified.
- Headless Chrome traffic follows `--proxy-server` or WinHTTP settings. Neither is enforcement unless WFP or AppContainer blocks the direct path.

### Gaps
- No primary or authoritative 2026 report of headless Chrome/Edge inside AppContainer/LPAC on Windows (pass/fail, required flags) was found.
- Whether Playwright-driven Chromium works under MXC ProcessContainer was not found; searching the MXC repo for "playwright" returned nothing.

---

## Q4. OpenAI Codex CLI's Windows sandbox (2025–2026)

### Takeaway
Codex has three Windows backends:
- **unelevated:** restricted token derived from the current user, ACL write boundary, env-level "offline" controls, no read denial. Fallback only.
- **elevated:** a one-time admin setup creates dedicated sandbox users, a persistent WFP/firewall block for the offline user, deny-read ACLs and private desktops. Previously the preferred mode.
- **MXC:** recommended where supported (Win11 24H2/25H2 build .9278+). No admin, no extra accounts, no firewall rules.

Codex rejected plain AppContainer because default-deny reads would need too many holes. That reason comes from third-party reporting.

### Cited Findings
- **Elevated mode:**
  - "Requires administrator-approved setup", but commands run without admin.
  - Uses dedicated lower-privilege sandbox users plus "firewall rules and local policy changes".
  - Has "Stronger network isolation" via "a dedicated offline-user firewall rule".
  - Supports denied read paths.

  — [Codex docs: Windows sandbox](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- **Unelevated mode:**
  - Uses "a restricted Windows token derived from your current user" with ACL-based filesystem boundaries.
  - Network uses "environment-level offline controls" and "has weaker network isolation than `elevated`".
  - No denied read paths.
  - Intended for when admin setup is blocked and policy permits.

  — [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- **MXC mode:**
  - "Uses native process isolation, with no administrator-approved setup, extra Windows accounts, or local firewall rules".
  - Requires Windows 11 24H2 (26100.9278) or 25H2 (26200.9278).
  - Config: `[windows] sandbox = "mxc"` or `[features] prefer_mxc = true`; admins can set `allow_mxc = false`.
  - "Remaining child processes stop when the foreground command exits."

  — [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- **Codex source structure:**
  - `codex-rs/windows-sandbox-rs` contains `token.rs`, `acl.rs`, `deny_read_acl.rs`, `wfp.rs`, `wfp_setup.rs`, `desktop.rs`, `hide_users.rs`, `dpapi.rs`, `conpty/` and an `elevated/` setup helper with a manifest.
  - A separate `codex-rs/mxc-sandbox` crate drives the MXC helper.
  - In the elevated path, the sandbox account is launched via `CreateProcessWithLogonW`.

  — [openai/codex windows-sandbox-rs](https://github.com/openai/codex/tree/main/codex-rs/windows-sandbox-rs); [desktop.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/desktop.rs); [mxc-sandbox](https://github.com/openai/codex/tree/main/codex-rs/mxc-sandbox)
- **Known operational issues:**
  - Error `1385`: the logon type is denied to the sandbox user, and IT must grant logon rights.
  - Diagnostics go to `CODEX_HOME/.sandbox/sandbox.log`. Secrets are in `CODEX_HOME/.sandbox-secrets/`, encrypted with DPAPI (`dpapi.rs` exists in the crate).

  — [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- **Issue #40158:** `codex exec` fell back to the restricted-token sandbox despite `windows.sandbox="elevated"`, and failed because "the unelevated restricted-token sandbox cannot enforce split filesystem read restrictions". — [openai/codex issue #40158](https://github.com/openai/codex/issues/40158) (from search snippet)
- **Third-party reporting** (danielvaughan.com, 2026-07-18, citing an OpenAI blog and InfoQ):
  - Elevated mode provisions `CodexSandboxOffline` (firewall-blocked) and `CodexSandboxOnline` accounts.
  - The restricted SID list is `[Everyone, Logon, Synthetic]`.
  - AppContainer was rejected because its default-deny read posture would need so many holes that "the security boundary became meaningless".

  — [Daniel Vaughan: Windows Sandbox Deep Dive](https://codex.danielvaughan.com/2026/07/18/codex-cli-windows-sandbox-architecture-powershell-ast-safety-elevated-unelevated-appcontainer-restricted-tokens/). The OpenAI primary post [Building a safe, effective sandbox to enable Codex on Windows](https://openai.com/index/building-codex-windows-sandbox/) returned HTTP 403 and was not read.
- Search-result summaries (not verified against source) say unelevated "offline" mode overrides proxy env vars and inserts stub executables for common network tools. — [search summary of FICUSLINK / third-party pages](https://ficuslink.com/en/r/codex-windows-sandbox-elevated-unelevated)

### Inferences
- Codex's design is directly reusable by Orole: synthetic per-workspace capability SIDs + `WRITE_RESTRICTED` token + private desktop + (optional, admin-once) sandbox user + `ALE_USER_ID` WFP filters. It is Rust (Apache-2.0); copying code requires attribution per Orole's AGENTS.md.
- OpenAI moving its recommendation to MXC suggests Microsoft's PSEC API is becoming the sanctioned primitive on new builds. Orole should plan an MXC (`wxc-exec.exe`) backend for 24H2+ hosts and a Codex-style legacy backend for 21H2–23H2.

### Gaps
- Exact current Firewall/WFP rule set and how "online" vs "offline" users map to policy, because the OpenAI primary blog was inaccessible.
- No published security audit or bypass list for Codex's Windows sandbox, beyond the `Everyone`-writable-folder caveat and issue reports.

---

## Q5. Anthropic sandbox-runtime (`@anthropic-ai/sandbox-runtime`) and Claude Code's sandbox; Windows status

### Takeaway
srt (Beta Research Preview) enforces filesystem and network limits with Seatbelt on macOS and bubblewrap + network namespace + socat-bridged proxies on Linux. It now has **alpha native Windows support**: a dedicated `srt-sandbox` local user, NTFS ACEs, a WFP filter allowing only loopback to the proxy, and a one-time elevated install. **Claude Code itself still runs commands unsandboxed on native Windows** and tells Windows users to use WSL2. Microsoft's MXC launch lists Claude Code as an "upcoming" MXC adopter.

### Cited Findings
- **Network:** srt is deny-by-default with `allowedDomains`/`deniedDomains`. HTTP/HTTPS goes through an HTTP proxy and other TCP through SOCKS5.
  - Linux removes the network namespace and bind-mounts proxy Unix sockets.
  - macOS Seatbelt only allows connections to the proxy's localhost port.
  - "Windows: A WFP filter blocks outbound connections from the `srt-sandbox` account except loopback to the proxy port range."

  — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)
- **Windows status and setup:**
  - "Windows support is **alpha**. The sandboxed process runs under a dedicated `srt-sandbox` local user account".
  - One-time elevated `npx @anthropic-ai/sandbox-runtime windows-install`, idempotent, "No logout is required".
  - "Running under a distinct user SID structurally closes the surrogate-spawn class of escape".
  - "Filesystem isolation is enforced by NTFS discretionary ACLs": additive inheriting ACEs for the `srt-sandbox` SID, removed at `reset()`.

  — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)
- **Windows limitations (from the same README):**
  - Schannel revocation checks fail (`CRYPT_E_REVOCATION_OFFLINE`).
  - "Per-user tool installs are not reachable" (tools in the user profile cannot be opened by the sandbox account).
  - Per-command `allowRead`/`allowWrite` overrides throw.
  - "`proxyAuthToken` is visible in the runner's command line."
  - "DNS resolution via the system resolver is not fenced."

  — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)
- **General caveats:**
  - Domain fronting may bypass the filter.
  - Allowing broad domains such as `github.com` enables exfiltration.
  - The filter "restricts domains only and doesn't inspect traffic".
  - A resolved-address check blocks allowlisted names that resolve to loopback, link-local or metadata addresses.

  — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)
- **Claude Code on Windows:** "The sandbox runs on macOS, Linux, and WSL2. On native Windows, Claude Code runs commands unsandboxed. To use the sandbox on a Windows machine, run Claude Code inside a WSL2 distribution." With `failIfUnavailable`, Claude Code exits at startup on native Windows. — [Claude Code docs: sandboxing](https://code.claude.com/docs/en/sandboxing)
- **Proxy env vars are advisory; the OS enforces.** Claude Code directs commands to the proxy with `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` and related variables. Tools that ignore them (plain `ssh`, DB drivers) "can't connect, even to an allowed host". This is because the OS layer (Linux netns, Seatbelt) blocks direct routes. `curl --noproxy '*'` "Fails ... because the command has no route around the sandbox proxy". — [Claude Code docs](https://code.claude.com/docs/en/sandboxing)
- **Optional TLS termination:** an experimental `network.tlsTerminate` lets the proxy swap credential "sentinels" for real secrets on allowed hosts, so sandboxed commands never see the secret. — [Claude Code docs](https://code.claude.com/docs/en/sandboxing)
- **MXC adopter list:** GitHub Copilot, OpenClaw, OpenAI Codex, Replit, LM Studio and Unsloth support MXC today; "Anthropic Claude Code ... and others" are upcoming. — [Windows Developer Blog, 2026-10-07](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/)

### Inferences
- srt's Windows design (distinct user + ACLs + WFP-on-user-SID + host proxy) is the same pattern as Codex elevated mode. This is the convergent "admin-once" architecture for pre-24H2 Windows.
- srt's "DNS not fenced" caveat matches Forshaw's finding that DNS goes through a system service. DNS is a residual exfiltration channel unless DNS is also controlled.

### Gaps
- srt's Windows source (`srt-win.exe`) internals were not reviewed: job objects, desktop isolation, how the logon is performed.
- No timeline from Anthropic for native-Windows sandboxing in Claude Code. The MXC blog's "upcoming" is Microsoft's statement.

---

## Q6. Other implementations: MXC, Sandboxie-Plus, Win32 app isolation, Windows Sandbox, Deno/Bun, WebAssembly

### Takeaway
**MXC is the most relevant new option.** It is MIT-licensed with Rust, .NET and Node SDKs plus a standalone `wxc-exec.exe` that takes JSON. That gives a Go app a usable integration path via a subprocess. Its best tier needs 24H2+ (August/September 2026 updates).

The other options, briefly:
- **Sandboxie-Plus:** a kernel-driver sandbox (admin install) with WFP callouts for per-box network rules.
- **Win32 app isolation:** still preview. Reports from August 2026 call it stalled.
- **Deno:** its permissions do not survive subprocess spawning.
- **WASI/Wasmtime:** strong capability isolation, but only for code that compiles to Wasm.

### Cited Findings
- **MXC overview:**
  - A "policy-driven execution layer for untrusted or dynamically generated workloads", GA on 2026-10-07.
  - Backends: process container (AppContainer on Windows, Seatbelt, Bubblewrap); session container (Windows 11 only, "distinct Windows account and session", separate desktop/clipboard/UI/input); WSL container; experimental MicroVM.
  - Modes: Enforcement, Learning (block and record to a JSON activity report) and Permissive.
  - Intune policy management of process containers is coming "soon".

  — [Windows Developer Blog](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/)
- **MXC packaging:**
  - Repo created 2026-02-06, MIT licence, mostly Rust.
  - SDKs: `mxc-sdk` (crates.io), `Microsoft.Mxc.Sdk` (NuGet), `@microsoft/mxc-sdk` (npm).
  - "Non-SDK consumption: Platform-specific executor binaries, such as `wxc-exec.exe`, accept JSON container-creation requests defined by the stable schema."
  - Windows backends: `processcontainer` (default), `windows_sandbox`*, `wslc`, `microvm`*, `hyperlight`*, `isolation_session` (* = experimental).
  - `--audit` mode "turns off all sandbox security" and is used for policy authoring.

  — [microsoft/mxc README](https://github.com/microsoft/mxc) (fetched via GitHub API 2026-10-15)
- **MXC minimum builds** for Process Isolation / Session Isolation:
  - 24H2: 26100.9278 / 26100.9550
  - 25H2: 26200.9278 / 26200.9550
  - 26H2: 26300.9550
  - 26H1: 28000.2804 / 28000.3086

  **Windows 11 21H2 (22000) is not listed.** — [MXC os-version-support.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/os-version-support.md)
- **MXC egress on PSEC (GA):**
  - Each container gets WFP egress filters (IP/CIDR, protocol and port, IPv4 and IPv6; block beats allow) and a per-container WinHTTP proxy, "applied with no UAC prompt per launch".
  - Recommended "Model 2" is proxy-only egress: WFP lets the container connect only to a configured loopback proxy. The proxy is bound by identity (package family or AppContainer profile) via `allowedProxyPeer`.
  - Out of GA scope: transparent TCP/UDP redirection, L7 classification, "Durable DNS-name rules", payload inspection.

  — [MXC networking.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/networking.md)
- **MXC host prep:** `wxc-host-prep.exe` is "privileged-by-manifest" (`requireAdministrator`). `wxc-exec.exe` "never elevates itself". — [MXC host-prep.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/host-prep.md)
- **MXC maturity:** in July 2026 it was described as an "early preview". — [Help Net Security](https://www.helpnetsecurity.com/2026/07/07/microsoft-execution-containers-ai-agents-constraints/)
- **Sandboxie-Plus network filtering:**
  - WFP callouts in the Sandboxie *driver* at `ALE_AUTH_CONNECT` and `ALE_AUTH_RECV_ACCEPT` (IPv4/IPv6), enabled with `NetworkEnableWFP=y`.
  - Stronger than the user-mode Winsock hooks in SbieDll, which "code can bypass".
  - Default is permit when no rule matches.
  - Coverage "is not a guarantee for every raw or ICMP/ICMPv6 path".

  — [Sandboxie-Plus: WFP support](https://sandboxie-plus.com/?p=2497)
- **Sandboxie device restriction:** `RestrictDevices=y` limits driver/device endpoints. — [Neowin: Sandboxie Plus 1.3.1 notes](https://www.neowin.net/software/sandboxie-plus-131--classic-5581/)
- **Win32 app isolation:**
  - The Learn overview still carries a preview notice.
  - An August 2026 report says it "has been stuck in preview for a long time", with the latest updates in 2024.
  - It is built on AppContainers and requires MSIX packaging.

  — [Microsoft Learn: App isolation overview](https://learn.microsoft.com/windows/win32/secauthz/app-isolation-overview); [Privacy Guides, 2026-08-28](https://www.privacyguides.org/news/2026/08/28/windows-is-testing-new-privacy-protections-for-apps/); [Windows blog: Sandboxing Python with Win32 app isolation (2024)](https://blogs.windows.com/windowsdeveloper/2024/03/06/sandboxing-python-with-win32-app-isolation/)
- **Deno:** "A subprocess runs as a separate program with its own permissions ... whatever it does happens outside the sandbox". FFI runs native code in-process. "Treat both as equivalent to `--allow-all`". `--allow-net=host` scopes network access by host. — [Deno docs: Security](https://docs.deno.com/runtime/fundamentals/security/)
- **Wasmtime/WASI:** "applications can only access files and directories they've been given access to". There are no raw syscalls, and all I/O goes through imports. Defenses include a 2 GB guard region and Spectre mitigations on table and `br_table` access. "Mitigating Spectre continues to be a subject of ongoing research." — [Wasmtime docs: Security](https://docs.wasmtime.dev/security.html)

### Inferences
- **MXC is the best fit for Orole on 24H2+ hosts.** Orole's daemon (Go) can launch `wxc-exec.exe` with a JSON request per command, or embed it through a small Rust shim. There is no Go SDK. The Learning/audit modes help tune policies for git, Python and Node.
- On 22000, MXC either refuses or drops to the AppContainer + DACL tier. That tier lacks per-destination egress and needs admin host prep. This is inferred from the docs; I did not test MXC on 22000.
- Sandboxie-Plus is a mature *user* tool, but its kernel driver and global install model make it a poor embedded dependency for an app.
- Deno and Wasm are useful only for running *scripts the agent writes* in a constrained runtime. They cannot contain arbitrary `git`, `python` or `npm` invocations.

### Gaps
- **Windows Sandbox** (Hyper-V VM): I found no 2026 primary source in this session. In MXC it is listed as an experimental `windows_sandbox` backend. Third-party reporting says its startup latency "conflicts with the agent's execute-observe loop" ([Daniel Vaughan](https://codex.danielvaughan.com/2026/07/18/codex-cli-windows-sandbox-architecture-powershell-ast-safety-elevated-unelevated-appcontainer-restricted-tokens/)). Edition and admin requirements were not re-verified.
- **Windows "Administrator protection"** was not researched. From background knowledge it is a UAC/elevation hardening feature, not a process sandbox, but this is not verified.
- **Bun's permission model** status in 2026 was not researched.
- **WASI networking** (wasi-sockets) defaults in Wasmtime: the security page does not cover networking.

---

## Q7. Egress control without admin: what is truly enforceable per-process on Windows

### Takeaway
- **Without admin, on build 22000:** the only OS-enforced per-process network control is **binary**. An AppContainer launched without `internetClient`/`privateNetworkClientServer` has no internet or LAN access. DNS still resolves via the system service. That container also cannot reach a host loopback proxy without an admin-granted exemption, short of an undocumented capability trick.
- **Proxy env vars:** advisory only.
- **Domain allowlisting through a proxy with enforcement:** needs WFP or firewall rules. These are admin-only to install, but can be installed **once** and scoped to a dedicated user SID (Codex, srt) or container SID.
- **Windows 11 24H2+ (Aug 2026 builds):** PSEC lets MXC apply per-container WFP and proxy configuration without a per-launch prompt.

### Cited Findings
- Standard users cannot add WFP filters. Everyone gets only `FWPM_ACTRL_OPEN`/`CLASSIFY` on the engine. — [Microsoft Learn: WFP access control](https://learn.microsoft.com/en-us/windows/win32/fwp/access-control)
- "Both WFP filter writes and per-container WinHTTP proxy configuration require a privileged context". PSEC applies "outbound WFP filters in the OS's elevated context". The AppContainer fallback "does not install directional WFP filters". — [MXC networking.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/networking.md)
- The AppContainer loopback exemption API is admin-only, and AppContainer-to-host loopback is blocked by default. — [Project Zero](https://projectzero.google/2021/08/understanding-network-access-windows-app.html)
- Proxy env vars only steer cooperative tools. Enforcement comes from the OS layer, which removes direct routes. — [Claude Code docs](https://code.claude.com/docs/en/sandboxing)
- MXC also sets `HTTP(S)_PROXY`, but "The containment boundary is the absence of direct internet capability". — [MXC networking.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/networking.md)
- Codex's no-admin (unelevated) mode admits "weaker network isolation" through "environment-level offline controls". — [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox)
- srt on Windows installs a WFP filter on the `srt-sandbox` account during a one-time elevated install. After that, per-command runs need no admin. — [sandbox-runtime README](https://github.com/anthropic-experimental/sandbox-runtime)
- **Known AppContainer egress bypasses:**
  - Default firewall permit rules for `dmcertinst.exe`, `omadmclient.exe` and `deviceenroller.exe` match only on App ID. An AppContainer that can spawn children could use them to reach the network ("fairly trivial to exploit"). WontFix in 2021.
  - CVE-2020-1509: LSASS did not enforce the Enterprise Authentication capability.

  — [Project Zero](https://projectzero.google/2021/08/understanding-network-access-windows-app.html); [Security Affairs](https://securityaffairs.com/posts/microsoft-wfp-appcontainer-bypass)

### Inferences
- **Recommended for Orole on 22000:**
  1. A **no-network** profile using AppContainer without capabilities, or a restricted token plus an admin-once WFP block on a sandbox user.
  2. A **proxied-network** profile that needs a one-time admin install: a sandbox local user + persistent WFP filters with `ALE_USER_ID` = sandbox user, permitting only loopback to the daemon's proxy port. The proxy runs in the daemon outside the sandbox and does the domain allowlisting.

  The daemon has no network listener beyond loopback, which is consistent with AGENTS.md if the proxy binds loopback only.
- DNS is a residual exfiltration channel in every Windows design reviewed (srt says "not fenced"; AppContainer DNS goes through the service). Treat DNS-based exfiltration as out of scope or mitigate it at the host resolver.

### Gaps
- No source confirmed a reliable, documented, no-admin way on 21H2 to let an AppContainer reach *only* a specific loopback port.

---

## Q8. Comparison table (strength, admin at install vs runtime, tool compatibility, startup cost, availability on build 22000)

### Takeaway
On build 22000, a restricted token + job + desktop gives a no-admin write boundary. Adding a dedicated user + WFP gives a stronger, network-controlled boundary for one admin prompt at install. MXC/PSEC is the forward path but needs an OS upgrade. Startup cost of all process-level options is qualitatively low compared with VMs, but no measured numbers were found.

### Cited Findings

| Mechanism | What it enforces | Admin at install | Admin at runtime | Tool compatibility (git/Python/Node/Chromium) | On build 22000? | Sources |
|---|---|---|---|---|---|---|
| Write-restricted token + synthetic SIDs (Codex unelevated) | Writes only where ACL grants synthetic SID; no read denial; `Everyone`-writable dirs leak; network only "env-level" | No | No | High (reads as the user) | Yes | [token.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/token.rs), [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox) |
| Dedicated local user + ACLs + WFP on user SID (Codex elevated, srt Windows alpha) | Read/write via NTFS ACLs, egress blocked except allowed (srt: loopback to proxy), distinct SID closes surrogate-spawn escapes | **Yes (once)** | No | Medium-high; per-user tool installs unreachable (srt); logon-right issues (Codex 1385) | Yes | [srt README](https://github.com/anthropic-experimental/sandbox-runtime), [wfp.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/wfp.rs), [Codex docs](https://learn.chatgpt.com/docs/windows/windows-sandbox) |
| AppContainer (no caps) | Default-deny files/registry (ACL grants needed), no network, no loopback to host, DNS still works | No (MXC tier 3 prep needs admin once for `C:\` root and `\Device\Null`) | No | Low without prep: cmd/PowerShell/node fail on `C:\` access; every toolchain dir must be granted | Yes | [MXC host-prep](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/host-prep.md), [Project Zero](https://projectzero.google/2021/08/understanding-network-access-windows-app.html) |
| LPAC | As AppContainer but also denies `ALL APPLICATION PACKAGES` objects | No | No | Lower still (needs `ALL RESTRICTED APPLICATION PACKAGES` ACLs) | Yes (Win10 1703+) | [Chromium doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md) |
| Job object | Kill-on-close, process count, memory/CPU limits, UI limits | No | No | High (but `ActiveProcessLimit=1` breaks shells and Chromium) | Yes | [x/sys types](https://github.com/golang/sys/blob/master/windows/types_windows.go), [Chromium doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md) |
| Private desktop | UI/input isolation from user's desktop | No | No | High for CLI; GUI tools invisible | Yes | [desktop.rs](https://github.com/openai/codex/blob/main/codex-rs/windows-sandbox-rs/src/desktop.rs) |
| Low/Untrusted IL | No write-up to Medium objects; reads mostly allowed | No | No | Medium (writes to profile fail) | Yes | [Chromium doc](https://chromium.googlesource.com/chromium/src/+/HEAD/docs/design/sandbox.md) |
| WFP filters / Firewall per-program rules | Per-SID/AppContainer/app egress by IP/port/protocol | **Yes** | No if persistent | N/A (network only) | Yes | [WFP access control](https://learn.microsoft.com/en-us/windows/win32/fwp/access-control) |
| MXC ProcessContainer on PSEC (BaseContainer) | OS-native FS allow/deny, per-container WFP egress, WinHTTP proxy, UI policy, learning mode | No (for PSEC tier) | No ("no UAC prompt per launch") | Medium; upward traversal hits ACCESS_DENIED; tune with audit/learning | **No** (needs 26100/26200.9278+) | [MXC os-version-support](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/os-version-support.md), [networking.md](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/networking.md) |
| MXC Session isolation | Separate Windows account + session, desktop, clipboard, input | Not documented in sources read | — | High (full user session) | **No** (needs .9550+) | [Windows Dev Blog](https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/), [os-version-support](https://github.com/microsoft/mxc/blob/main/docs/backends/process-container/os-version-support.md) |
| Sandboxie-Plus | Driver-based FS/registry virtualization, WFP callouts per box | **Yes (driver)** | No | High (designed for arbitrary apps) | Yes | [Sandboxie WFP](https://sandboxie-plus.com/?p=2497) |
| Win32 app isolation | AppContainer + MSIX packaging, consent prompts | Packaging | No | Low-medium; preview, stalled | Preview only | [Learn overview](https://learn.microsoft.com/windows/win32/secauthz/app-isolation-overview), [Privacy Guides](https://www.privacyguides.org/news/2026/08/28/windows-is-testing-new-privacy-protections-for-apps/) |
| Deno permissions | In-runtime FS/net/env checks | No | No | Only Deno code; `--allow-run`/FFI = allow-all | Yes | [Deno security](https://docs.deno.com/runtime/fundamentals/security/) |
| WASI (Wasmtime) | Capability FS, no raw syscalls | No | No | Only Wasm-compiled code | Yes | [Wasmtime security](https://docs.wasmtime.dev/security.html) |

- Windows 11 21H2 (the developer's build 22000) Home/Pro reached **end of servicing on 2023-10-10**. It gets no security updates, which weakens any OS-enforced sandbox on that host. — [Microsoft Learn lifecycle](https://learn.microsoft.com/en-us/lifecycle/announcements/windows-11-21h2-end-of-servicing)

### Inferences
- **Suggested layered design for Orole's daemon:**
  - **Always:** a job object (kill-on-close, limits), a private desktop, a handle allowlist, mitigation policies, and a write-restricted token with per-agent synthetic SIDs on a private workspace.
  - **Optional one-time admin "hardened mode":** a sandbox local user + deny-read ACLs on secrets + persistent WFP allowing only loopback to the daemon's egress proxy.
  - **On 24H2+ with the August 2026 update or later:** prefer MXC (`wxc-exec.exe`) ProcessContainer with Model 2 proxy egress.
  - **Fallback:** keep VMs for untrusted browsing or high-risk tasks.
- Upgrading the dev machine from 21H2 to 24H2/25H2 is effectively required to develop and test the MXC/PSEC path, and it is advisable for security regardless, since 21H2 is out of servicing.

### Gaps
- No measured process-creation or ACL-application latency for any mechanism. Startup cost is qualitative.
- Session-isolation admin requirements in MXC were not documented in the sources read.
