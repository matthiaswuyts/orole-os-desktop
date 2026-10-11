# Machines and SSH pairing

## Ownership and lifecycle

The daemon owns `machines.Manager`, persisted machine metadata, SSH credentials, and active command cancellation. `internal/app.Machines` is a stateless IPC bridge. Register `registerMachines()` after `Policy` exists and before `registerFiles()`; retain `Daemon.Machines` and call `Machines.Close()` at shutdown. `machines.NewProvider(d.Machines)` registers the shared Files `remote` provider. Bind `app.Machines{L: link}` in the desktop process and regenerate MyGo bindings. The Machines surface is `frontend/src/features/machines/MachinesView.tsx`.

Migration `0070_machines.sql` adds execution backend, SSH username, public host-key bytes, approved roots, command grant and write grant to the existing machines table. Local identity is `local`, initialized from the actual hostname and runtime OS. Local operations use the native OS directly and do not require SSH. Remote connections use `golang.org/x/crypto/ssh` and `github.com/pkg/sftp`; no Tailscale, network listener, or remote Orole daemon is required.

Transport and execution are separate fields: native transport/native execution; SSH transport/POSIX execution on Linux; SSH transport/PowerShell execution on Windows. Remote Windows requires OpenSSH server with PowerShell available; file access requires its SFTP subsystem. Health verifies pinned SSH authentication, not the installed command backend. Host physical memory is reported separately from daemon heap; unavailable metrics remain explicitly unavailable.

## Pairing and trust

