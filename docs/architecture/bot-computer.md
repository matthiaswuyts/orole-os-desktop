# Bot computer: a local VM per bot

Every chat bot can have its own small Linux computer on the user's PC: its own disk, a terminal and (later) a
visible browser. Design: `docs/superpowers/specs/2026-10-15-bot-computer-design.md`. Plans:
`docs/superpowers/plans/2026-10-15-bot-computer*.md`.

**Status: phase 1 (sandbox core and guest agent).** The manager, store, guest protocol and `orole-guest` exist
and are tested with a fake backend. Nothing starts a real VM yet: the Windows QEMU backend, runtime download and
setup come in phase 2; bot tools in phase 3; the Computer tab in phase 4.

**A bot's computer lives in the daemon: it survives GUI restarts, not a daemon exit or reboot. Its disk persists;
running commands do not.**

## Architecture

```
 bot tool call (phase 3)
        |
 internal/sandbox.Manager ---- Store (table sandboxes)
        |  Ensure(bot) -> *Guest
        v
 Backend.Boot (phase 2: QEMU + WHPX) ---> VM.Conn(): one byte stream (virtio-serial <-> AF_UNIX socket / pipe)
        |                                          |
 Guest (h2c client, golang.org/x/net/http2)  <---->  orole-guest (h2c server) in the Linux guest
                                                     internal/sandbox/guestagent
```

| Unit | Location | Responsibility |
|---|---|---|
| Types, paths | `internal/sandbox/sandbox.go`, `paths.go` | `Limits` (2048 MB, 2 vCPU, 10 GB by default), `State`, `Status`, `Availability`, errors; files under `<data>\sandbox\` |
| Backend | `internal/sandbox/backend.go`, `backend_windows.go`, `qmp.go` | `Backend.Boot(BootSpec) -> VM`; `VM.Conn/Shutdown/Kill/Done/ExitErr`. Windows: `qemu-system-x86_64 -accel whpx,kernel-irqchip=off` in the daemon's kill-on-close job object; QMP `system_powerdown`, then kill. Other platforms: `unsupported-os`. |
| Store | `internal/sandbox/store.go`, `migrations/0112_sandboxes.sql` | one row per bot: image version, overlay path, limits, last use |
| Manager | `internal/sandbox/manager*.go` | lifecycle, cap, idle stop, crash detection, takeover lock, delete |
| Guest client | `internal/sandbox/guestclient*.go` | typed calls over h2c; caps every answer; maps a dead channel to the manager's reason |
| Wire | `internal/sandbox/wire` | JSON types and limits shared with the guest |
| Guest agent | `internal/sandbox/guestagent`, `cmd/orole-guest` | the handler (OS-neutral, run in-process by host tests) and the Linux main serving it on `/dev/virtio-ports/org.orole.guest.0` |

### Lifecycle

- `Ensure(bot)` boots on demand. Concurrent callers share one boot (single flight); the boot runs with its own
  60 s timeout, independent of the first caller's context. A boot is refused with "the computer isn't set up yet"
  when the backend is not available or no image is installed, and with "not enough free memory" when free RAM is
  below the VM's RAM + 1024 MB. It then records the sandbox, boots, and waits until the guest's `/health` says
  ready. A boot that fails or times out kills the VM, writes a line to `logs\sandbox-<bot>.log` and reports
  "the computer failed to start"; there is no retry loop, the next call tries again.
- At most 4 VMs run. When a fifth is needed the least recently used idle VM is stopped first. Idle means no guest
  request in flight, no viewer and no takeover. When every VM is busy, `Ensure` waits up to 30 s for
  one to become idle, then fails with "all computers are busy".
- A VM idle for 15 min (no bot activity, no viewer, no takeover) is powered off (`/poweroff`, then the backend's
  shutdown, then kill). The overlay disk stays.
  An open terminal does not keep the VM alive by itself: only bytes moving on it (at most once per second) count
  as activity, so a forgotten, silent terminal is stopped with the VM and its session ends.
- A VM that exits on its own is a crash: calls in flight fail with "the computer stopped unexpectedly", the
  status becomes off with that error, the takeover lock is dropped, and the next call boots again.
- Restarting `orole-guest` inside the VM makes the host treat the channel as dead; the next call reboots the VM.
- A VM that is stopping keeps its overlay open, so `Ensure` waits for it to be gone before booting again.
- `TakeOver`/`Release` hold the takeover lock in the daemon; while it is held the VM is never idle-stopped or
  evicted, and (phase 3) bot browser tools are refused.
- `Delete(bot)` stops the VM (calls in flight fail), then removes the overlay, the socket and the record. The log
  is kept.
- `Shutdown` (daemon exit) stops every VM and refuses new ones.

### Guest protocol

HTTP/2 without TLS (prior knowledge) over the VM's one byte stream; base URL `http://guest`. Every request
carries `X-Orole-Token`, a 32-byte random hex token generated per boot, passed to the guest through QEMU fw_cfg
(`opt/orole/token`, from a user-only file that is deleted once QEMU has read it) and otherwise held only in daemon
memory; the guest answers 401 otherwise.

