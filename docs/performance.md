# Performance

All numbers below are **measured** on the machine in the first section, on 2026-10-08, with the tree at the time of
writing. They are single-machine, idle-ish desktop numbers: treat them as a regression baseline, not a guarantee.

## Machine

Intel Core i7-9700K @ 3.60 GHz (8 logical CPUs), 32 GB RAM, Windows 11 Pro 10.0.22000, Go 1.27.1, NVMe/SSD not
characterised, WebView2 154.0.4258.62. Other development processes were running (other agents building and testing),
so expect noise.

## Go benchmarks (`tests/performance`)

```powershell
go test -run '^$' -bench . -benchmem -count=3 ./tests/performance      # or: make bench
```

Three runs each; values are the range.

| Benchmark | What it measures | ns/op (3 runs) | allocs/op |
|---|---|---|---|
| `SQLiteInsertAudit` | one autocommit `audit_events` insert (WAL, `synchronous=NORMAL`) | 69,940 - 78,309 (~70-78 us) | 25 |
| `SQLiteInsertAuditTx100` | one op = a transaction of 100 inserts | 1.176 - 1.231 ms (~12 us/row) | 1131 |
| `SQLiteQueryAuditRecent` | `ORDER BY at DESC LIMIT 50` over 100,000 rows with the `at` index | 95,202 - 96,371 (~96 us) | 727 |
| `SQLiteSettingsUpsert` | `settings` upsert (what `settings.set` does) | 41,951 - 43,685 (~42 us) | 16 |
| `IPCEchoRoundTrip` | no-op echo over the real named pipe, authenticated, JSON frames | 58,370 - 59,630 (~59 us) | 41 |
| `DaemonHealthRoundTrip` | `daemon.health` through a real `daemon.Daemon` (reads DB schema version, memstats) | 127,909 - 129,449 (~128 us) | 49-50 |
| `ConPTYEcho` (`echo-µs/op`) | `terminals.Manager.Write` of a short token to `cmd.exe` until it is echoed back through ConPTY | 8,572 - 9,909 (~8.6-9.9 ms) | 12 |

Notes:
- `ConPTYEcho`: ns/op (~24 ms) includes a deliberate 15 ms drain sleep per iteration; the custom `echo-µs/op` metric is
  the write-to-echo time only. It covers conhost + shell echo inside the daemon; it excludes IPC (~0.06-0.13 ms) and
  xterm.js rendering, which are added on top in the UI.
- Batching inserts in a transaction is ~6x cheaper per row than autocommit.

### MCP Streamable HTTP loopback latency

```powershell
go test -run '^$' -bench '^BenchmarkMCPLoopbackCallTool$' -benchmem -count=3 ./tests/performance
```

`BenchmarkMCPLoopbackCallTool` measures one sequential, warm `mcp.Manager.CallTool` invocation over real
Streamable HTTP to a loopback `httptest` server running the maintained `github.com/modelcontextprotocol/go-sdk/mcp`
SDK. The local fixture echoes a short text argument. This is a transport/manager regression benchmark, **not
production Mail or Drive network latency**; it does not contact either service or use stored user credentials.

The timed path includes manager catalog lookup, call timeout handling, SDK request/response serialization,
loopback HTTP, fixture argument decoding and result creation, and manager result sanitization. Setup uses a
temporary database and in-memory credentials. Server startup, database migrations, MCP initialization/catalog
discovery, two warmup calls, payload allocation, full response validation, and teardown are outside the timer.
Warmup calls validate distinct echoed payloads; the last timed response is checked for exact text, text block
type, untrusted marking, and absence of truncation or tool errors. Each timed call also fails on transport or
tool errors.

This adapter-facing primitive excludes `InvokeChecked` policy/approval overhead, daemon IPC, UI rendering,
OAuth login/refresh, TLS, WAN latency, and real provider work. `ns/op` and `allocs/op` include both client and
fixture work in the same process. It measures warm round trips, not connection establishment or tail latency.
Observed on Windows amd64, Intel Core i7-9700K, 2026-10-08: three passing runs
measured 3.97–5.00 ms/op, 364,096–368,410 B/op and 289–292 allocs/op.
These are SDK loopback measurements, not production Mail or Drive latency.

## Daemon start, idle memory, idle CPU (`scripts/measure-daemon.ps1`)

```powershell
./scripts/build.ps1 -DaemonOnly ; ./scripts/measure-daemon.ps1
```

Fresh `OROLE_HOME` temp dir per run (so each cold start includes creating the database and applying all migrations
present in the tree, schema version 30 at the time). Cold start = process spawn until the named pipe accepts a
connection (the IPC hello is not included). 5 runs, then 20 s idle sampling after a 2 s settle.

| Metric | Result |
|---|---|
| Cold start, median / min / max | 80.4 ms / 63.5 ms / 348 ms (max is the first, disk-cold run) |
| Idle working set | 11.3 MB |
| Idle private (committed) memory | 46.5 MB |
| Idle CPU over 20 s | 0.000 % (below the resolution of `TotalProcessorTime`) |
| Go heap reported by `daemon.health` at idle | 0.7-1.5 MB, 11-13 goroutines |

## Desktop app (partially measured)

`scripts/smoke-installer.ps1` and an ad-hoc loop launched the packaged `Orole OS.exe`:

| Metric | Result |
|---|---|
| Time from process start until the window exists with its title ("Orole OS"), 5 launches | 571, 93, 161, 71, 47 ms (first is cold, rest warm) |
| Daemon appears and answers on the pipe after app launch | observed within the first 0.5 s poll in the smoke test |
| **Dashboard (Home) render time** | **not measured**: no instrumented first-paint/first-render hook, and the window-exists time above is not the UI paint |
| **Time to interactive UI** | **not measured** |

Window creation time is not first paint of the React UI; do not quote it as app start-up.

## Task feed and agents board (frontend, measured 2026-10-11)

Same machine, other processes running (noise). "Before" is `origin/main` at `c7c9b17`, "after" is this change, both
measured by the same tests (`TaskViewRenders`, `AgentsBoard`, `ActivityHeatmapWidget`, `TaskCanvasDiff`) and, for the
browser rows, one Playwright run against `vite` with a test-only MyGo bridge: a 500-event history, 150 live events
while pinned to the newest message.

| Scenario | Before | After |
|---|---|---|
| 100 live events into a 500-event history (jsdom): `groupEvents` calls | 200 | 100 |
| ... assistant-message / tool-card renders | 21,683 / 11,650 | 38 / 35 |
| Browser: rows in the DOM / DOM nodes with 400 feed rows | 400 / 5,231 | 14 / 197 |
| Browser: 150 live events while pinned, long tasks (>50 ms) / blocking time | 150 / 19.3 s | 0 / 0 ms |
| 200-session board, 100 session updates: card renders | 20,000 (600 on first paint) | 100 (200 on first paint) |
| Activity heatmap: date-format calls on first render / in total after a hover and 20 session updates | 743 / 16,354 | 0 / 0 |
| Open canvas, 40 session updates: `Agents.diff` requests | 41 | 1 |

Rows of a conversation longer than 60 are windowed (`@tanstack/react-virtual`); the feed groups each event once and
reuses unchanged items so memoized rows skip rendering.

## Reproducing and extending

Add benchmarks to `tests/performance`; windows-only ones go in `*_windows_test.go`. When behaviour changes in a
way that moves these numbers, rerun and update this page with the date.