1. Enter a host and port, account name, explicit remote OS, and the path of a local unencrypted OpenSSH private key. The server must already authorize the public key.
2. `Probe` reads the SSH host key without authentication. This is untrusted discovery, not trust-on-first-use.
3. Independently verify the SHA256 host-key fingerprint (for example, `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the server). Paste the independently verified fingerprint into the confirmation field.
4. The Go policy engine approves pairing as a destructive/trust-changing operation. The daemon repeats the handshake, requires the exact fingerprint, authenticates with the selected key, and saves both public key bytes and fingerprint. The private key is imported into the per-user OS credential store under `machines/<id>/key`, never returned through IPC or stored in SQLite.
5. Pairing grants no roots, writes, or commands. Configure these independently under Machine access; that grant change also needs Go-side approval.

Every later SSH connection compares both fingerprint and exact public key bytes before authentication. A changed key fails closed. There is no “accept anyway” retry. For intentional key rotation, verify independently, remove the old pairing, then pair again. Removing a pairing deletes its stored credential and identity, not remote files. Existing references to its old ID no longer resolve.

Encrypted private key files and SSH-agent forwarding are not supported. Protect the source key file with account ACLs; the original is not removed. Credential Manager size/OS errors are surfaced instead of persisting elsewhere. Non-Windows credential-store limitations remain explicit.

## Authorization, execution, and cancellation

All capability decisions are in Go through the shared policy engine. Resource keys are `machine/<id>` (pair/probe use the address before an ID exists). Reads use `machines.files.read`; writes require the per-machine write grant plus `machines.files.write`; commands require the command grant plus `machines.execute` classified destructive. Pairing, removal and access changes are destructive approvals. Local Files/Projects roots do not authorize machine operations, and machine roots do not authorize local Files/Projects. Remote provider paths cannot select `local`.

Native machine roots use the same canonical path boundary as the local Files provider. Approval persists the resolved directory, including Windows junction and 8.3-name expansion. Requests may retain the original ancestor alias (for example, a hosted runner's temporary-directory path): when no lexical root matches, the provider resolves the request before selecting an approved root. This never grants the alias's parent or siblings. The selected root must still resolve to its approved location, and the requested target must remain inside it; junction escapes and retargeted roots are denied. Rename/delete resolve the parent but do not follow the final link. Windows regressions cover alias-based grant, stat, read, create/update, revocation, and containment.

Commands use an executable plus an argument array. Native execution uses `exec.CommandContext` without shell interpolation. Linux encodes every argument as a POSIX single-quoted word. Windows uses a PowerShell bootstrap to launch a .NET Process with `UseShellExecute=false`, an explicit executable, and MS C-runtime-quoted arguments; it bypasses PowerShell 5's lossy legacy native-argument binder. The bootstrap uses escaped string literals and travels as UTF-16LE `-EncodedCommand`, avoiding the SSH server's default-shell quoting layer. Native Windows executables using nonstandard command-line parsers may require their own argument conventions. NUL arguments are refused. Arbitrary commands run with the account's full OS permissions: **file roots are not an execution sandbox**. A caller intentionally choosing a shell can request shell semantics, subject to command approval.

`Execute` requires a unique UUID operation ID. `Cancel` cancels that command, including approval waiting. Commands default to 30 seconds and cap at 300; connect/probe/health and file operations have bounded network deadlines and cancellation closes the SSH socket. Stdout and stderr are capped independently at 512 KiB with explicit truncation. Nonzero process exits are result data; transport, timeout and cancellation failures are errors. Closing SSH terminates the channel, but cannot guarantee that a deliberately detached remote process is killed. Native cancellation kills the launched process, not arbitrary detached descendants. Daemon shutdown cancels active commands; commands are not reboot-persistent jobs.

## Shared file browser

`machines.NewProvider(manager)` implements `files.Provider` as `remote`. Namespace paths are `/<machine-uuid>/<remote-path>`; examples are `/<id>/home/alice` and `/<id>/C:/Users/Alice`. Paths use `/` and retain the machine ID through navigation. Roots come only from paired remote machines. No operation falls back to the local filesystem.

Implemented operations: roots, directory listing, stat, bounded read, optimistic atomic write, and bounded recursive name search. Writes require an etag unless explicitly creating or forcing. Read responses detect a changed file while reading. Replacement uses the server's atomic POSIX-rename extension; a server lacking it returns a clear error rather than deleting the original. Creation uses standard SFTP rename with no-overwrite semantics. Remote mkdir, rename and deletion intentionally return `files.ErrUnsupportedOp`; the UI explains this. Remote deletion never silently becomes permanent deletion.

Remote file operations share one pooled SSH connection with an SFTP subsystem per machine (`internal/machines/pool.go`), so consecutive operations skip the TCP/SSH handshake, credential read, key parse and SFTP start-up. The pool never caches authorization: every operation still takes its access snapshot, policy decision and root checks, and every dial still enforces the pinned host key and fingerprint. A session is reused only for the exact address, username, fingerprint and host key it authenticated with. It closes after one minute without an active operation, on connection loss, when the operation using it is cancelled or times out, when the machine's access is edited or the machine is removed (which also discards a dial in flight), and when the manager closes. Remote etags are `v1:<mtime unix nanos>:<size>:` with an empty hash: they come from file attributes and never download content. SFTP v3 carries whole-second mtimes, so a same-size rewrite within the same second as the previous version is not detected. Path containment checks issue their independent queries concurrently over the shared connection.

Approved roots are canonicalized on the remote server. Each request checks root containment, current canonical identity, and refuses symlink components. Search does not follow symlinks. Account ACLs remain the final boundary: an authenticated remote server is a trusted endpoint and SFTP is not a hostile-server sandbox. Remote processes can race file changes; SFTP does not provide a universal atomic compare-and-swap primitive, so optimistic etag checks detect observed conflicts but cannot eliminate an external writer between the final comparison and rename. Users needing isolation must configure the SSH account/server filesystem permissions accordingly.

Typed daemon APIs, usable independently of the shared browser:

- `List(ctx) ([]Machine, error)`, `Get(ctx, id) (Machine, error)`
- `Probe(ctx, actor, ProbeRequest) (Identity, error)`
- `Pair(ctx, actor, PairRequest) (Machine, error)`
- `SetAccess(ctx, actor, Access) (Machine, error)`, `Remove(ctx, actor, id) error`
- `Health(ctx, actor, id) (MachineHealth, error)`
- `Execute(ctx, actor, Command) (CommandResult, error)`, `Cancel(operationID) error`
- `ListFiles(ctx, actor, FileRequest) ([]files.Entry, error)`
- `StatFile(ctx, actor, FileRequest) (files.Info, error)`
- `ReadFile(ctx, actor, FileReadRequest) (files.ReadResult, error)`
- `WriteFile(ctx, actor, FileWriteRequest) (files.Info, error)`

The IPC method prefix is `machines.`; file operations use `machines.files.*`. The desktop bridge uses the same typed request/result structs and fixes actor identity to `user` in daemon handlers. Automation/agent adapters must supply their own actor identity directly, never impersonate the user.

## Box labels and live footer

`Machines.rename` changes the persisted display name through Go policy approval;
it does not alter machine identity, hostname, credentials, or transport. The local
hostname initialization preserves a customized name.

The footer uses coss Preview Card and Tooltip for connected-box details, physical
memory use, and online counts. Unknown, stale, and failed probes are not reported
as offline or zero-memory samples. Clicking a box opens machine details.

The installed desktop was exercised on 2026-10-08: changing the local box name to
`Smoke workstation` required daemon approval and updated the footer and composer.
The footer displayed real physical memory use and `1/1 online`; its preview
exposed hostname details.

## Verification and integration smoke

No external Linux or Windows SSH host was available to this implementation session; no external-host verification is claimed. Tests are provided for key changes, quoting, default-denied access, root boundaries, namespace isolation, and pairing/authenticated reconnect against an in-process SSH test endpoint. That endpoint is test code, never production fallback data.

The combined Go run passed machine tests under the race detector on 2026-10-08. A separate real
daemon smoke then verified the local hostname and health, approved-root write/read, stale-etag
conflict after an external change, denial outside the approved root, and an explicitly approved
native `go version` invocation through authenticated named-pipe IPC. No SSH installation was needed
for these local operations.

External-host acceptance still required:

3. On a disposable Linux or Windows SSH server, create an account restricted to a temporary directory and authorize an ed25519 key. Independently obtain its host-key fingerprint. Pair through Machines. Deny a policy approval first and confirm no pairing appears; approve a second attempt. Restart the GUI and daemon and confirm the pinned identity persists and reconnect authenticates.
4. Grant a temporary remote root. In Files → Remote, browse and open a real file, edit and save, then externally change the file and confirm stale-etag conflict handling. Try a sibling directory and a symlink escape; both must fail. No local root should appear under Remote. Unsupported folder mutations must surface an error, never affect a local path.
5. Grant commands, approve a harmless executable with arguments containing spaces, quotes, `$()`, semicolons and non-ASCII text, and verify the server receives them literally. Test both Linux and Windows, plus a nonzero exit. Start a long-running command, cancel by its operation ID, and test a short timeout. Do not assume detached child cleanup.
6. Rotate the disposable server's host key without re-pairing. Health, commands and files must all fail on changed identity before authentication. Restore the old key or perform explicit remove-and-pair after independently verifying the replacement. Do not weaken host-key checks to make the smoke pass.
