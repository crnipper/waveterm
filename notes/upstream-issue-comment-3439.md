<!-- DRAFT for the user to review: comment on https://github.com/wavetermdev/waveterm/issues/3439 (not posted) -->

We hit this on v0.14.5 (Linux remote, laptop client over VPN). It has a second, worse effect
that isn't mentioned here yet: **the hosted program freezes**, not just the view. Our remote
process (a Node TUI agent) was blocked in `write()` to its tty (`wchan=wait_woken`) for
21 minutes, until Wave was restarted and reattached (`clientSeq == serverSeq`, a single
`SendData: error sending stream data: timeout sending request` in the job log). I reproduced it
against a real `wsh jobmanager` with a small harness. Here are the root causes I found, with
line numbers from v0.14.5:

1. **Spurious "timeout sending request"** (`pkg/wshutil/wshrpc.go`). Every NoResponse
   request is registered with `registerRpc(handler, …, "")` (L801), so they all share
   `RpcMap[""]`. When one finishes, its timeout goroutine sends `""` to `CtxDoneCh`.
   `runServer` then calls `unregisterRpc("")` (L405), which cancels the context of **the
   NoResponse request that is registered under `""` right now** (L511). If that request is
   waiting for room in `OutputCh`, it fails immediately with "timeout sending request"
   (L807). A test with a consumer that is only slightly slower than the producer fails
   4283 of 5000 sends, each after less than 1 ms. That explains why `tail -f` plus a bad
   network triggers this so reliably. Fix: don't register requests with an empty reqId.

2. **A dropped stream packet wedges the stream permanently.**
   `routedDataSender.SendData` (`pkg/jobmanager/mainserverconn.go` L44-50) logs and drops the
   packet, and `StreamManager.senderLoop` ignores it. But `sentNotAcked` has already been
   advanced, and there is no retransmission. The reader parks all later packets as
   out-of-order and keeps ACKing the seq of the hole, so the window never reopens, even
   after the path recovers.

3. **The job manager stops reading the PTY when the window is full.** In connected mode the
   circular buffer is in sync mode with window = min(64 KB, rwnd) (`streammanager.go` L137/243/265),
   and `handleReadData` blocks on it (L321). With 2) or a dead/half-open client, the kernel
   pty buffer fills and **the program blocks on its tty** until a reattach. Nothing times
   out: there is no ACK timeout and no keepalive in the stream protocol.

4. **Clean disconnects hit the same wall.** `defer WshCmdJobManager.disconnectFromStreamHelper(serverImpl)`
   (`jobmanager.go` L425) runs when `handleJobDomainSocketClient` returns, which is
   immediately. So a closed job socket never detaches the stream. Every packet then fails
   with "server is no longer running" but counts against the window, and after 64 KB the
   PTY stops being read.

5. **connserver can wedge its router on logging.** connserver logs to stderr, a pipe to
   sshd. `addToBacklog_withlock` calls `log.Printf` while holding `router.lock`
   (`wshrouter.go` L326, under `sendRpcMessageToLink` L338). The backlog grows exactly when
   the upstream ssh isn't draining. If stderr blocks, the router stops reading every job
   socket, and the job managers' sends time out for real.

Also: `SendComplexRequest`'s deferred `PanicHandler` (L775) swallows the error, so a panic (e.g.
send on a closed `OutputCh`) returns `(nil, nil)` and `SendCommand` then crashes on
`handler.finalize()`.

PR #3460 handles the symptom of 2) (it disconnects on send error) plus a wavesrv read watchdog,
but not 1), 4), 5), or the case where the client just stops ACKing. In that case the job
manager never sees an error, and a sleeping laptop can't run a watchdog.

I have a branch with a small fix (wsh side only, no protocol change): the NoResponse
registration fix; send error → detach; if the PTY reader has been blocked with no ACK
progress for 10 s → detach (the stream drops to the existing 2 MB drop-oldest mode, so the
program keeps running); the detached client's connection is closed so the existing
route-down auto-reconnect resyncs with gap accounting; the disconnect hook fixed; and a
non-blocking log writer in connserver. It comes with a reproduction harness (a real `wsh
jobmanager`, fault injection on the job socket, a probe program with a tty-write heartbeat)
and unit tests. Happy to open it as a PR (or split it) if that's useful.
