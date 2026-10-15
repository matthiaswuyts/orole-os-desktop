# In-VM plumbing for agent browser, terminal and files, with human live view and takeover (state as of October 2026)

Scope: the components and protocols that sit on top of a per-agent local VM. The host is a Windows 10/11 desktop app (Go daemon + React UI in WebView2) that must not open listeners on network interfaces. Each section ends with how the component can be wired over local IPC instead of TCP. Research was done on 2026-10-15. Most sources are live repos and docs read on that date. Older items are labelled.

---

## 1. In-guest control daemons: E2B envd, Daytona toolbox, OpenHands action/agent server, Cua computer-server

### Takeaway
All four products use the same pattern: a single daemon inside the guest that exposes exec/PTY, files and sometimes GUI control over HTTP-family RPC, authenticated with a per-sandbox bearer/access token. E2B's envd is the most transferable design for a Go host. It is a Go binary using chi + ConnectRPC (gRPC-compatible, works over plain HTTP/1.1). It has `process` and `filesystem` RPC services plus a small REST API (`/files`, `/init`, `/health`, `/freeze`), an `X-Access-Token` header, signed file URLs, and cgroup-based self-protection. The others are REST (Daytona), REST+WebSocket (OpenHands agent-server) and JSON-over-WebSocket (Cua).

