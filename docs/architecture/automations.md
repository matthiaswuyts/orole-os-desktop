# Automations (#57–#59)

Automations are daemon-owned. The desktop `Automations` service forwards typed IPC calls; React keeps only editor/view state. SQLite migration 0060 stores versioned definitions, immutable per-run definition snapshots and a write-ahead step journal. Times exposed to the UI are Unix milliseconds. Definitions are not created automatically.

## Authoring and running

The Automations view lists enabled definitions and their last result. Run now works even for a disabled definition; disabling stops automatic triggers, not existing runs. History shows step status, attempts, duration and redacted output. Pending human approvals can be approved or rejected; cancellation is explicit. The dashboard's Recent automation results widget uses the same daemon data. Browser preview has no backend and reports this honestly.

The structured editor supports adding, moving and removing typed steps. Unavailable integration adapters appear disabled with a reason. Parameter and schema errors are returned as field paths. A conditional branch targets a later step ID or `end`; backwards jumps are rejected. Conditional `when` guards can skip individual steps. The three example templates open unsaved drafts: “Notify me when CI fails”, “Morning GitHub issues”, and “Run tests after agent finishes”. Supply real repository/project/directory values and save to create a definition. The schedule editor previews the next five times using the daemon's local timezone.

The CI template is a scheduled check every 15 minutes, not a nonexistent CI push watcher. Select a real repository and ref; failed checks branch to notification. Persistent failures notify each time. GitHub query results include `checks`, `failed`, `repo`, and `ref` for the checks kind.

## Triggers and provenance

- Manual: authenticated `automations.run` IPC.
- Schedule: five-field cron (`minute hour day month weekday`), lists/ranges/steps, month/weekday names, common aliases, or `every 15m`/`every 2h`/`every 1d`. Minimum interval one minute. Fixed wall times follow local DST, skipping nonexistent times and the second occurrence of repeated times. Restricted day-of-month and weekday use cron's OR semantics. After downtime/sleep, `skip` consumes missed slots without running; `run-once` fires once, never a backlog burst. Ninety seconds of lateness is scheduling grace.
- Application event: exact topic, `prefix.*`, or `*`, with optional filter. `internal/events` has bounded, nonblocking subscriber queues; a full queue drops the newest event and increments `Subscription.Dropped`. This bus is not a durable message broker.
- Webhook: opt-in loopback listener, described below.

Modules publish facts through `d.Events.Publish(events.Event{Topic,Data,Chain})`. Capture `events.ChainFrom(ctx)` when automation-driven work starts and persist it with work that outlives the call. The chain contains causing automation IDs; an ID cannot reappear, and the maximum chain length is three. Automation completion publishes `automation.run.finished`. Event filters access `event.topic` and `event.data`; templates also access `trigger.data`, `run.id`, and `steps.<id>.output`. Agent and integration payloads are untrusted data, not instructions.

## Steps and safety

Command/test steps require an executable allowlist and explicit absolute working directory. Executables resolve with `exec.LookPath`; actual file identity is compared against resolved allowlist entries. Batch scripts are rejected. Each templated argument remains one argv element; there is no shell interpolation. Only a small environment allowlist is inherited. Explicit extra environment values are inputs, not a secret storage mechanism. Exit zero means pass; nonzero records output and fails the step. `onError=continue` allows later steps to inspect failed output. Definition allowlists are additional restrictions, not authorization grants.

HTTP steps require explicit allowed hosts and HTTPS unless the definition opts into HTTP. Private, loopback, link-local and other nonpublic addresses are blocked after resolution, and the checked numeric IP is dialed directly. No proxy environment is used. Every redirect is rechecked; at most three are allowed, and supplied headers are stripped on redirect. Authorization/cookie credentials and URL userinfo are rejected. Responses default to 64 KiB, capped at 1 MiB. These requests intentionally cannot access local network services.

