# Wave durable sessions freeze the hosted program: investigation (v0.14.5)

Branches in `~/sources/waveterm` (none pushed):

- `investigate-durable-sessions`: untouched `v0.14.5`
- `durable-freeze-repro`: reproduction only (harness, tests, these notes) plus a test-only
  `WAVETERM_JOBSOCKETDIR` override so the harness never touches `/tmp/waveterm-<uid>`
- `durable-never-block`: prototype fix, based on `durable-freeze-repro`

All experiments ran under `/tmp/wds` (own Go toolchain, own HOME, own job sockets, own tmux
`-S` socket). Nothing touched `~/.waveterm`, the live `wsh jobmanager`/`connserver`
processes, the live tmux server, or the pi-channeld hub.

## TL;DR

The job manager never buffers more than **64 KB** for a connected client. When that client
stops ACKing, the job manager **stops reading the PTY**. The kernel pty buffer then fills
and the hosted program blocks in `write()` to its tty (`wchan=wait_woken`, i.e. `n_tty_write`).
It stays blocked until a new client reattaches. Four independent bugs get a stream into that
state, and nothing ever gets it out:

1. **Spurious `timeout sending request` (the likely trigger in our incident and in upstream
   #3439).** Every NoResponse RPC (all stream data packets) is registered in `RpcMap` under
   the same key `""`. When one finishes, its timeout goroutine makes `runServer` call
   `unregisterRpc("")`, which **cancels the context of whichever NoResponse request is
   registered under `""` right now**. If that request is waiting for room in the 32-slot
   `OutputCh`, even for a millisecond, it fails immediately with "timeout sending request".
   The unit test shows 4283 of 5000 sends failing, each after less than 1 ms, against a consumer
   that is only slightly slow. So no 5 s stall is needed.
2. **A failed send is a permanent hole.** `routedDataSender.SendData` logs the error and
   drops the packet, but `sentNotAcked` has already advanced. There is no retransmission. The
   reader parks every later packet as out-of-order and keeps ACKing the seq just before
   the hole. The window never reopens, so the stream is wedged **even after the network or
   connserver recovers**. The job still shows as Attached (#3439).
3. **Clean disconnect also freezes.** The disconnect hook is `defer`ed in
   `handleJobDomainSocketClient`, which returns right after spawning its goroutines. So a
   closed job socket **never** calls `ClientDisconnected()`. The stream stays "connected" to
   a dead RPC: each packet fails with "server is no longer running" but is counted as
   in flight. After 64 KB the PTY stops being read. This refutes the brief's guess that the
   clean-disconnect mode keeps draining: the draining "disconnected" mode is effectively
   dead code between reattaches.
4. **connserver's router can wedge on its own log output.** connserver logs to stderr, a pipe
   to sshd. `WshRouter.addToBacklog_withlock` calls `log.Printf` **while holding
   `router.lock`** (and the backlog grows exactly when the upstream ssh stalls). When the
   ssh connection stalls, stderr blocks, so the router lock is held forever and connserver
   stops reading **every** job socket. The job managers' sends then time out for real (5 s),
   which is bug 2 again.

Plus the half-open case needs no bug at all: acks stop arriving, 64 KB fills, and the PTY is
not read until someone reattaches. Nothing detects the dead peer. sshd has no
`ClientAliveInterval`, and the job manager has no ACK timeout.

## 1. Mechanism with code references (tag v0.14.5)

### Output path

`pty master` → `StreamManager.readLoop` → `CirBuf` (2 MB, `streammanager.go:18`) →
`senderLoop` → `routedDataSender.SendData` → `WshRpc.OutputCh` (32 msgs) → job unix socket →
connserver `WshRouter` → ssh → wavesrv `streamclient.Reader` → ACK back the same way.

### Flow control and where backpressure is applied

- Connected mode puts the circular buffer in **sync mode with window = min(cwnd 64 KB, rwnd)**:
  `ClientConnected` `streammanager.go:137`, `RecvAck` `:243`, `SetRwndSize` `:265`,
  `CwndSize` `:17`, wavesrv `DefaultStreamRwnd = 64 KB` `jobcontroller.go:68`.
- `CirBuf.WriteAvailable` returns a wait channel when `count >= windowSize` in sync mode
  (`cirbuf.go:78`), and `handleReadData` blocks on it (`streammanager.go:321`). **This is the
  point where the PTY stops being read.** The buffer holds exactly the unacked bytes, so
  the job manager buffers at most 64 KB for a connected client, not 2 MB.
- The sender stops when `effectiveRwnd - sentNotAcked <= 0` (`streammanager.go:388`).
- After that the only thing between the program and the frozen reader is the kernel tty
  buffer (the probe froze after about 85 KB: 64 KB window plus the pty buffer).

### (a) Client stalls or half-opens

- Acks stop → window full → reader blocked → **program blocked**. Nothing times out: no
  ACK timeout, no keepalive in the stream protocol, `ClientAliveInterval 0`,
  `tcp_keepalive_time 7200`. The job log shows nothing (our `blackhole` scenario: empty log).
- If the job socket stops being read (connserver wedged, see bug 4), `SendData` blocks on
  `OutputCh` for `DefaultTimeoutMs = 5000` (`wshrpc.go:28`, `:807`), logs
  "timeout sending request" (`mainserverconn.go:49`) and **drops the packet**
  (`streammanager.go:355` ignores it).

### (b) Clean disconnect (socket closed)

- `defer WshCmdJobManager.disconnectFromStreamHelper(serverImpl)` at `jobmanager.go:425`
  runs as soon as `handleJobDomainSocketClient` returns, which is immediately. It is a no-op
  then (the client has not authenticated yet), and it never runs again.
- Afterwards `SendComplexRequest` fails fast with "server is no longer running"
  (`wshrpc.go:765`). The job logs are full of these: `e48b35ae…` about once a minute for idle
  shells, and `eceec468…` 118 in one burst on 09/24 23:12. Each lost packet still counts
  against the window, so after 64 KB of output the PTY stops being read. Low-output
  shells take days to get there, which is why it looks as if it "keeps draining".

### (c) Client absent

- Only after `ClientDisconnected()`, which in practice happens only inside
  `connectToStreamHelper_withlock` on reattach: the buffer goes to async mode with a 2 MB
  window and drops the oldest bytes (`streammanager.go:186`, `cirbuf.go:94`). This is the only
  mode that drains, and bugs 2 and 3 mean a dead client never reaches it.

### Reattach and replay (clientSeq/serverSeq)

- wavesrv `restartStreaming` (`jobcontroller.go:1204`) sends `JobPrepareConnect{Seq = WaveFS
  file size + totalGap}`. `ClientConnected` consumes up to `clientSeq` and returns
  `startSeq = max(headPos, clientSeq)`. If `serverSeq > clientSeq`, wavesrv records a gap
  (`:1316`). Replay starts from the last **acked** byte, so after a wedge nothing is lost
  (`clientSeq == serverSeq` in our incident and in #3439). The stream is "corked"
  (rwnd = 0) until `JobStartStream`.
- `SetAttachedClient: kicking out existing client` (`jobmanager.go:217`) closes the old
  connection. It is the only thing that ever unwedges a stream.

### Bug 1 in detail (spurious timeout)

- `SendComplexRequest` always calls `registerRpc(handler, …, handler.reqId)` (`wshrpc.go:801`).
  For NoResponse requests `reqId == ""`, so all of them share `RpcMap[""]`.
- `registerRpc` starts a goroutine: `<-handler.ctx.Done(); w.retrySendTimeout(reqId)`.
  `SendCommand` finalizes (cancels) the handler right after a successful send. So request
  A's goroutine pushes `""` onto `CtxDoneCh`, and `runServer` calls
  `unregisterRpc("", …)` (`wshrpc.go:405`), which calls `rd.Handler.callContextCancelFn()`
  (`:511`) on **request B**, the one now registered under `""`. If B is blocked in
  `select { case OutputCh <- barr: … case <-ctx.Done(): return "timeout sending request" }`,
  it fails immediately.
- The same code runs in wavesrv for ACKs (`StreamDataAckCommand` is NoResponse). A
  dropped final ACK wedges the stream too.

### Bug 4 in detail (router wedge)

- `/proc/<connserver>/fd/2` is a pipe (to sshd). The prod connserver logs to stderr
  (`cmd/wsh/cmd/wshcmd-connserver.go:453` only adds a file in dev mode).
- `sendRpcMessageToLink` (`wshrouter.go:338`) holds `router.lock` and calls
  `addToBacklog_withlock`, which calls `log.Printf("[router] backlog … reached %d messages")`
  (`:326`). That happens precisely when the upstream (ssh) link is not draining.
- `TestRouterStallsWhenLogOutputBlocks`: with log output blocked, the router accepts 68 to 71
  job messages and then the job socket reader blocks (`router.inputCh <-`, `:589`). With
  logging flowing it backlogs all 2000.

### Our incident, re-read

`23:29:22 SendData: timeout sending request`: exactly one. After it there are no more
SendData calls (the window was full). The PTY was not read, and pi (Node, synchronous tty
writes) blocked in `write()`. Its 20 s hub heartbeat stopped. At `23:50:34` the new
connserver reattached, `clientSeq == serverSeq` (nothing lost), and pi resumed in the same
second. A single timeout with no follow-up errors matches bug 1 (one spurious drop) better
than a sustained stall, which produces a timeout every 5 s until the window is full. The
laptop sleeping behind the VPN makes the socket back up (via bug 4 or plain ssh
backpressure), which makes bug 1 far more likely and turns bug 2 into a wedge. Either way,
once one packet is lost, nothing short of a reattach recovers it.

## 2. Reproduction

Harness: `cmd/test-jobfreeze` (on `durable-freeze-repro`). It plays wavesrv plus connserver
against a **real `wsh jobmanager`** process, using the same StartJob, PrepareConnect and
StartStream RPCs, the same 64 KB rwnd, and the same streamclient reader. A Node probe (like
pi: synchronous tty writes) writes lines to its tty and a heartbeat file after each batch.
The harness samples the heartbeat age, the bytes the client consumed, and
`/proc/<probe>/{stat,wchan}`. Then it injects a fault on the job socket, and finally
reattaches the way wavesrv does.

```
. /tmp/wds/env.sh     # PATH=/tmp/wds/go/bin, GOPATH/GOCACHE under /tmp/wds
go build -o /tmp/wds/bin/wsh-before ./cmd/wsh            # on durable-freeze-repro
go build -o /tmp/wds/bin/wsh-after  ./cmd/wsh            # on durable-never-block
cmd/test-jobfreeze/repro.sh /tmp/wds/final before=/tmp/wds/bin/wsh-before after=/tmp/wds/bin/wsh-after
# single scenario, verbose:
/tmp/wds/bin/test-jobfreeze -wsh /tmp/wds/bin/wsh-before -dir /tmp/wds/runs/x -fault hiccup -hiccup 7 -pertick 1 -tickms 1 -pad 20
```

Faults:

| fault | models |
|---|---|
| `blackhole` | the client reads the socket but drops stream data and never ACKs (half-open laptop, connserver alive and buffering) |
| `stopread` | the client stops reading the socket (connserver router wedged, bug 4) |
| `hiccup` | `stopread` for 7 s, then reads resume (a transient stall) |
| `close` | the client closes the socket (connserver exited: clean disconnect) |
| `-tmux` | the probe runs inside tmux (private `-S` socket); the job's pty hosts the tmux client |

`drip` means 20-byte lines every 1 ms, so packets are tiny like a TUI's. That makes the socket
buffer fill before the window does.

Results (`notes/repro-summary-before-after.txt`; 18 one-second samples per fault; full logs
are in `notes/repro-logs/`):

```
before  blackhole       frozen 16/18  end: FROZEN   after reattach: RUNNING  joblog: (nothing)
before  stopread-drip   frozen 15/18  end: FROZEN   after reattach: RUNNING  joblog: SendData timeout sending request
before  hiccup-drip     frozen 14/18  end: FROZEN   after reattach: RUNNING  joblog: 1x SendData timeout sending request   <- the incident signature
before  close           frozen 16/18  end: FROZEN   after reattach: RUNNING  joblog: 27x server is no longer running
before  tmux-blackhole  frozen  0/18  end: RUNNING
after   blackhole       frozen  8/18  end: RUNNING  (detached after 10 s stall timeout)
after   stopread-drip   frozen  2/18  end: RUNNING  (send failed -> detached)
after   hiccup-drip     frozen  2/18  end: RUNNING
after   close           frozen  0/18  end: RUNNING
after   tmux-blackhole  frozen  0/18  end: RUNNING
```

In every frozen sample the probe shows `state=S wchan=wait_woken` (blocked in `n_tty_write`)
and its heartbeat stops. In `before hiccup-drip` the client gets a little more data after the
socket is read again, then stops at the hole for good. One timeout line, a permanent freeze,
and a lossless reattach (`clientSeq == serverSeq`) are exactly what our incident and #3439
show.

Unit tests:

- `pkg/wshutil/wshrpc_noresp_race_test.go`: before, 4283 of 5000 NoResponse sends fail
  spuriously (under 1 ms each). After the fix, 0.
- `pkg/wshutil/wshrouter_logstall_test.go`: the router wedges when log output blocks (bug 4).
  On the fix branch `TestRouterNonBlockingLogOutput` shows it no longer does.
- `pkg/jobmanager/streammanager_test.go` (fix branch): a stalled client is detached and the
  reader keeps draining 4 MB; a slow but ACKing client keeps lossless backpressure; a send
  error detaches without loss.

## 3. Options compared

| | effort | risk | upstream fit | UX |
|---|---|---|---|---|
| A. tmux inside the durable session (user-level, today) | minutes | none to Wave | n/a | program never freezes (verified); Wave view can still wedge until reattach; scrollback lives in tmux |
| B. tmux-backed durable sessions (Wave feature) | large | medium-high | low as default, maybe opt-in | as A, and Wave features degrade (below) |
| C. job manager never blocks on a dead client (prototype) | small: about 150 lines in wsh only | low | high (fixes #3439 root causes, no protocol change) | program blocks at most for the stall timeout; lossless unless more than 2 MB was produced while detached |
| D. fix the RPC race and error handling (bugs 1 and 2) | tiny | low | high | removes the most common trigger |
| E. connserver non-blocking logging (bug 4) | tiny | low | high | connserver keeps serving other jobs during ssh stalls |
| F. dead-peer detection: sshd `ClientAliveInterval` | config | low | n/a | **does not fix v0.14.5**; see below |
| G. wavesrv stall watchdog (PR #3460, part 2) | small | low-medium | medium | recovers a wedged view while the laptop is awake; cannot help while it sleeps |

### A/B: tmux-backed

Verified with stock v0.14.5 (`before tmux-blackhole`, `before-tmux-hiccup`): the tmux
server keeps reading the pane no matter what happens to its client, so the probe never
froze. The Wave stream itself still wedges on the hole bug. The terminal looks frozen until
reattach, but the program keeps working, which is what matters for agents.

User-level recipe (no Wave change; `WAVETERM_JOBID` is set in durable job envs by
`jobcontroller.StartJob`). Put it in `~/.bashrc`, guarded so it runs only in durable Wave
terminals:

```bash
if [ -n "$WAVETERM_JOBID" ] && [ -z "$TMUX" ] && command -v tmux >/dev/null; then
  exec tmux -L wave new-session -A -s "wave-${WAVETERM_JOBID:0:8}"
fi
```

(`-L wave` gives a separate tmux server from the agents' existing tmux server.)

What a Wave-integrated version (B) would have to handle:

- Job creation: `Cmd = tmux -L wave-<clientid> new-session -A -s <jobid> -- <shell>` in
  `jobcontroller.StartJob`, or replace the job manager with `tmux attach` in a
  plain remote pty. The first is easy. The second throws away the job manager.
- Reattach: unchanged if the job manager stays. Otherwise `tmux attach` plus a redraw.
- Scrollback: Wave's WaveFS output file would hold tmux screen redraws, not program
  output. Wave's scrollback and search become nearly useless; you use tmux copy-mode
  instead. Replay after reattach turns into "tmux redraws the current screen" (which also
  hides gaps).
- Resize: fine (the job manager's `pty.Setsize` makes the tmux client resize the window).
- Exit status: the job manager sees the tmux client's exit, not the program's. It would need
  `remain-on-exit` plus `#{pane_dead_status}` or a wrapper.
- Shell integration and `wsh`: Wave's OSC sequences (cwd via OSC 7, the Wave
  shell-integration OSCs, `wsh` over OSC when there is no socket) are swallowed by tmux
  unless `allow-passthrough on` is set and each one is wrapped in a tmux DCS. The
  `WAVETERM_*` env vars (block id, JWT) of later panes come from the tmux server's
  environment, so `update-environment` handling would be needed. Mouse and keybindings
  conflict with the tmux prefix and mouse mode.
- Gains: a mature pane-draining server, attach from plain ssh when Wave is not around, and
  survival of a job manager crash.

Verdict: good as a user-level workaround now. Not the right fix for Wave, because Wave
built the job manager precisely to own the pty and scrollback.

### C: never stop draining (prototype on `durable-never-block`)

- `StreamManager`: if flow control blocks the PTY reader and there has been no ACK progress
  (acked bytes or window growth) for `DefaultStallTimeout = 10s`, detach the client. The
  buffer switches to async mode, which wakes the reader and keeps draining into the 2 MB
  ring (oldest bytes dropped). A slow but live client still gets lossless backpressure,
  because every ACK resets the timer.
- A send error detaches the client instead of leaving a hole.
- A detached client's connection is closed. connserver unregisters the job route, wavesrv
  gets a route-down event and runs its **existing** auto-reconnect
  (`jobcontroller.go:374`, `AutoReconnectDelay 1s`, 30 s cooldown), which does
  PrepareConnect/StartStream with gap accounting. No wire or wavesrv change is needed.
- The job socket's disconnect hook now runs when the connection actually ends (bug 3).
- Plus D (`wshrpc.go`): NoResponse requests are no longer registered, and
  `SendComplexRequest` returns an error instead of `(nil, nil)` after a recovered panic.
  The harness hit that: a closed rpc crashed the process through `SendCommand →
  handler.finalize()` on a nil handler.
- Plus E: connserver logs through `logutil.NonBlockingWriter` (drops lines when stderr is
  stalled).
- Knobs and follow-ups: the stall timeout (5 s would match the RPC timeout). A connected-mode
  buffer larger than the 64 KB window (say 256 KB) would absorb short stalls with no freeze
  at all, at the cost of Ctrl-C latency on runaway output (bufferbloat). The connserver
  router backlog is unbounded: sysinfo and events pile up during long half-opens.

### F: dead-peer detection

- VM sshd `ClientAliveInterval 30` + `ClientAliveCountMax 4`: turns a half-open connection
  into a clean disconnect after about 2 minutes. connserver exits and the job sockets
  close. **With v0.14.5 that still freezes the program after 64 KB** (bug 3,
  `before close`). With fix C it would also free connserver's resources sooner, so it is
  worth having, but it is not the fix.
- Laptop `ServerAliveInterval`: Wave uses its own Go ssh client. Its `ConnMonitor` sends
  `keepalive@openssh.com` and only flips a "Stalled" health status, and none of it runs
  while the laptop sleeps. It cannot help the VM side.
- TCP keepalive (`tcp_keepalive_time 7200`): far too slow, and a sysctl change anyway.
- Stream-level: an ACK-progress timeout while blocked is the dead-peer detection that
  matters, and it is what C implements. It needs no protocol change because it only acts
  while the job manager is actually blocked.

### Upstream state (checked 2026-09-26)

- #3439 (open), "Durable SSH terminal remains falsely Attached and non-interactive after RPC
  stream timeout": same signature, with "tail -f + bad network" as the easiest trigger.
  https://github.com/wavetermdev/waveterm/issues/3439
- #2985 (open), "Terminal Running Codex in Durable Session Looses Connection and Requires
  Restart": the same symptom. Disconnect/Reconnect recovers it.
  https://github.com/wavetermdev/waveterm/issues/2985
- PR #3460 (open, CLA not signed, not merged): send errors disconnect the client, plus a 45 s
  wavesrv read watchdog. It covers bug 2's symptom but not bug 1's cause, bug 3, bug 4, or the
  no-ACK half-open case where the job manager never sees an error.
  https://github.com/wavetermdev/waveterm/pull/3460
- #3248 (durable sessions for local terminals) is unrelated.

## 4. Recommendation

Local, today (needs the user's go-ahead where noted; I changed nothing live):

1. For agent terminals, run the agent under tmux (the recipe above, or the existing
   tmux setup). That is the only protection available without touching Wave, and it is
   verified.
2. When a durable terminal freezes, use the connection menu **Disconnect → Reconnect**. It
   reattaches without killing anything. Do **not** use "Force Restart Controller": in
   v0.14.5 it terminates the session (#3439).
3. Optional, user decision: build `wsh` from `durable-never-block` with the version string
   set to 0.14.5 and put it in `~/.waveterm/bin`. Only **new** durable sessions and the next
   connserver would use it; the running job managers keep the old binary. Wave may reinstall
   `wsh` on a version mismatch. I have not done this.
4. Optional sysadmin: `ClientAliveInterval 30`, `ClientAliveCountMax 4` in sshd. This is
   hygiene, not a fix (see F).

Upstream:

- Comment on #3439 with the root causes (bugs 1 to 4) and repro. Draft:
  `notes/upstream-issue-comment-3439.md`.
- PR from `durable-never-block` (after the CLA). Draft: `notes/upstream-pr-draft.md`. It could
  be split into (i) the wshrpc NoResponse race and the nil-handler fix, (ii) the job manager
  send-error, stall-detach and disconnect-hook fixes, and (iii) connserver non-blocking logs.
  (i) alone probably removes most occurrences. (ii) makes any remaining cause harmless.
