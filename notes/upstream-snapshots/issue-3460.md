#3460 [open] fix: recover durable SSH sessions stuck falsely-attached after stream timeout (#3439)
https://github.com/wavetermdev/waveterm/pull/3460
comments=2
## Summary

Fixes #3439 (and the root cause behind #2985): a durable SSH terminal intermittently becomes completely non-interactive while the shield keeps showing **Durable Session (Attached)**. Recovery previously required restarting Wave (or Force Restart Controller).

## Root cause (two-sided)

**1. Remote job manager swallows stream send errors.**

`routedDataSender.SendData` (`pkg/jobmanager/mainserverconn.go`) logged `SendData: error sending stream data: timeout sending request` and then **discarded the error**. The `StreamManager` therefore kept the stream in `connected` state forever:

- the sender loop kept "sending" into a dead RPC path and never fell back to buffering mode,
- the remote shell and PTY stayed alive but their output went nowhere,
- the job manager kept reporting the client as attached, so the main server saw a healthy route and never reconnected.

**2. Main server has no stall detection.**

`jobcontroller.runOutputLoop` blocks in `reader.Read` indefinitely. The only auto-reconnect trigger is the `RouteDown` event (`handleRouteDownEvent`) — but in this failure mode the job route never goes down, so no reconnect is ever attempted. The block stays frozen until the whole app (or the connection controller) is restarted.

## Fix

**Remote job manager** (`pkg/jobmanager`):

- `DataSender.SendData` now returns an `error` instead of swallowing it.
- `StreamManager.senderLoop`: on send error, the stream drops back to disconnected (buffering) mode via `ClientDisconnected()` — the 2 MB circular buffer keeps capturing PTY output — and invokes a new `OnSendError` callback.
- `JobManager.handleStreamSendError`: closes the stale client socket. This makes the job route drop on the main server, which triggers the **existing** auto-reconnect path (`attemptAutoReconnect` → `ReconnectJob` → new attach → stream resumes from the buffered offset with no data loss, exactly like after an app restart).

**Main server** (`pkg/jobcontroller/jobcontroller.go`):

- `runOutputLoop` reads are wrapped in a 45 s stall watchdog (`StreamStallTimeout`). If no data/EOF arrives within the timeout, `handleStalledStream` runs: it asks the remote connection to drop the job manager's dead client (`RemoteDisconnectFromJobManagerCommand`) and then performs a `ReconnectJob`, which does a fresh attach + `PrepareConnect`/`StartStream` from the persisted offset. Remote shell state and scrollback survive. A 30 s cooldown (`AutoReconnectCooldown`, shared with the existing reconnect path) prevents reconnect flapping.

## Tests

- New `TestSendErrorDisconnectsClient` in `pkg/jobmanager/streammanager_test.go`: verifies that a send failure (1) drops the stream to disconnected mode, (2) fires `OnSendError`, and (3) a new client can attach and receives the buffered data.
- `go build ./...` clean, `go test ./pkg/jobmanager/ ./pkg/streamclient/` pass.

## Notes / trade-offs

- A completely silent shell (no output for >45 s) will trigger a seamless reconnect cycle roughly every timeout period. It's invisible to the user (same PIDs, same buffer, `totalGap=0`) but shows up in logs. Happy to raise the default or make it configurable if preferred.
- The `wsh` binary must be re-deployed to remote hosts for the job-manager half of the fix to take effect (Wave handles this automatically on connect with the new build).
- Interface change is internal-only: `jobmanager.DataSender` (implemented by `routedDataSender` and the `cmd/test-streammanager` helper, both updated).

## Reproduction (for validation)

1. Durable SSH session, run a long-lived TUI with sustained output (Codex CLI / lazygit / claude).
2. Leave for hours until the RPC stream write times out (`SendData: timeout sending request` in the remote durable-session log).
3. Before: block frozen, shield says Attached, only app restart helps.
4. After: job manager closes the stale client, wavesrv reconnects automatically (log: `stream stalled ... forcing job manager reconnect` / `closing stale client connection`), same PIDs, session responsive again.