| Route | Purpose | Limits |
|---|---|---|
| `GET /health` | version and readiness | |
| `POST /exec` | `/bin/sh -lc <command>` as `bot`, in its own process group (host tests: `/bin/sh -c` as the agent's user) | timeout 120 s default, 600 s max; stdout+stderr 1 MiB kept, the rest dropped (`truncated`); background children get 2 s to release the output |
| `POST /files/read`, `/files/write` | guest files; relative paths under `/home/bot`; files and directories a write creates belong to `bot` | read 4 MiB (`truncated`), write 16 MiB max |
| `POST /pty?cols=&rows=` | login shell (bash) as `bot` on a pseudo-terminal it owns, full duplex; `X-Orole-Pty` id, `X-Orole-Exit` trailer | |
| `POST /pty/resize` | resize by id | |
| `POST /screen`, `POST /browser` | 501 until phases 3-4 | |
| `POST /poweroff` | 202, then a clean power off | |

The host never trusts the guest: answers over 8 MiB are refused, exec output is cut to 1 MiB and file content to
4 MiB again on the host, and everything a bot receives from the guest is untrusted data (phase 3 wraps it).

### Storage

`<data>\sandbox\runtime\<version>\` (QEMU), `images\<version>\` (base image), `bots\<bot>.qcow2` (overlay),
`run\<16 hex>.sock` (guest channel; the name is a hash of the bot id so the path stays under the 107-character AF_UNIX limit) with `.qmp` (QEMU monitor) and `.tok` (the per-boot token, only while QEMU starts) next to it, `<data>\logs\sandbox-<bot>.log`.
The overlay is created with `qemu-img create -F qcow2 -b <base>` on first boot only, never recreated, and a bot keeps
booting the kernel, initrd, cmdline and base of the image version recorded for it while that version is installed; when it is
gone the boot fails with `ErrImageGone` instead of booting the overlay against another base. Bot ids are checked against
`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$` before they become file names.

### Security notes

- No network listener on the host; the guest channel is a local stream. QEMU connects out to two AF_UNIX sockets the
  daemon listens on (guest channel and QMP) and opens no listener of its own.
- `<data>\sandbox\run\` gets an explicit protected DACL (the user and SYSTEM only), once per daemon run, because
  `%LOCALAPPDATA%\Orole` inherits read access for other local groups; the sockets inherit it. The token reaches QEMU
  in a file there with its own user-only DACL (`-fw_cfg name=opt/orole/token,file=`), never in argv; the file is
  deleted once QMP answers a command that is only allowed after machine initialization (QEMU has read its fw_cfg
  files by then) and on every failed boot, and the token is never logged.
- QEMU runs in a job object with kill-on-close, so VMs never outlive the daemon.
- The VM is the security boundary. The token authenticates the daemon's connection to the guest. In the guest,
  `orole-guest` runs as root and is the only process that can read the token (fw_cfg sysfs files are 0400 root)
  or open the channel (`/dev/virtio-ports/org.orole.guest.0` is 0600 root by a udev rule); commands and terminals
  run as the unprivileged `bot`, which has no sudo. `/files/read` and `/files/write` still run as root (paths are not
  confined), so a bot can read the token through them; acceptable, because the guest is already the bot's.
- Inside the guest, nftables rejects everything to 10.0.2.2 and fec0::2 (QEMU user networking's aliases for the
  host's loopback) except DHCP renewals from port 68, which only root can bind.
- The guest has open internet and can reach the home network (phase 2 documents this in the UI).

### Guest image

`guest/flake.nix` builds a NixOS system (nixpkgs pinned in `guest/flake.lock`, version in `guest/VERSION`) into
`vmlinuz`, `initrd`, `cmdline` (`init=<toplevel>/init` plus kernel parameters; the host appends `console=ttyS0
panic=-1`), `base.qcow2` (compressed qcow2, ext4 labelled `nixos` without a partition table, grown to the overlay's
size on boot) and `SHA256SUMS`. `nix build ./guest#image-debug` is the same with root autologin on the serial console.
Building it: `docs/development/guest-image.md`.

Facts later phases rely on: user `bot` (uid 1000, home `/home/bot`, no sudo, locked password); Chromium in cage
(wlroots headless backend, pixman) with CDP on `127.0.0.1:9222` and its profile in `/home/bot/.config/orole-chromium`;
wayvnc on `127.0.0.1:5900`; `orole-guest` as a root systemd unit after module loading, restarted forever;
`qemu_fw_cfg` loaded in the initrd; Python 3 + uv, Node.js, git, curl, jq, ripgrep; nix-ld and envfs so downloaded
Linux binaries and `#!/bin/bash` scripts run. `orole-guest` reads its token from fw_cfg
(`/sys/firmware/qemu_fw_cfg/by_name/opt/orole/token/raw`) and falls back to `orole.token=` on the kernel command line.

#### Guest image checks

Not run yet: no local Nix (the image is built in CI, `.github/workflows/guest-image.yml`). Boot the debug image
(`docs/development/guest-image.md`) and record here: `systemd-analyze`; `systemctl is-active cage-tty1 wayvnc
orole-guest`; `ls -l` of the fw_cfg token (0400 root) and `/dev/virtio-ports/org.orole.guest.0` (0600 root);
`ss -ltnp` (5900 and 9222 on 127.0.0.1 only); `runuser -u bot -- curl -s http://127.0.0.1:9222/json/version`;
`nft list ruleset`; `runuser -u bot -- curl -m 5 http://10.0.2.2:<port>` refused; internet as bot; `df -h /`;
`free -m`; and the NixOS report's criteria as bot (uv + numpy/pandas, a native npm module, a downloaded Node binary
through nix-ld, a `#!/bin/bash` script, Playwright's browser download, expected to fail).

### Testing

`go test -race ./internal/sandbox/... ./cmd/orole-guest/...` runs everywhere: the fake backend serves the real
guest handler over `net.Pipe`, and the test binary doubles as the guest's shell (`guestagent/agenttest`). The
Linux CI lane also runs the real `/bin/sh` exec tests and the real PTY test, and builds `orole-guest`; the tests that
switch to another user (`TestRunAsSwitchesUser`, `TestWriteFileAsRunAsOwnsNewPathsOnly`) need root and skip otherwise.
On Windows the QEMU backend is tested against a fake QEMU (it connects the sockets and answers QMP like the real
one); `TestRealBootConnectsTheGuestAgent` boots real QEMU with the spike's Alpine kernel and `orole-guest` and talks h2c
to it when `OROLE_SPIKE_QEMU` and `OROLE_SPIKE_ALPINE` are set.

## Phase 2 spike (WHPX on build 22000)

Run on 2026-10-15 on the developer PC (Windows 11 Pro 21H2, build 22000, Hyper-V running), unelevated. Re-run with
`go test -tags spike -run TestSpike -v ./internal/sandbox` (see the header of `internal/sandbox/spike_windows_test.go`).
The boot test uses the production `qemuArgs` builder (token via `-fw_cfg name=opt/orole/token,file=`), with only the
serial console moved from a file to a socket so the test can drive the guest's `/bin/sh`.

| Question | Result |
|---|---|
| QEMU build | `qemu-20260811-win64.zip` from draft release `qemu-v20260811` (SHA-256 `513d950c…54bf1b`, matches `SHA256SUMS`; unmodified files of `qemu-w64-setup-20260811.exe`, SHA-512 `5bcf9eed…f543`). `qemu-system-x86_64 --version`: `QEMU emulator version 11.1.0 (v11.1.0-12130-ge470268ff4)`. Accelerators: tcg, whpx |
| Alpine netboot 3.24.2 SHA-256 (`dl-cdn.alpinelinux.org/alpine/v3.24/releases/x86_64/netboot-3.24.2/`) | vmlinuz-virt `40f620bc8c93d952e57dd8dfc0f94fca1759d192a4fc4a260705d50ca378559c`, initramfs-virt `c990c63e4602aa84b92d7df54fd180cb0e56590d61b71221ba6d60e913d26357`, modloop-virt `1e7a3eea707d2ecb3d0502f97fb4dc4dcbac1fe06075e24ae19bfdf4343bb1cf` (kernel 6.18.52-0-virt) |
| Probes, this PC (Hyper-V role on, HypervisorPlatform feature reads disabled) | build=22000, dll_loads=true, hypervisor_present=true, create_partition=ok (no error), firmware_virtualization=false (Windows reports false inside the Hyper-V root partition), install_state=2 (disabled), cbs_reboot_pending=false |
| Probes, WHP enabled before / after reboot | not run: the feature was not changed (agents never change Windows features), and `WHvCreatePartition` already succeeds with it disabled |
| `-accel whpx` (default) | **fail**: `WHPX: Failed to enable nested virtualization, hr=80370302` then `failed to initialize whpx: Invalid argument`. Same with `whpx,hyperv=off` and `whpx,kernel-irqchip=on` |
| `-accel whpx,kernel-irqchip=off` | **ok** (also with `,hyperv=off`). QEMU 11.1 only asks WHP for nested virtualization on the in-kernel irqchip path, which build 22000 rejects |
| Direct kernel boot (`-kernel/-initrd`), boot to shell | whpx,kernel-irqchip=off cpu=host: ok, shell after 1.69 s; cpu=max: ok, 1.71 s. tcg cpu=max: ok, 3.20 s. Whole run (boot, all checks, poweroff): 6.5 s / 6.4 s / 8.5 s |
| AF_UNIX chardev (QEMU connects to a Go `net.Listen("unix")`) | ok, all three runs (guest channel, QMP and console sockets connected 32-34 ms after start; QMP greeting received) |
| virtio-serial port name `org.orole.guest.0` visible in guest | ok (`/dev/vport3p1`, `/sys/class/virtio-ports/vport3p1/name`) |
| guest→host / host→guest bytes | ok / ok, all three runs |
| `-fw_cfg name=opt/orole/token,file=<path>` readable in guest | ok: `/sys/firmware/qemu_fw_cfg/by_name/opt/orole/token/raw` holds the token (needs the `qemu_fw_cfg` module; Alpine ships it in modloop, not the netboot initramfs). Token not in QEMU argv |
| `-chardev pipe` DACL | `O:<user SID>D:(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;<user SID>)(A;;FR;;;WD)(A;;FR;;;AN)`; Everyone/Anonymous ACE: **yes** (read) |
| Guest reaches host 127.0.0.1 via 10.0.2.2 (`-netdev user`) | yes (HTTP GET from guest reached a Go listener on 127.0.0.1) |
| Guest internet via `-netdev user` | yes (`wget http://example.com`) |
| `poweroff -f` with `-no-reboot` ends QEMU | yes (exit status 0) |
| `%LOCALAPPDATA%\Orole` ACL (socket files inherit it) | inherited: SYSTEM (F), BUILTIN\Administrators (F), the user (F), and a local group `CodexSandboxUsers` (RX) inherited from the profile. Not user-only |

Decisions: `-accel whpx,kernel-irqchip=off` on Windows (Task 3: `defaultAccel`); `-cpu host` (`defaultCPU`; `max`
also works); channel: AF_UNIX (the pipe chardev grants Everyone and Anonymous read, so it is not a fallback without
changing QEMU); host loopback: reachable, so the guest's nftables rules block 10.0.2.2 (Task 8). The
data folder's ACL is inherited, not user-only: the backend should set an explicit user-only DACL on
`<data>\sandbox\run\` rather than rely on inheritance.
