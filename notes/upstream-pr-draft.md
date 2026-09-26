<!-- DRAFT for the user to review (not opened). Branch: durable-never-block (drop the repro-only
     WAVETERM_JOBSOCKETDIR/cmd/test-jobfreeze/notes commits or keep the harness, maintainers' call). -->

# durable sessions: a stalled or broken client must never freeze the hosted program

Fixes #3439, #2985. Supersedes/complements #3460.

## Problem

When the stream to the client breaks, a durable session's job manager stops reading the PTY
after 64 KB, and **the hosted program blocks in `write()` to its tty** until Wave reattaches.
The ways the stream breaks:

- a spurious `timeout sending request` (NoResponse RPC race, below) drops a stream packet →
  permanent hole (no retransmission) → the window never reopens;
- the client stops ACKing (half-open ssh, laptop asleep, wedged connserver) → window full;
- the job socket is closed → the disconnect hook never runs (it was deferred in a function that
  returns immediately) → packets fail with "server is no longer running" but count against
  the window;
- connserver's router blocks in `log.Printf` under `router.lock` when stderr (→ sshd) stalls
  → it stops reading all job sockets.

## Changes

- `pkg/wshutil/wshrpc.go`
  - Don't register NoResponse requests (reqId `""`) in `RpcMap`. They all shared
    `RpcMap[""]`, and a finished one's timeout goroutine cancelled the context of the one in
    flight, which then failed instantly with "timeout sending request" whenever `OutputCh`
    was momentarily full.
  - `SendComplexRequest` returns the recovered panic as an error instead of `(nil, nil)`
    (callers dereferenced the nil handler).
- `pkg/jobmanager/streammanager.go`
  - `DataSender.SendData` returns an error. On error the client is detached (no hole).
  - Stall detection: while flow control blocks the PTY reader, if there has been no ACK
    progress for `DefaultStallTimeout` (10 s), the client is detached. The stream falls back
    to the existing disconnected mode (the 2 MB circular buffer drops the oldest bytes), so the
    PTY keeps draining. A slow but ACKing client keeps lossless backpressure.
  - A `ClientBrokenHandler` callback runs after a detach.
- `pkg/jobmanager/jobmanager.go`, `mainserverconn.go`
  - The broken-client handler closes that client's connection. connserver unregisters the
    job route, and wavesrv's existing route-down auto-reconnect does
    PrepareConnect/StartStream (with gap accounting if more than 2 MB was produced meanwhile).
  - The disconnect hook now runs when the job socket's read loop ends. The attached client
    is cleared when its connection goes away.
- `cmd/wsh/cmd/wshcmd-connserver.go`, `pkg/util/logutil/nonblockingwriter.go`
  - connserver logs through a non-blocking writer that drops lines when the sink is stalled.

No wire protocol change, and no wavesrv change needed.

## Tests

- `pkg/wshutil/wshrpc_noresp_race_test.go`: 5000 NoResponse sends against a slightly slow
  consumer. Before: about 85% fail with "timeout sending request" in under 1 ms. After: 0.
- `pkg/wshutil/wshrouter_logstall_test.go`: the router stops draining a job link when log
  output blocks (before). It keeps draining with the non-blocking writer (after).
- `pkg/jobmanager/streammanager_test.go`: a stalled client is detached and 4 MB is drained
  without the writer blocking; a slow but ACKing client is not detached; a send error
  detaches and replays from the last acked seq.
- `cmd/test-jobfreeze` (+ `repro.sh`): end-to-end against a real `wsh jobmanager` with fault
  injection on the job socket (blackhole ACKs / stop reading / 7 s hiccup / close) and a Node
  probe with a tty-write heartbeat:

```
before  blackhole       frozen 16/18 s   end: FROZEN
before  hiccup-drip     frozen 14/18 s   end: FROZEN   (1x "timeout sending request", wedged after recovery)
before  close           frozen 16/18 s   end: FROZEN
after   blackhole       frozen  8/18 s   end: RUNNING  (detached after 10 s without ACK progress)
after   hiccup-drip     frozen  2/18 s   end: RUNNING
after   close           frozen  0/18 s   end: RUNNING
```

In all cases a reattach resumes with `clientSeq == serverSeq` (no loss below 2 MB).

## Trade-offs / follow-ups

- A client that makes no ACK progress for 10 s while output is blocked gets disconnected and
  reconnected automatically (subject to the 30 s auto-reconnect cooldown). 5 s would match the
  RPC send timeout.
- Optional: let the connected-mode buffer exceed the network window (e.g. 256 KB) so short
  stalls never block the program. The cost is Ctrl-C latency on runaway output.
- The connserver router backlog is still unbounded during long upstream stalls.
