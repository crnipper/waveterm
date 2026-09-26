#3439 [open] [Bug]: Durable SSH terminal remains falsely Attached and non-interactive after RPC stream timeout
https://github.com/wavetermdev/waveterm/issues/3439
comments=2
## Current Behavior

A durable SSH terminal intermittently becomes completely non-interactive:

- Terminal output stops updating.
- Keyboard input has no effect.
- The shield continues to show **Durable Session (Attached)**.
- Wave reports the SSH connection as connected.
- The remote shell and foreground application remain healthy.
- Fully quitting and reopening Wave successfully reattaches to the original session and restores interactivity.

In the captured occurrence, a Codex process was running in the terminal. After the Wave block stopped responding, Codex continued working remotely and completed its task. The original processes remained alive:

```text
PID 1964  wsh jobmanager
PID 1973  bash
PID 3100  codex
```

After restarting Wave, the same PIDs were preserved and the existing session became responsive again.

The remote durable-session log recorded:

```text
2026/07/20 16:09:19 SendData: error sending stream data: timeout sending request
```

Wave did not mark the job disconnected after this error. The shield remained Attached and automatic reconnection did not occur.

When Wave was restarted, the remote log showed:

```text
2026/07/20 18:57:45 AuthenticateToJobManager: authentication successful
2026/07/20 18:57:45 SetAttachedClient: kicking out existing client
2026/07/20 18:57:45 connectToStreamHelper: disconnecting existing client
2026/07/20 18:57:45 connectToStreamHelper: detached old stream id=cf519bf0-ae5e-4a93-9119-53c63534e019
2026/07/20 18:57:45 PrepareConnect: streamid=b21b548f-887d-4e49-8d3d-025521da369c clientSeq=19863439 serverSeq=19863439 streamDone=false streamError="" hasExited=false
2026/07/20 18:57:45 StartStream: streamid=b21b548f-887d-4e49-8d3d-025521da369c rwnd=65536 streaming started
```

`clientSeq` and `serverSeq` were identical and `totalGap=0` in the local Wave log, indicating the backend had the complete terminal stream even though the terminal block was frozen.

There was an earlier local WebSocket timeout for the exact tab and block:

```text
2026-07-20 15:35:21.507 [websocket] WritePump error
write tcp 127.0.0.1:45359->127.0.0.1:33439: i/o timeout

wshrouter unbind route "feblock:777680bf-221f-4bba-9cb3-5689b245b1f3"
wshrouter unregister link 6#[ws:tab:9c4d3ecd-3fda-4dd1-99bc-dc1aa0879e74]
```

Wave immediately created another WebSocket link and rebound the block, but the terminal later became unresponsive.

Similar remote `SendData: timeout sending request` errors occurred on July 15, July 19, and twice on July 20 across durable sessions.

## Expected Behavior

When the durable job's RPC or stream path stops accepting data:

1. Wave should mark the durable job Detached or Stalled.
2. The shield should not continue showing Attached.
3. Wave should close the stale route and automatically reconnect to the existing job manager.
4. Output and input health should be checked end-to-end rather than relying only on route registration.
5. Recovery should not require restarting the entire Wave application.

## Steps To Reproduce

The failure is intermittent:

1. Run Wave v0.14.5 on Windows.
2. Open an SSH terminal to a Linux host.
3. Enable Durable Session.
4. Run a long-lived interactive TUI that produces sustained output, such as Codex CLI.
5. Leave the session running for several hours.
6. Eventually, the terminal may stop updating and accepting input.
7. Observe that the shield still says **Durable Session (Attached)** and the SSH connection still says connected.
8. Verify from another SSH session that the remote shell/application remains alive.
9. Fully quit and reopen Wave.
10. Observe that Wave reattaches to the original shell and application and the terminal becomes responsive.

## Environment details

**Wave Version:** v0.14.5 (build 202604161539), Electron 41.1.0, remote `wsh` v0.14.5

**Platform:** Windows x64

**Remote SSH host:** Linux 6.8.0-136-generic x86_64

## Additional investigation

The v0.14.5 source appears to explain why the state remains falsely Attached.

`routedDataSender.SendData` [logs and ignores errors](https://github.com/wavetermdev/waveterm/blob/v0.14.5/pkg/jobmanager/mainserverconn.go#L44-L50).

The RPC enqueue [times out after five seconds](https://github.com/wavetermdev/waveterm/blob/v0.14.5/pkg/wshutil/wshrpc.go#L763-L808).

The frontend [displays Attached solely when the stored job status is `connected`](https://github.com/wavetermdev/waveterm/blob/v0.14.5/frontend/app/block/durable-session-flyover.tsx#L264-L279).

The observed sequence suggests:

1. The job manager's bounded outbound channel or downstream Unix-socket writer stopped draining.
2. `SendData` timed out.
3. The error was logged but did not close the stale connection.
4. No route-down event was generated.
5. The job status remained `connected`, leaving the shield falsely Attached.
6. Restarting Wave rebuilt the router and reconnected the original job successfully.

There is also a related recovery problem: the documentation recommends **Advanced -> Force Restart Controller** for a session that will not reconnect, but in v0.14.5 that action [destroys the controller](https://github.com/wavetermdev/waveterm/blob/v0.14.5/frontend/app/view/term/term-model.ts#L789-L804). Destroying a durable controller calls [`TerminateAndDetachJob`](https://github.com/wavetermdev/waveterm/blob/v0.14.5/pkg/blockcontroller/durableshellcontroller.go#L192-L203).

In practice, Force Restart Controller terminates the preserved session and opens a new bash shell. This should either reconnect non-destructively or be labeled as terminating the existing session.

Full logs are available, but the raw `waveapp.log` contains Wave routing tokens and would need to be redacted before attachment.