### Cited Findings
**E2B envd (Firecracker guest daemon, Go)**
- envd is described as a "Daemon that runs inside a sandbox that allows interacting with the sandbox via calls from the SDK". Server stubs are generated from specs in `./spec/`. — [e2b-dev/infra envd README](https://github.com/e2b-dev/infra/tree/main/packages/envd)
- `defaultPort = 49983` (flag `-port`). It binds `0.0.0.0:<port>`, with router `go-chi/chi/v5`, ConnectRPC (`connectrpc.com/authn`, `connectrpc.com/cors`) and `idleTimeout = 640 * time.Second`. Registered services are `filesystemRpc.Handle(...)`, `processRpc.Handle(...)` and a generated REST API (`api.HandlerFromMux`), plus `POST /upgrade` for live self-upgrade. Auth middleware is `authn.NewMiddleware(permissions.AuthenticateUsername)`. — [envd main.go](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/main.go)
- envd puts guest processes into cgroups and reserves headroom for itself: `maxMemoryReserved = min(MemTotal/8, 128 MiB)` and `memoryMax = memoryHigh = MemTotal - reserved`. The groups are `ptys` (cpu.weight 200), `socats` (cpu.weight 150, memory.min 5 MB) and `user` (cpu.weight 50). A `WorkloadFreezer` freezes workloads before pause. A `portScannerInterval = 1000 ms` scans for newly opened guest ports. The flag `-isnotfc` runs it outside Firecracker and skips the MMDS poll. — [envd main.go](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/main.go)
- Process service `process.Process` has these RPCs: `List` (unary), `Start` (server-streaming), `Connect` (server-streaming, reattach to a running process by `pid` or `tag`), `Update` (unary, PTY resize), `StreamInput` (client-streaming), `SendInput`, `SendSignal` (SIGTERM/SIGKILL) and `CloseStdin`. PTY size is `cols`/`rows`. Event stream types are `StartEvent{pid}`, `DataEvent{stdout|stderr|pty bytes}`, `EndEvent{exit_code, exited, status}` and `KeepAlive`. The proto notes "For PTY, send Ctrl+D (0x04) instead" of CloseStdin. — [process.proto](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/spec/process/process.proto)
- REST API endpoints: `GET /health` (204), `GET /metrics`, `POST /init` (sync time/metadata from host), `POST /freeze`, `/unfreeze`, `/collapse` (collapse heap into hugepages before pause), `/fsfreeze`, `/fsthaw`, `GET /envs`, `GET/POST /files` (upload via multipart or octet-stream, `X-Metadata-<key>` stored as xattrs) and `POST /files/compose` (zero-copy concatenation). Auth uses `AccessTokenAuth` = header `X-Access-Token`, or a `signature` + `signature_expiration` query parameter for presigned file URLs. — [envd.yaml](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/spec/envd.yaml)

**Daytona toolbox**
- The Toolbox API is an HTTP REST API that runs inside every sandbox and auto-starts on a local, dynamically assigned port. Its endpoint groups are `/process/*` (exec, sessions, PTY), `/files/*`, `/git/*`, `/lsp/*`, `/port/*`, `/computeruse/*` (keyboard, mouse, screenshots), `/version`, `/work-dir`, `/user-home-dir` and `/init`. Auth is `Authorization: Bearer <token>`. It is reached through `{toolboxProxyUrl}/{sandboxId}`. — [Daytona Toolbox API overview (third-party Mintlify mirror)](https://mintlify.wiki/daytonaio/daytona/api/toolbox/overview); [ex_daytona community SDK](https://readme.hex.pm/ex_daytona/0.3.0)
- PTY support was added "across daemon, API, SDKs" for real-time terminal sessions, resizing and process control. The Ruby SDK's `connect_pty_session` reattaches over a WebSocket. — [Daytona changelog](https://www.daytona.io/changelog/pty-support-sandbox-cleanup); [Daytona PTY docs](https://www.daytona.io/docs/pty/); [rubydoc Daytona::Process](https://www.rubydoc.info/gems/daytona/Daytona/Process)

**OpenHands**
- Legacy runtime (V0): the backend "communicates with the action execution server over RESTful API" (actions in, observations out). The runtime starts a bash shell, a Jupyter Kernel Gateway for IPython and a browser. VS Code is exposed at `http://localhost:{port}/?tkn={token}`, with ports taken from file-locked ranges. — [OpenHands runtime architecture](https://docs.openhands.dev/usage/architecture/runtime)
- Current (Software Agent SDK): the agent server is an "HTTP/WebSocket server" that streams events over a live WebSocket. Workspace types are `DockerWorkspace`, `DockerDevWorkspace` and `APIRemoteWorkspace`, and the image is `ghcr.io/openhands/agent-server:latest-python`. — [OpenHands agent-server overview](https://docs.openhands.dev/sdk/guides/agent-server/overview)
- The architecture docs list REST/WebSocket endpoints for conversations, bash, files, events, desktop and VSCode, with per-user session isolation and API-key auth. The MLSys 2026 paper describes workspace UIs as "browser-based VSCode IDE, VNC desktop, and persistent Chromium browser" for human inspection and control. — [OpenHands SDK arch overview](https://docs.openhands.dev/sdk/arch/overview); [arXiv 2511.03690](https://arxiv.org/html/2511.03690v1); [MLSys 2026 oral](https://mlsys.org/virtual/2026/oral/3749)

**Cua computer-server**
- Local endpoint is `ws://localhost:8000/ws`. Requests are JSON `{command, params}` and responses carry `success`. Cloud containers use `wss://…:8443` and require an `authenticate` command (container name + API key) immediately after connect. Cua's own docs say the API reference is "under development". — [Cua WebSocket API](https://cua.ai/libraries/computer-server/WebSocket-API); [Cua computer-server docs](https://docs.trycua.com/docs/libraries/computer-server)
- A third-party skill page shows `pip install cua-computer-sdk` then `cua-server start --port 8000`, health at `/status`. This is not official. — [clawhub desktop-automation](https://clawhub.ai/sarinali/desktop-automation)

**E2B Desktop (GUI layer over envd)**
- Xfce-based Linux desktop. The SDK offers `desktop.stream.start()` (whole desktop or a single `window_id`, one stream at a time) and `get_url(view_only=True)`. `require_auth=True` generates an auth key for the stream URL. Actions are mouse (click/drag/scroll), `write()`, `press()`, `launch()`, `open()`, `screenshot()`, window queries and `commands.run()`. — [e2b-dev/desktop](https://github.com/e2b-dev/desktop)

### Inferences
- For Orole, an envd-style guest agent in Go (chi + ConnectRPC, or `handle[P,R]`-style typed JSON over a stream) fits best. ConnectRPC runs over any `net.Listener`, so the same handlers can be served on an `AF_VSOCK` listener (`mdlayher/vsock`) instead of `0.0.0.0:49983`. The host Go daemon then dials with `go-winio` hvsock (see section 5). Nothing listens on an IP interface on either side.
- Copy three envd ideas directly. (a) Reattachable processes by `tag` with server-streaming events, so the GUI can reconnect to a PTY. (b) A cgroup reservation so the agent daemon survives a guest OOM. (c) `/freeze`, `/init` (clock resync) and `/fsfreeze` hooks around snapshot/pause.
- Every product authenticates the guest daemon with a bearer token because it is reachable over a network. With a vsock-only daemon, the transport is already scoped to "this VM's host". A per-VM token injected at boot (kernel cmdline, MMDS-equivalent, or the first message over vsock) still defends against other in-guest processes connecting to the agent port, because any guest process can open AF_VSOCK.

### Gaps
- The exact Daytona daemon listen port (commonly cited as 2280) and the PTY WebSocket path could not be confirmed from an official page. The mirror says the port is "dynamically assigned".
- The full Cua command list could not be retrieved (the official reference pages 404'd or were "under development").
- The OpenHands agent-server auth header name and VNC/desktop ports were not confirmed in the docs read.

---

## 2. Browser inside the VM: headless vs headful, driving it, memory, profiles, anti-bot, downloads

### Takeaway
Run real Chrome/Chromium headful on a virtual display (Xvfb, or a Wayland kiosk compositor such as cage) when a human must watch or take over. Use "new" headless (same browser code as headful) for pure automation. Drive it with CDP via Playwright, Playwright MCP or Chrome DevTools MCP. Since Chrome 136, remote debugging requires a non-default `--user-data-dir`. Persistent profiles are the Playwright MCP default but allow only one browser per profile. Memory is highly page-dependent: about 60–180 MB idle and around 1 GB peak on heavy sites in the one concrete measurement found.

### Cited Findings
- **Headless modes:** new Headless (`--headless`) runs the full Chrome code with platform windows created but not shown (since Chrome 112). The old headless is a separate implementation that has shipped only as the standalone `chrome-headless-shell` binary since Chrome 132.0.6793.0 (Puppeteer `headless: 'shell'`). — [Chrome Headless docs (updated 2024-10-21)](https://developer.chrome.com/docs/chromium/headless)
- **Remote debugging restriction (Chrome 136, announced 2025-03-17):** `--remote-debugging-port` and `--remote-debugging-pipe` "will no longer be respected if attempting to debug the default Chrome data directory". A custom `--user-data-dir` is required, the motivation was cookie theft, and Chrome for Testing keeps the old behaviour. — [Chrome blog: remote debugging switches](https://developer.chrome.com/blog/remote-debugging-port)
- **Playwright MCP options** include `--cdp-endpoint`, `--cdp-header`, `--browser`, `--headless`, `--extension` (attach to running Edge/Chrome), `--user-data-dir`, `--isolated`, `--storage-state`, `--proxy-server`, `--allowed-origins`, `--blocked-origins`, `--allowed-hosts`, `--output-dir`, `--caps` (vision, pdf, devtools), `--snapshot-mode` and `--shared-browser-context`. — [microsoft/playwright-mcp](https://github.com/microsoft/playwright-mcp)
- **Playwright MCP profiles and modes:**
  - The default is a persistent profile at `%USERPROFILE%\AppData\Local\ms-playwright\mcp-{channel}-{workspace-hash}` (Windows) or `~/.cache/ms-playwright/...` (Linux).
  - "A persistent profile can serve only one browser instance at a time". `--isolated` gives a fresh profile per session, which can be seeded with `--storage-state`.
  - The default snapshot mode acts on the accessibility tree. `--caps=vision` adds coordinate tools (`browser_mouse_click_xy`).
  - `--port` enables an HTTP transport at `/mcp`, and stdio is the default.
  - The README points to "Playwright CLI with SKILLS" as more token-efficient for coding agents. — [microsoft/playwright-mcp](https://github.com/microsoft/playwright-mcp)
- **Chrome DevTools MCP:**
  - Shows flags `--slim`, `--headless`, `--no-usage-statistics` and `--no-performance-crux`.
  - Usage statistics are on by default, and performance tools may send trace URLs to the CrUX API.
  - It checks npm for updates (disable with `CHROME_DEVTOOLS_MCP_NO_UPDATE_CHECKS`).
  - It can connect to a running Chrome (docs/advanced-usage.md), and only Chrome and Chrome for Testing are officially supported. — [ChromeDevTools/chrome-devtools-mcp](https://github.com/ChromeDevTools/chrome-devtools-mcp)
- **Wayland kiosk:** cage is a "Wayland kiosk compositor" that runs "a single, maximized application". It is built on wlroots 0.20 with optional compile-time Xwayland, and nested inside an existing X11/Wayland session it opens a virtual output. — [cage-kiosk/cage](https://github.com/cage-kiosk/cage)
- **Container/VM hygiene (Puppeteer):**
  - "Running without a sandbox is strongly discouraged", so run as a non-root user instead of `--no-sandbox`.
  - Use `--init`/`dumb-init` to reap zombie Chrome processes.
  - Install CJK/Thai fonts.
  - On read-only roots, set `XDG_CONFIG_HOME`/`XDG_CACHE_HOME` or `userDataDir` to writable paths. — [Puppeteer troubleshooting](https://pptr.dev/troubleshooting)
- **Memory (weak evidence):**
  - One concrete measurement (PyBA, Amazon.com, headless): about 60 MB idle with low_memory, about 180 MB without it, about 940 MB peak during the session, and about 130 MB after cleanup. — [PyBA performance docs](https://pyba.readthedocs.io/en/latest/performance.html)
  - Unsourced FAQ-style estimates give 50–150 MB per idle instance and 200–800 MB actively browsing. — [webscraping.ai FAQ](https://webscraping.ai/faq/headless-chromium/what-are-the-resource-requirements-for-running-headless-chromium-at-scale); [DEV Community](https://dev.to/deepak_mishra_35863517037/scaling-headless-browsers-managing-contexts-vs-instances-1d73)
  - Site Isolation has been on by default on desktop since Chrome 67, so cross-site iframes add renderer processes. One source cites "10-13% RAM overhead", which is unverified. — [superchargebrowser (secondary)](https://www.superchargebrowser.com/library/chrome-memory-management-deep-dive/)
- **Anti-bot:** Puppeteer/Playwright enable the CDP `Runtime` domain by default, and page scripts can detect that side effect. Vendors claim DataDome and Cloudflare use this "Runtime.enable leak". puppeteer-extra-plugin-stealth does not close it. Patched forks (Patchright, rebrowser-patches, nodriver, camoufox) patch at the CDP/C++ level. Patchright's patches apply only when it launches real Chrome via `launchPersistentContext`, not when attaching over CDP. Behavioural detection (mouse paths, typing cadence) still applies. These are mostly vendor and secondary sources. — [apiserpent 2026](https://apiserpent.com/blog/puppeteer-stealth-still-works-2026); [Centinel Analytica](https://www.centinelanalytica.com/blog/patched-chromium-browsers); [Patchright plugin page](https://hub.openclaw.ai/websirnik/plugins/patchright-stealth)
- **Downloads (CDP):** `Page.setDownloadBehavior(behavior: deny|allow|default, downloadPath)` is experimental and deprecated. `Page.downloadWillBegin`/`downloadProgress` are deprecated in favour of `Browser.downloadWillBegin` and `Browser.downloadProgress` (`guid`, `totalBytes`, `receivedBytes`, `state: inProgress|completed|canceled`). — [devtools-protocol Page.pdl](https://raw.githubusercontent.com/ChromeDevTools/devtools-protocol/master/pdl/domains/Page.pdl)

### Inferences
- A headful Chrome on Xvfb or cage inside the guest serves both modes. The agent drives it over CDP, and the human sees the same window through VNC/WebRTC or CDP screencast. Headful real Chrome with a persistent per-agent profile also gives the fewest bot-detection tells, because Patchright-style patches only help with launched persistent contexts.
- Keep CDP off TCP entirely. Launch Chrome with `--remote-debugging-pipe` from the in-guest agent and relay CDP frames over vsock to the host, or run Playwright/Playwright MCP inside the guest with stdio piped over vsock. Chrome 136 forces a non-default `--user-data-dir` anyway, which matches per-agent profiles.
- Profile strategy: keep one persistent profile per agent identity (cookies/logins), stored on the VM disk or a per-agent overlay. Use `--isolated` + `--storage-state` for throwaway tasks. Never share a persistent profile across concurrently running VMs (one-instance-per-profile limit).
- Disable Chrome DevTools MCP telemetry and CrUX by default (`--no-usage-statistics`, `--no-performance-crux`) to honour the "no unexpected egress" posture.
- Downloads: set `Browser.setDownloadBehavior` to a guest download directory, watch `Browser.downloadProgress`, then surface completed files to the host via the agent's file API (copy-out), not via a shared writable host folder.

### Gaps
- No authoritative 2025–2026 measurement of Chromium RSS/PSS inside a small VM was found, nor of Xvfb/cage overhead. Numbers above are from a single tool's benchmark and unsourced FAQs. Measure locally.
- The fd semantics of `--remote-debugging-pipe` (fd 3/4) and whether Playwright uses pipe mode by default were not verified in this session.
- No primary Chromium/V8 source was found for the claimed May 2025 change that removed the old stack-getter CDP detection.

---

## 3. Live view and takeover for the human: noVNC, VNC over a socket, WebRTC (Selkies, neko), CDP screencast; embedding in WebView2

### Takeaway
There are three viable tiers.
- **CDP screencast:** browser-only, cheapest, JPEG frames with explicit ack-based flow control. Input is injected via CDP.
- **VNC + noVNC:** whole desktop, mature. noVNC can be given an existing `WebSocket` or `RTCDataChannel` instead of a URL, which makes it easy to tunnel through app IPC.
- **WebRTC (neko) or Selkies:** smoothest video, but WebRTC needs UDP/ICE ports. Selkies now defaults to plain WebSockets, with WebRTC opt-in.

No vendor publishes comparable latency/CPU numbers. Hyper-V sockets are documented as "not the best for backup/video", which is a caution for high-bitrate streams.

### Cited Findings
- **CDP screencast:** `Page.startScreencast` (experimental) parameters:
  - `format` (`jpeg`|`png`), `quality` 0–100, `maxWidth`, `maxHeight`, `everyNthFrame`.
  - `maxFramesInFlight` ("frames allowed before a screencastFrameAck is required", default 3).
  - `sendLastFrame` ("trading overall performance for lower latency").

  Each `Page.screencastFrame` carries base64 `data`, a `sessionId` frame number and metadata (`deviceWidth`/`Height`, `pageScaleFactor`, `scrollOffsetX/Y`, `timestamp`). The client must call `Page.screencastFrameAck(sessionId)`. — [devtools-protocol Page.pdl](https://raw.githubusercontent.com/ChromeDevTools/devtools-protocol/master/pdl/domains/Page.pdl)
- **noVNC:** `new RFB(target, urlOrChannel, options)`. The second argument "must be a valid WebSocket URL" or "can also be a `WebSocket` or `RTCDataChannel`". Options and properties include `credentials`, `viewOnly`, `scaleViewport`, `resizeSession`, `qualityLevel` (0–9, default 6, JPEG), `compressionLevel` (0–9, default 2) and `focusOnClick`. Clipboard uses the `clipboard` event and `clipboardPasteFrom(text)`. — [noVNC API.md](https://github.com/novnc/noVNC/blob/master/docs/API.md)
- **neko:**
  - Self-hosted virtual browser in Docker streaming over WebRTC, written in Go + Vue, about 22.5k stars.
  - Images: Chromium, Chrome, Firefox, Edge, Brave and others, plus Xfce/KDE desktops.
  - Supports multi-user, multi-participant control.
  - Playwright/Puppeteer can be installed to automate the browser.
  - No latency figures are published. — [m1k1o/neko](https://github.com/m1k1o/neko)
- **neko networking:**
  - Needs either an ephemeral UDP range (`epr`, e.g. 59000-59100) or a single `udpmux`/`tcpmux` port (e.g. 59000). "UDP is generally better for latency", TCP is a fallback.
  - `nat1to1` advertises a single address.
  - "Reverse proxies cannot forward WebRTC traffic". You must expose WebRTC ports or use TURN. — [neko WebRTC config](https://neko.m1k1o.net/docs/v3/configuration/webrtc)
- **Selkies:** Linux-native HTML5 remote desktop streaming that targets X11 and Wayland. It streams over "plain WebSockets by default, with WebRTC available as an opt-in transport". The headline claim is "at least 60 frames per second on Full HD". It runs without a GPU, with automatic GPU selection when present. — [selkies-project/selkies](https://github.com/selkies-project/selkies)
- **E2B Desktop** exposes the desktop as a stream URL with optional view-only and an auto-generated auth key. — [e2b-dev/desktop](https://github.com/e2b-dev/desktop)
- **OpenHands** exposes a VNC desktop and a persistent Chromium for "human inspection and control". — [arXiv 2511.03690](https://arxiv.org/html/2511.03690v1)
- **Hyper-V sockets** capabilities: "Data stream only" and "No block memory (not the best for backup/video)". — [Microsoft: Make your own integration services](https://learn.microsoft.com/en-us/virtualization/hyper-v-on-windows/user-guide/make-integration-service)
- **WebView2:** custom schemes registered at environment creation can be served by the app via `WebResourceRequested`, and they participate in CORS/CSP. No Microsoft doc says WebSocket traffic is interceptable this way. — [ICoreWebView2CustomSchemeRegistration](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2customschemeregistration)

### Inferences
- **Recommended for Orole (no listeners):**
  - **(a) Browser-only view: CDP screencast relayed by the Go daemon.** The guest agent owns the CDP session (pipe), forwards `screencastFrame` JPEGs over hvsock/vsock, and the host pushes them to React via MyGo events onto a `<canvas>`. Takeover is implemented as CDP `Input.dispatch*` events from the UI, gated by a Go-side "human has control" lock that pauses the agent loop. Use `maxFramesInFlight` and the ack to apply backpressure end-to-end, and `everyNthFrame`/`maxWidth` to cap CPU.
  - **(b) Full desktop view: x11vnc or wayvnc in the guest, bridged by the guest agent to a vsock stream.** In the UI, hand noVNC a WebSocket-like shim object backed by MyGo IPC instead of a `ws://` URL. noVNC officially accepts a `WebSocket` or `RTCDataChannel`. Duck-typing a shim is plausible but unverified, so test it. Alternatively, run the RFB protocol inside a Go-side VNC client and push decoded frames, which is more work.
  - **Avoid WebRTC (neko) as the primary path.** ICE needs UDP sockets and candidate exchange, and "reverse proxies cannot forward WebRTC". The guest would have to be reachable over a virtual NIC from WebView2, which conflicts with the no-network-listener rule on the host side. Selkies' default WebSocket transport is tunnellable like noVNC.
- WebView2 cannot open `ws://` to a custom scheme, so any `ws://`-based client (noVNC URL mode, xterm attach addon, Selkies web client) would need a loopback TCP listener. That is disallowed by the project rules, so every stream must be adapted to the app's own message channel.
- Bandwidth sanity: JPEG screencast at 1280x800, quality ~60, 10–15 fps is likely a few Mbit/s, which is far below what a memory-copy transport can carry. The hvsock "not the best for video" caveat matters mainly for high-bitrate raw frames. This is an estimate, not a measurement.

### Gaps
- No published end-to-end latency or CPU cost comparison (noVNC vs WebRTC vs CDP screencast) was found. The only quantitative claim is Selkies' ≥60 fps at 1080p.
- Whether noVNC accepts an arbitrary duck-typed channel object (beyond `WebSocket`/`RTCDataChannel`) was not verified.
- Selkies' WebSocket-mode encoders and CPU usage without a GPU are not documented in the README.

---

## 4. Terminal: guest PTY streamed to the host (xterm.js), tmux for persistence

### Takeaway
The proven pattern is a guest-side PTY owned by the guest agent. Output is streamed as server-streaming RPC events (envd `Start`/`Connect` with `DataEvent.pty`) and input arrives via unary/client-streaming calls plus a resize RPC. On the host it is rendered with xterm.js (`@xterm/xterm`, webgl renderer, serialize addon for snapshot-on-reattach). tmux control mode (`-CC`) adds structured, flow-controlled pane output if sessions must survive guest-agent restarts.

### Cited Findings
- envd PTY model:
  - `Start` (server-stream) returns `StartEvent{pid}`, then `DataEvent{pty bytes}`, then `EndEvent{exit_code}` with `KeepAlive`s.
  - `Connect` reattaches by `pid` or `tag`.
  - `Update` changes PTY `cols`/`rows`.
  - `SendInput`/`StreamInput` carry keystrokes.
  - PTY processes run in a dedicated `ptys` cgroup with higher CPU weight (200) than user processes (50). — [process.proto](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/spec/process/process.proto); [envd main.go](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/main.go)
- Daytona PTY sessions are created with an ID, cols/rows and an output callback. They can be reattached over WebSocket (`connect_pty_session`). — [Daytona PTY docs](https://www.daytona.io/docs/pty/); [rubydoc Daytona::Process](https://www.rubydoc.info/gems/daytona/Daytona/Process)
- xterm.js is frontend-only ("must be connected to a process through a library such as node-pty"), with a zero-dependency core and package `@xterm/xterm`. Official addons include `addon-attach` (WebSocket to a server process), `addon-fit`, `addon-serialize` (buffer to VT sequences or HTML), `addon-webgl` (WebGL2 renderer), `addon-search`, `addon-unicode11`, `addon-web-links`, `addon-clipboard`, `addon-image` and `addon-progress` (OSC 9;4). Users include VS Code, Tabby, Hyper and JupyterLab. — [xtermjs/xterm.js](https://github.com/xtermjs/xterm.js)
- tmux control mode:
  - `-C` (canonical, for testing) or `-CC` (emits DCS `\033P1000p`). Replies are wrapped in `%begin` … `%end`/`%error`.
  - Async notifications include `%output <pane> <escaped>`, `%window-add`, `%session-changed` and `%exit`.
  - Flow control is enabled via `refresh-client -f pause-after=<s>`. tmux then sends `%pause`, and the client resumes with `refresh-client -A '%0:continue'`. `%extended-output` adds lag in ms. `capture-pane` refreshes a paused pane.
  - It was designed for iTerm2. — [tmux wiki: Control Mode](https://github.com/tmux/tmux/wiki/Control-Mode)

### Inferences
- For Orole, the in-guest agent owns PTYs, keyed by a stable tag. The host daemon holds one multiplexed vsock/hvsock connection per VM and fans PTY events to the UI over existing MyGo events. Use xterm.js without `addon-attach`, because it is WebSocket-based and would need a listener, and feed `term.write()` from IPC events instead.
- Persistence layers:
  - The guest PTY survives GUI restarts and host-daemon restarts as long as the VM keeps running. It does not survive a VM stop unless the VM is snapshotted with memory. This mirrors the existing AGENTS.md rule ("survive GUI restarts, not daemon exit or reboot") one level down, and the UI copy should say so.
  - A bounded ring buffer in the guest agent, or `addon-serialize` snapshots on the host, provides scrollback on reattach.
  - tmux (`new-session -d`, then control-mode attach) is worth adding only if the guest agent itself must be restartable/upgradable without killing shells (envd solves this with a live `/upgrade` handover instead).
- Use tmux control mode's `pause-after` or envd-style keepalives to stop a chatty process (e.g. `yes`) from flooding the IPC channel and the WebView. Rate-limit or coalesce PTY chunks on the host before emitting UI events.

### Gaps
- No measured latency figures for PTY over vsock/hvsock were found. They are expected to be sub-millisecond transport-wise, but this is unverified.
- Whether WebView2's xterm.js webgl renderer has any WebView2-specific issues was not researched.

---

## 5. Host<->guest transport without TCP listeners: hvsock (AF_HYPERV), vsock, serial, named pipes; Go libraries

### Takeaway
On Windows/Hyper-V (including WSL2-style utility VMs), use Hyper-V sockets. The host uses `AF_HYPERV` via `github.com/Microsoft/go-winio` (`ListenHvsock`, `HvsockDialer`, `VsockServiceID(port)`). The Linux guest uses plain `AF_VSOCK` via `github.com/mdlayher/vsock`, with vsock port N mapping to service GUID `N-facb-11e6-bd58-64006a7986d3`. Traffic "runs without using networking". The catch is that service GUIDs must be registered under HKLM by an administrator, which suits a one-time installer step. On KVM/Firecracker the same guest code works unchanged against virtio-vsock. Note that Firecracker resets vsock connections on snapshot.

### Cited Findings
- **Hyper-V sockets:**
  - Available since Windows 10 Anniversary Update and Server 2016. "All communication over Hyper-V sockets runs without using networking and all data stays on the same physical memory."
  - Linux guests need `CONFIG_VSOCKET=y` and `CONFIG_HYPERV_VSOCKETS=y`.
  - Capabilities: "Data stream only", "No block memory (not the best for backup/video)". — [Microsoft: Make your own integration services](https://learn.microsoft.com/en-us/virtualization/hyper-v-on-windows/user-guide/make-integration-service)
- **Registration:**
  - "the application must be registered with the Hyper-V Host's registry" under `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Virtualization\GuestCommunicationServices\<GUID>` with `ElementName`. The sample PowerShell "must be run as administrator".
  - Linux guests address by `svm_cid`/`svm_port`. The host uses template GUID `00000000-facb-11e6-bd58-64006a7986d3` with the first field replaced by the port (e.g. `00000ac9` = port 2761).
  - Sockets are created with `socket(AF_HYPERV, SOCK_STREAM, HV_PROTOCOL_RAW)` on Windows and `socket(AF_VSOCK, SOCK_STREAM, 0)` in the Linux guest. — [Microsoft doc](https://learn.microsoft.com/en-us/virtualization/hyper-v-on-windows/user-guide/make-integration-service)
- **VMID wildcards:** `HV_GUID_WILDCARD`/`ZERO` (all partitions), `HV_GUID_CHILDREN` `90db8b89-0d35-4f79-8ce9-49ea0ac8b7cd` (listen for child VMs), `HV_GUID_LOOPBACK` `e0e16197-...`, `HV_GUID_PARENT` `a42e7cda-...` (connect to host from guest). Socket option `HVSOCKET_CONNECTED_SUSPEND`: "sockets do not disconnect when the virtual machine is paused". — [Microsoft doc](https://learn.microsoft.com/en-us/virtualization/hyper-v-on-windows/user-guide/make-integration-service)
- **go-winio hvsock API:**
  - `HvsockAddr{VMID, ServiceID guid.GUID}`, `ListenHvsock(addr)` → `HvsockListener`.
  - `HvsockConn` with `CloseRead`/`CloseWrite`/deadlines.
  - `HvsockDialer{Deadline, Retries, RetryWait}.Dial(ctx, addr)`, package-level `Dial`.
  - `VsockServiceID(port uint32) guid.GUID`.
  - `HvsockGUIDWildcard()`, `…Broadcast()`, `…Loopback()`, `…SiloHost()`, `…Children()`, `…Parent()`. — [go-winio hvsock.go](https://raw.githubusercontent.com/microsoft/go-winio/main/hvsock.go)
- go-winio also covers named pipes and Hyper-V sockets as package areas. pkg.go.dev listed v0.6.4, published 2026-10-13, which was not cross-checked against GitHub releases. — [pkg.go.dev go-winio](https://pkg.go.dev/github.com/Microsoft/go-winio)
- **mdlayher/vsock:**
  - Linux `AF_VSOCK`. API: `Listen(port, cfg)`, `ListenContextID`, `Dial(contextID, port, cfg)`, `FileListener`, `net.Conn`/`net.Listener`.
  - Well-known CIDs: `Hypervisor` 0, `Local` 1, `Host` 2. `ContextID()` doubles as a capability probe.
  - pkg.go.dev shows v1.3.0, published 2026-05-26. — [pkg.go.dev mdlayher/vsock](https://pkg.go.dev/github.com/mdlayher/vsock)
- **linuxkit/virtsock:**
  - Older library. It provides `pkg/hvsock` and `pkg/vsock` plus `vsudd` (Unix-socket↔virtsock proxy used in Docker for Mac/Windows).
  - On Windows "the server currently runs only inside the VM, and the host connects to it". With the server on the host and a Linux client, "unidirectional `shutdown()` doesn't work properly" and clients may get unexpected EOFs.
  - Support for Windows builds before 14393 was dropped. — [linuxkit/virtsock](https://github.com/linuxkit/virtsock)
- **Firecracker vsock across snapshots:** "Open vsock connections are closed on snapshot", listen sockets survive, and the guest gets `VIRTIO_VSOCK_EVENT_TRANSPORT_RESET`. On resume, listeners get their CID updated. — [Firecracker snapshot-support.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md)
- **WSL precedent:** WSL's default Windows-drive transport is "Plan 9 over a Hyper-V socket". — [boxofcables.dev (2026-05-31)](https://boxofcables.dev/wsl2-per-device-swiotlb-pools-for-virtiofs-and-virtioproxy)

### Inferences
- **Topology:**
  - **Guest:** the agent listens on `AF_VSOCK` port P (mdlayher/vsock `Listen`).
  - **Host:** the Go daemon dials `HvsockAddr{VMID: <vm id>, ServiceID: VsockServiceID(P)}` with `HvsockDialer` retries while the VM boots.
  - This direction (guest listens, host dials) matches virtsock's warning about host-side servers. Because the hvsock is addressed by VMID, nothing can reach it from the LAN.
  - For guest→host calls (e.g. the egress proxy in section 7), the host listens on `HvsockGUIDChildren()` or a specific VMID with a registered service GUID. That is still not an IP listener.
- **Registration:** register a small fixed set of service GUIDs (agent RPC, egress proxy, file service, VNC/screencast) once from the elevated NSIS installer. Runtime then needs no admin rights. Validate the peer VMID on accept, because wildcard/children listeners accept any child partition.
- **Multiplexing:** run one hvsock connection per VM carrying a multiplexer (HTTP/2 via ConnectRPC, or yamux/smux-style streams) so PTYs, screencast frames, file transfers and CDP don't each need a registered GUID. Give video/screencast its own stream or connection to avoid head-of-line blocking of PTY keystrokes.
- **Pause/snapshot:** set `HVSOCKET_CONNECTED_SUSPEND` on host sockets if Orole pauses Hyper-V VMs while idle. Design the protocol to resume after reconnect anyway, because Firecracker drops vsock connections on snapshot and other hypervisors may too.
- **Other transports:** named pipes (go-winio) remain the transport between Orole's GUI process and the daemon, unchanged. A serial console (Hyper-V COM port → named pipe) is a useful out-of-band channel for boot logs and emergency shell, not for data.

### Gaps
- How a non-Hyper-V-Manager VM on Windows (e.g. one created through HCS/hcsshim or the Windows Hypervisor Platform as used by Docker Sandboxes) exposes vsock/hvsock was not researched here. Hyper-V sockets require a Hyper-V partition, and WHP-based custom VMMs may need their own virtio-vsock backend.
- No throughput or latency benchmarks for hvsock were retrieved (virtsock's `data/` directory has benchmark data but was not read).
- Whether registering GUIDs is needed for WSL utility VMs specifically (WSL registers its own) was not checked.

---

## 6. Files: virtiofs, 9p/Plan9 (WSL), SMB, copy-in/copy-out APIs; read-only host folders; performance

### Takeaway
Shared filesystems over 9p are convenient but slow for small-file workloads. One July 2026 WSL measurement found `/mnt/c` (9p) about 7x slower than ext4 for sequential writes and about 58x slower per small-file create/delete. virtiofs (FUSE-based, with an optional DAX window) is designed for "local file system semantics and performance" and is still opt-in/experimental in WSL (`virtiofs=true`). For agent VMs the safest default is to keep the workspace on the VM's own disk and move files with an explicit copy-in/copy-out API (as E2B `/files` and Daytona `/files/*` do). A share is reserved for an explicitly granted, read-only host folder.

### Cited Findings
- **virtio-fs:**
  - "lets virtual machines access a directory tree on the host" and "is designed to offer local file system semantics and performance".
  - 9p-style systems "are based on existing network protocols" not optimized for virtualization.
  - It is built on FUSE with host daemon `virtiofsd` (the Rust `virtiofsd-rs` is the active one).
  - The experimental DAX window maps file contents to host page cache, bypassing the guest page cache.
  - Support is listed for QEMU 5.0, libvirt 6.2, Linux 5.4 and Kata 1.7. No numeric benchmarks are given. — [virtio-fs.gitlab.io](https://virtio-fs.gitlab.io/)
- **WSL:**
  - `virtiofs` (default `false`) is "An experimental setting to use VirtioFS for Windows filesystem shares. Requires `virtio` and `hostFileSystemAccess`".
  - `[automount] enabled` mounts drives via DrvFs. `[interop] enabled=false` blocks launching Windows processes. `defaultVhdSize` defaults to 1 TB. — [WSL config docs (updated 2026-09-16)](https://learn.microsoft.com/en-us/windows/wsl/wsl-config)
  - "Virtiofs remains opt-in for now: the default transport is still Plan 9 over a Hyper-V socket."
  - PR #40654 (merged 2026-05-27) gives each virtio device its own SWIOTLB DMA pool. It requires kernel 6.18.26.3-1, and the author advises keeping WSL RAM above 1 GB for the pool's 64 MB headroom. — [boxofcables.dev](https://boxofcables.dev/wsl2-per-device-swiotlb-pools-for-virtiofs-and-virtioproxy)
- **9p performance (personal benchmark, July 2026):**
  - 1 GiB sequential write to `/mnt/c` ran at about 125 MB/s vs about 850 MB/s on the ext4 home directory, on the same physical disk.
  - Small-file create+delete took about 5.8 ms vs about 0.1 ms per op.
  - 9p `msize=65536` bounds each operation. — [brainwagon.org (2026-07-11)](https://brainwagon.org/blog/2026_07_11_wsl2_filesystem_speed) (via search summary)
- **Copy APIs:**
  - E2B `POST /files` (multipart or `application/octet-stream`, parent dirs auto-created, `X-Metadata-*` → xattrs), `GET /files` download, presigned via `signature`/`signature_expiration`, and `POST /files/compose` for zero-copy chunk concatenation. — [envd.yaml](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/spec/envd.yaml)
  - envd also has a `filesystem` ConnectRPC service. — [envd main.go](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/main.go)
  - Daytona exposes `/files/*` for read/write/search/manage. — [Daytona Toolbox overview (mirror)](https://mintlify.wiki/daytonaio/daytona/api/toolbox/overview)

### Inferences
- **Default for Orole agent VMs:**
  - The workspace lives on the VM's own ext4 disk (fast, isolated, snapshot-friendly).
  - Copy-in/copy-out uses a streaming file RPC over the hvsock channel, mirroring E2B's `/files` + `/files/compose` (chunked upload with resume).
  - Results surface to the user through an explicit "export" action.
- **Read-only host folder grants:** the exported directory is chosen by the user, and the policy is decided Go-side. Prefer a host-side file server that enforces read-only and path confinement itself (a 9p server over hvsock, as WSL does, or a virtiofs daemon with read-only export). Do not rely on a guest mount flag, because a compromised guest can remount. Guest-side `ro` mount options are defence-in-depth only.
- **SMB:** not recommended. It needs the guest to reach a host SMB listener over the virtual network, which conflicts with "no network listeners" and with blocking host services (section 7).
- **Build-heavy work:** for `node_modules`/`.git`-style workloads, copy the repo into the VM rather than mounting it. The 9p numbers show small-file I/O is the main penalty.

### Gaps
- No head-to-head virtiofs vs 9p benchmark (WSL or QEMU) with numbers was found.
- Read-only export options for `virtiofsd` and for WSL's Plan 9 server were not confirmed.
- No Go 9p or virtiofs server library was evaluated in this session.

---

## 7. Guest networking and egress control: NAT, per-VM allowlist via host proxy, blocking host LAN/services, DNS

### Takeaway
The production pattern in 2025–2026 agent sandboxes is "no direct network, all egress through a host-side allowlisting proxy".
- **Anthropic sandbox-runtime:** removes the network namespace and reaches HTTP/SOCKS5 proxies over bind-mounted Unix sockets.
- **Docker Sandboxes:** each agent gets a microVM, with HTTP/HTTPS filtering and credential injection at a proxy.

For Orole, the equivalent is a guest with no NIC (or a NIC with no route) plus an in-guest forwarder to a host proxy over hvsock. DNS resolution happens at the proxy. This blocks host LAN and host services by construction.

### Cited Findings
- **Anthropic sandbox-runtime (`srt`, `@anthropic-ai/sandbox-runtime`):**
  - Network is denied by default. An empty `allowedDomains` means no network.
  - Traffic is forced through host-side HTTP and SOCKS5 proxies.
  - On Linux the sandboxed process's network namespace is removed and proxies are reached via bind-mounted Unix sockets. macOS Seatbelt allows only the proxy's localhost port.
  - A third-party write-up says Windows uses a machine-wide WFP filter for a dedicated sandbox account, allowing only loopback to the proxy port range.
  - It is labelled a beta research preview. — [anthropic-experimental/sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime); [rywalker.com analysis](https://rywalker.com/research/anthropic-sandbox-runtime)
- **Docker Sandboxes:**
  - Each agent runs in its own microVM with a private Docker daemon. "Network filtering adds an additional control layer for HTTP/HTTPS traffic".
  - Docker uses a custom VMM (not Firecracker): Hypervisor.framework on macOS, Windows Hypervisor Platform on Windows, KVM on Linux.
  - Windows support is listed as "Hyper-V Experimental" (architecture page) vs "Early Access" with Windows 11 + `HypervisorPlatform` feature (get-started page).
  - The Desktop-integrated `docker sandbox` CLI is deprecated in favour of standalone `sbx`.
  - A third-party summary says the proxy also injects credentials so the agent never sees secrets. — [Docker Sandboxes architecture](https://docs.docker.com/ai/sandboxes/architecture); [Get started](https://docs.docker.com/ai/sandboxes/get-started/); [Docker blog](https://www.docker.com/blog/untrusted-autonomous-workload-ai-sandboxes); [ostechnix](https://ostechnix.com/docker-sandboxes-introduction/)
- **WSL networking knobs (a reference for Hyper-V utility VMs):**
  - `networkingMode` values: `none` ("WSL network is disconnected"), `nat` (default), `bridged` (deprecated since 2.4.5), `mirrored`, `consomme` ("called `virtioproxy` in the past"; NAT falls back to Consomme since 2.3.25).
  - `firewall=true` applies Windows Firewall and Hyper-V-specific rules to WSL traffic.
  - `dnsTunneling=true` "uses a virtualization feature to answer DNS requests from within WSL, instead of requesting them over a networking packet", with resolver `10.255.255.254`.
  - `autoProxy` pushes the Windows HTTP proxy into Linux.
  - `localhostForwarding` (default true) makes guest-bound ports reachable from host `localhost`. — [WSL config](https://learn.microsoft.com/en-us/windows/wsl/wsl-config); [WSL networking](https://learn.microsoft.com/en-us/windows/wsl/networking)
- **Hyper-V firewall:** on by default for WSL ≥2.0.9 on Windows 11 22H2+. It is configurable per VM creator ID, e.g. `Set-NetFirewallHyperVVMSetting -Name '{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}' -DefaultInboundAction Allow` and `New-NetFirewallHyperVRule ... -VMCreatorId`. In NAT mode the guest reaches the host via the default-gateway IP, so host services bound to `0.0.0.0` are reachable from the guest unless firewalled. — [WSL networking](https://learn.microsoft.com/en-us/windows/wsl/networking)
- Playwright MCP has browser-level `--proxy-server`, `--allowed-origins`, `--blocked-origins` and `--allowed-hosts`. — [microsoft/playwright-mcp](https://github.com/microsoft/playwright-mcp)

### Inferences
- **Recommended Orole design:**
  - **(1)** Attach no virtual NIC to agent VMs (the equivalent of `networkingMode=none`), so the guest has no route to the host LAN, the host's own IPs, or link-local metadata addresses.
  - **(2)** The guest agent runs a tiny HTTP CONNECT/SOCKS5 forwarder on guest `127.0.0.1:3128`, exported via `HTTP(S)_PROXY` and Chrome `--proxy-server`. It tunnels each connection over hvsock to the host daemon.
  - **(3)** The host daemon runs the policy proxy. It resolves DNS itself, so there is no guest DNS and no DNS exfiltration path. It enforces a per-VM domain/port allowlist (decided Go-side, logged) and refuses private, loopback and link-local destinations after resolution, to defeat DNS rebinding to the LAN. It can optionally inject credentials (Docker-style) so secrets stay in Credential Manager.
  - This mirrors `srt` (no netns + Unix-socket proxies) with hvsock in place of Unix sockets. It never opens an IP listener on the host.
- **If a NIC is unavoidable** (e.g. non-HTTP protocols): use an internal-only switch/NAT with Hyper-V firewall rules (`New-NetFirewallHyperVRule`) that block the host's subnet and the gateway's service ports. This is weaker, because UDP/QUIC and raw sockets bypass an HTTP proxy, so the allowlist must also be enforced at L3/L4.
- **QUIC/HTTP3:** Chrome will fall back to TCP when UDP is unavailable, so the no-NIC design naturally forces all browser traffic through the proxy. HTTPS remains opaque (CONNECT by SNI/host) unless TLS interception is added, which is not recommended. Allowlisting operates on hostnames.

### Gaps
- The Docker Sandboxes network-policy syntax and implementation (where the proxy runs, how it is wired into the microVM) were not retrieved.
- The `srt` Windows/WFP details come only from a third-party write-up.
- Hyper-V firewall behaviour for non-WSL VMs (custom VMCreatorId) was not verified.

---

## 8. Lifecycle and resources: warm pools, snapshot/restore, idle suspend, memory caps, disk quota, GC, RAM per Chromium VM, concurrency on 16–32 GB laptops

### Takeaway
MicroVM boot is cheap: Firecracker specifies ≤125 ms to `/sbin/init` and ≤5 MiB VMM overhead. Snapshot restore is lazy (mmap + page faults/UFFD), but has security caveats for clones (VMGenID, reseed).
- **E2B:** pause costs about 4 s per GiB of RAM and resume about 1 s. envd freezes cgroups and collapses heap to hugepages before pause.
- **Budget:** realistic per-agent VM budget with headful Chromium is about 1–2 GiB (guest OS + Xvfb/VNC + Chrome at 60–180 MB idle and ~1 GB peak on heavy sites). By inference, a 16 GB laptop sustains about 3–5 concurrent browser-agent VMs and a 32 GB laptop about 10–14, assuming idle VMs are suspended or ballooned.

### Cited Findings
- **Firecracker spec** (1 vCPU / 128 MiB, M5D/M6G metal):
  - VMM startup in 8 CPU ms (wall about 12 ms, range 6–60 ms).
  - Boot ≤125 ms from `InstanceStart` to `/sbin/init` (minimal kernel/rootfs, serial off).
  - VMM memory overhead ≤5 MiB (excl. MMDS, can exceed with many vsock connections).
  - Network up to 14.5 Gbps at ≤80% of a core, +~0.06 ms latency. Storage up to 1 GiB/s. — [Firecracker SPECIFICATION.md](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md)
- **Firecracker snapshots:**
  - Full vs diff snapshots (diff is in developer preview). Memory is restored by `MAP_PRIVATE` mmap of the memory file with on-demand loading; writes are CoW. Backends are `File` or `Uffd` (userspace page-fault handler).
  - No restore latency number is given ("very fast"). Restore is slow with cgroups v1, so use v2.
  - The memory file must stay immutable for the VM's lifetime.
  - The wall clock continues from snapshot time.
  - Cloning a snapshot replicates RNG state, tokens and IDs, and "resuming the same snapshot more than once is considered insecure". VMGenID causes Linux ≥5.18 to reseed, but app-level state needs its own handling.
  - Snapshots can consume a lot of disk, so integrators must enforce quotas. — [Firecracker snapshot-support.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md)
- **E2B persistence:**
  - Pause takes about 4 s per GiB of RAM and resume about 1 s. Pause saves filesystem + memory, so running processes survive.
  - `keepMemory: false` gives a filesystem-only pause with cold boot on resume.
  - Paused sandboxes are now kept indefinitely. Older docs said 30 days/beta, which is outdated. — [E2B persistence docs](https://e2b.dev/docs/sandbox/persistence); [Blaxel analysis](https://blaxel.ai/blog/e2b-session-limit)
- envd pre-pause hooks are `/freeze` (cgroup freeze), `/collapse` (hugepage collapse to shrink snapshot work), `/fsfreeze` for filesystem-only pause, and `/init` to resync clock after resume. — [envd.yaml](https://raw.githubusercontent.com/e2b-dev/infra/main/packages/envd/spec/envd.yaml)
- **WSL resource knobs** (Hyper-V utility VM precedent):
  - `memory` defaults to 50% of host RAM and `swap` to 25% of host RAM.
  - `vmIdleTimeout` defaults to 60000 ms and `[general] instanceIdleTimeout` to 15000 ms.
  - `[experimental] autoMemoryReclaim` = `disabled|gradual|dropCache` (doc lists default `dropCache`). `sparseVhd` makes new VHDs sparse. `defaultVhdSize` defaults to 1 TB and caps disk growth.
  - `isolateDistroCgroup=true` gives per-distro cgroups. — [WSL config (updated 2026-09-16)](https://learn.microsoft.com/en-us/windows/wsl/wsl-config)
- **Hyper-V socket survival:** `HVSOCKET_CONNECTED_SUSPEND` keeps sockets connected while a VM is paused. — [Microsoft hvsocket doc](https://learn.microsoft.com/en-us/virtualization/hyper-v-on-windows/user-guide/make-integration-service)
- **Chromium memory:** about 60 MB idle (low-memory mode) / 180 MB (default), about 940 MB peak and about 130 MB after cleanup on Amazon.com, headless. — [PyBA performance](https://pyba.readthedocs.io/en/latest/performance.html)
- **Docker Sandboxes** precedent for local per-agent microVMs on Windows: requires Windows 11 + Windows Hypervisor Platform and uses a custom VMM. — [Docker Sandboxes get started](https://docs.docker.com/ai/sandboxes/get-started/); [Docker blog](https://www.docker.com/blog/untrusted-autonomous-workload-ai-sandboxes)

### Inferences
- **Per-VM RAM budget (estimate, not measured):**
  - Minimal guest kernel+init+agent: about 100–200 MB.
  - Xvfb/cage + VNC server: about 50–100 MB.
  - Headful Chrome: 200 MB to 1 GB+ depending on sites.
  - A hard cap of 2 GiB with a balloon/free-page-reporting floor of about 768 MB–1 GiB when idle is a reasonable starting point. Reserve guest headroom for the agent daemon envd-style (`min(MemTotal/8, 128 MiB)`) so a runaway tab OOMs before the agent does.
- **Concurrency (estimate):**
  - Windows + WebView2 app + host daemon need about 5–7 GB on a 16 GB laptop. That leaves about 8–10 GB, i.e. about 4–5 active browser VMs at 2 GiB caps. Fewer if pages are heavy, more if idle VMs are paused.
  - On 32 GB, about 10–14 active.
  - Paused/snapshotted VMs cost disk, not RAM. E2B's about 4 s/GiB pause cost suggests idle-suspend after a few minutes rather than seconds.
- **Lifecycle policy for Orole:**
  - **(1) Warm pool:** keep 1–2 pre-booted VMs from a golden image, then hand one out and boot a replacement. With a booted-snapshot template, restore instead of boot, but follow Firecracker's clone guidance: reseed RNG, regenerate agent tokens and machine-id, and resync clock via `/init` after every restore.
  - **(2) Idle suspend:** freeze workload cgroups, pause the VM (Hyper-V save state, or Firecracker snapshot), and persist with memory. Fall back to filesystem-only after N hours.
  - **(3) Disk:** use a differencing/overlay disk per VM on a base image, a per-VM size cap (cf. WSL `defaultVhdSize`), sparse VHDX, and GC of overlays and snapshots by last-used time with a global quota.
  - **(4) GC:** reconcile on daemon start (orphaned VMs, snapshots, hvsock registrations).
  - **(5) Surface in the UI** that paused agents resume with the browser and terminals intact, but a VM stop or host reboot without a memory snapshot loses running processes.
- Firecracker numbers are Linux/KVM-only. On Windows, Hyper-V VM boot and save/restore are slower and less documented, so treat sub-second figures as unattainable on Windows hosts until measured.

### Gaps
- No authoritative measurement of Hyper-V (or Windows Hypervisor Platform) small-VM boot time, save/restore latency, or per-VM overhead on Windows 11 was found in this session.
- No published per-VM RAM figures for E2B Desktop, Cua or OpenHands sandboxes running Chromium were found.
- Firecracker gives no restore latency figure.
- Concurrency numbers above are reasoning, not benchmarks. They must be validated on target hardware (sum guest RSS and host-side VM working sets under a realistic browsing mix, and report p95).