Immediately before dispatch, command/test and HTTP steps use the daemon's shared policy as `automation:<id>`. A missing authorizer fails closed. Arbitrary commands (including test runners) and non-read HTTP methods are classified destructive; GET/HEAD are read effects. Command approvals show the resolved executable, every rendered argv element, working directory and explicit environment. HTTP policy sees the concrete method and full target, including each redirect. Stored denies and cancellation prevent dispatch. Native steps do not resolve credential references: secret-bearing authority details are rejected rather than concealed in approvals; use integration credential identifiers instead. Details that cannot fit the shared policy display without truncation or sanitization changes fail closed.

MCP, agent and GitHub steps call injected interfaces. Their adapters retain the daemon's own permission checks. MCP has no durable idempotency-key support and cannot select key strategy. Agent adapters use the orchestrator journal. Notifications use the existing notification service and its source preferences; a muted source may produce no notification.

### HTTP request authorization

Before DNS resolution or connection establishment, HTTP authorization receives a detached snapshot of the concrete method, full URL, all explicit headers (including automation chain and idempotency headers), and exact rendered body. Headers are copied and the body is retained as an immutable string; authorization cannot mutate the live request. A body-only change to a recipient or operation therefore changes the disclosed approval detail even when the endpoint is unchanged. Permitted headers and body bytes are not removed to make an approval fit.

The entire snapshot passes through the same fail-closed secret and display-completeness checks as other native effects. Recognized secrets in headers or bodies, oversized detail, invisible display alterations, or invalid UTF-8 prevent dispatch rather than presenting a concealed, truncated, or replacement-character version. These checks are defense in depth, not detection of every possible opaque credential.

Redirect authorization describes the next actual request after HTTP method/body transformations and the existing redirect header-stripping policy. In particular, POST redirects rewritten to GET disclose an empty body; 307/308 redirects disclose the preserved body. Every redirected request must pass policy before its own DNS lookup or connection, and denial sends nothing to that destination. Host allowlists, pinned public-address dialing, redirect limits, and the prohibition on redirecting idempotency-key requests remain unchanged.

## Crash recovery, approvals and limits

Each step is journaled before invocation and after completion, with a stable SHA-256 `(run,step)` idempotency key. Finished steps are never repeated on restart. For side effects:

- `at-most-once` (default): a journal still marked running after interruption becomes `uncertain`; it is not retried. Inspect the external system before starting a fresh run.
- `key`: only supported executors may select it. The external endpoint/adapter must durably honor the supplied idempotency key; an HTTP header alone cannot force a remote server to deduplicate.
- `repeatable`: the user explicitly asserts repeats are safe. Retries use exponential backoff, capped at one minute, up to eight retries.

Native command/HTTP steps first journal `authorizing`, then persist `running` immediately before the first actual dispatch. Restarting during a pending shared-policy approval reauthorizes safely; once dispatch is journaled, existing at-most-once uncertainty rules apply. Shared-policy denial, missing authorization, and approval timeout are not automatically retried, even with `key` or `repeatable`. Shared-policy approvals are distinct from explicit durable `approval` steps: they use the global policy queue and hold the active execution slot while waiting.

Per-step and per-run timeouts are enforced with contexts. Run elapsed time survives restart and includes time waiting for approval. Adapters must honor cancellation. Command cancellation terminates the directly launched process; arbitrary grandchildren are not promised to be killed. Human approval is a durable journal state and does not hold an execution slot. Approval resolution uses a transaction and cannot resolve an already canceled run. Settings controls global and default per-automation concurrency; each automation can override its own limit. Pending runs are bounded to 100 per automation.

Known secret values registered through `Engine.RegisterSecret` and bearer/GitHub/API token patterns are redacted from journal input/output/error and audit details. Sensitive JSON keys are also redacted. Redacted prior outputs are used consistently on restart. Do not paste credentials into definitions, command environments, prompts or URLs; definitions are configuration stored in SQLite, not a secret vault. Register secrets at the integration boundary before they can appear in tool output.

Save rejects recognized secret values and sensitive credential fields rather than persisting them in a definition snapshot. This is defense in depth, not a way to detect every possible opaque credential.

## Local webhooks

Disabled by default. Settings > Automations sets concurrency, enables the listener, selects a port (1024–65535), and generates/rotates the secret. The server binds **only `127.0.0.1`**, never a network interface. Exposing it externally requires the user's own tunnel and its associated security configuration. Orole does not create a tunnel.

The HMAC key is stored only under Credential Manager target `Orole OS/automations/webhook-hmac`. It is never returned to the frontend or stored in SQLite. A local sender running as the same Windows user can read it through `internal/credentials`; remote senders must be provisioned by the user's own trusted secret management workflow.

Send `POST /automations/<id>` with JSON (maximum 256 KiB), one `X-Orole-Timestamp` header (canonical decimal Unix seconds, no leading zeroes or `+`), and one `X-Orole-Signature` header containing `v1=<64 hex characters>`. **Only this versioned format is accepted:** the former bare/`sha256=` signature over timestamp and body is rejected. Use the stored credential bytes as the HMAC key; do not hex-decode the credential's text.

### Version 1 signing contract

The HMAC-SHA256 input is the literal 17-byte prefix `OROLE-WEBHOOK-V1\x00` (16 ASCII characters followed by one NUL byte), then **five length-prefixed fields in this order**:

1. Method: the exact ASCII bytes `POST`.
2. Target: the ASCII bytes of `/automations/` followed by Go `url.PathEscape(id)` (UTF-8 ID bytes, uppercase percent escapes). Ordinary generated UUID IDs require no escaping. Only the canonical spelling is accepted: no redundant escaping, slash/backslash in the decoded ID, `.`/`..` IDs, trailing slash, or query string (even a bare `?`). Scheme, host and port are not signed, allowing a user-managed tunnel to forward the same canonical path.
3. Timestamp: the exact `X-Orole-Timestamp` bytes.
4. Provenance: the exact `X-Orole-Chain` header value after HTTP's surrounding whitespace removal, or zero bytes if absent/empty. A nonempty value must be a JSON array of causing automation IDs, not `null`. Do not reserialize it after signing; even equivalent JSON with different internal whitespace changes the signature. ASCII JSON with escaped non-ASCII characters avoids header-encoding differences.
5. Body: the exact transmitted body bytes, not parsed or reserialized JSON.

Each field is encoded as its **unsigned 64-bit big-endian byte length followed immediately by those bytes**; there are no separators, terminators or implicit newlines. Prefix and field lengths are included in the HMAC. `X-Orole-Signature` itself is not. Hex digits may use either case; the version prefix is exactly `v1=`. Duplicate timestamp, signature or chain headers are rejected, rather than selecting one value. Any future field affecting provenance or dispatch must be added to a new authenticated envelope version, not an unsigned header.

Verification uses constant-time comparison and the existing inclusive ±5-minute timestamp window. Repeated identical signed deliveries deduplicate durably by signature and return the original run ID, including after daemon restart and regardless of hex digit case. Changing the target, method, timestamp, body or chain invalidates the captured signature. Authenticated chains still enforce self-recursion and maximum depth; knowing the global credential grants signing authority, so provision it only to trusted senders. Return status is 202 with `runId`, 204 for a nonmatching filter, 401 for invalid authentication, 404 for a noncanonical/unknown target, or an explicit error.

This Python sender accepts already-provisioned credential bytes in memory; it does not retrieve, print or persist secrets:

```python
import hashlib
import hmac
import json
import struct
import time
import urllib.parse
import urllib.request


def send_webhook(base_url, automation_id, credential, body, causes=None):
    if not automation_id or automation_id in (".", "..") or any(
        c in automation_id for c in "/\\"
    ):
        raise ValueError("automation ID must be one canonical path segment")
    target = "/automations/" + urllib.parse.quote(automation_id, safe="$&+:=@")
    timestamp = str(int(time.time()))
    chain = "" if causes is None else json.dumps(
        causes, ensure_ascii=True, separators=(",", ":")
    )
    fields = [b"POST", target.encode("ascii"), timestamp.encode("ascii"),
              chain.encode("ascii"), body]
    envelope = b"OROLE-WEBHOOK-V1\x00" + b"".join(
        struct.pack(">Q", len(field)) + field for field in fields
    )
    headers = {
        "Content-Type": "application/json",
        "X-Orole-Timestamp": timestamp,
        "X-Orole-Signature": "v1=" + hmac.new(
            credential, envelope, hashlib.sha256
        ).hexdigest(),
    }
    if chain:
        headers["X-Orole-Chain"] = chain
    request = urllib.request.Request(
        base_url.rstrip("/") + target, data=body, headers=headers, method="POST"
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        return response.status, json.load(response) if response.status == 202 else None
```

Supply `body` as the exact JSON byte sequence and `base_url` as the configured listener origin (for example `http://127.0.0.1:8765`). For retries, retain the original timestamp, chain, body and signature rather than creating a new delivery. The webhook regressions use the real loopback server and persisted runs to cover cross-target replay, changed provenance, canonical paths, malformed/legacy signatures, time/body limits and replay after engine restart.

## API and verification

IPC: `automations.list/save/delete/run/cancel/runs/run.detail/approvals.list/approve`, plus `automations.stepTypes`, `automations.config`, `automations.config.set`, `automations.webhook.generateSecret`, `automations.schedule.preview`, and `automations.templates`. Run/journal changes publish `automations.run` over IPC; the current frontend refreshes visible resources every five seconds.

Run `go test -race ./internal/automations/... ./internal/events/...` and `go vet ./internal/automations/... ./internal/events/...`. Tests inject clock, tool, notification, resolver and dialer implementations. Authorization regressions use the real shared policy with a child-process filesystem effect and a real HTTP server; missing/denied policy produces no effect, while approval permits dispatch. Crash tests close the first runner's database mid-operation and reopen it, exercising unfinished journal recovery without repeating finished actions.

The opt-in daemon smoke test runs the real `go version` command and sends a real notification through authenticated named-pipe IPC, retaining the complete journal in the isolated database:

```powershell
$env:OROLE_HOME='C:/code/projects/orole-os/.orole-dev'
$env:OROLE_AUTOMATION_SMOKE='1'
go test -v ./internal/daemon -run '^TestAutomationCommandNotification$'
```

It connects to an existing dev daemon, or starts/stops one through the existing daemon test harness if absent. It never stops a daemon it did not start. Approve the command through the shared policy UI when prompted; the executable allowlist alone does not grant authority. The output includes the redacted journal; no sample runs are seeded into production.

### Observed integration smoke

The real daemon workflow smoke passed on 2026-10-08 using the parent-approved isolated absolute home `C:/code/projects/orole-os/.orole-smoke-automation`; the existing `.orole-dev` instance was left untouched after it rejected authentication.

| Journal row | Status | Attempt | Output |
| --- | --- | --- | --- |
| `version` (`command.run`) | succeeded | 1 | exitCode `0`, stdout `go version go1.27.1 windows/amd64` |
| `notify` (`notify`) | succeeded | 1 | notification ID `1245de3e-afa3-4a36-8a8b-d6c4a1c595a2`, level `success` |

Run `c5760b67-7a27-4be7-a9fc-38e930a40d67` finished `succeeded`; its SQLite journal remains in the isolated smoke home. This verifies notification persistence/publication, not whether Windows displayed a native toast. Browser preview was also opened without mocks and rendered the honest “Automations needs the desktop app” state with no browser errors.
