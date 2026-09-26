#2985 [open] [Bug]: Terminal Running Codex in Durable Session Looses Connection and Requires Restart
https://github.com/wavetermdev/waveterm/issues/2985
comments=3
### Current Behavior

When running an interactive process (specifically gpt-5.3-codex), the terminal UI stops updating during interactive prompts (e.g., tool-use confirmations or user questions). The "Working" counter freezes, and the input field becomes unresponsive ("dead"). A manual disconnect and reconnect is required to force the display to catch up and reveal the pending interactive prompt.

Steps to Reproduce

Connect to a remote host via a Durable Session.

<img width="642" height="346" alt="Image" src="https://github.com/user-attachments/assets/7a96a034-921c-4320-8c07-cf75ff3ac1a9" />

Run a command that requires a tool-use confirmation or mid-process user input (e.g., Summarize recent commits via Codex).

Observe the "Working" counter in the terminal block.

The Bug: The counter stops incrementing, and the input field no longer accepts keystrokes.

<img width="642" height="348" alt="Image" src="https://github.com/user-attachments/assets/4df7954e-e023-41ed-b4ed-3d69e58948c9" />

Manually select "Disconnect" and then "Reconnect" from the connection menu.

<img width="644" height="347" alt="Image" src="https://github.com/user-attachments/assets/04a77350-43ae-4203-841c-353db8c9ddcc" />

Hit the reconnect button

<img width="645" height="348" alt="Image" src="https://github.com/user-attachments/assets/a1583c16-4ca6-4e21-81ae-a6e775a3bdd1" />

Observe that the display "snaps" to the current state, showing the interactive question that was previously hidden.

Actual Behavior:
- The UI fails to stream or refresh when the remote process transitions to an interactive input state.
- The "Working" timer freezes (e.g., stuck at 11m 43s despite the process continuing).
- Input is "dead" until a manual session cycle (Disconnect/Reconnect) is performed, at which point the terminal buffer updates to the correct state.

### Expected Behavior

The terminal should remain responsive and immediately display interactive prompts or tool-use confirmations without requiring a manual session refresh.

### Steps To Reproduce

Connect to a remote host via a Durable Session.

<img width="642" height="346" alt="Image" src="https://github.com/user-attachments/assets/7a96a034-921c-4320-8c07-cf75ff3ac1a9" />

Run a command that requires a tool-use confirmation or mid-process user input (e.g., Summarize recent commits via Codex).

Observe the "Working" counter in the terminal block.

The Bug: The counter stops incrementing, and the input field no longer accepts keystrokes.

<img width="642" height="348" alt="Image" src="https://github.com/user-attachments/assets/4df7954e-e023-41ed-b4ed-3d69e58948c9" />

Manually select "Disconnect" and then "Reconnect" from the connection menu.

<img width="644" height="347" alt="Image" src="https://github.com/user-attachments/assets/04a77350-43ae-4203-841c-353db8c9ddcc" />

Hit the reconnect button

<img width="645" height="348" alt="Image" src="https://github.com/user-attachments/assets/a1583c16-4ca6-4e21-81ae-a6e775a3bdd1" />

Observe that the display "snaps" to the current state, showing the interactive question that was previously hidden.

### Wave Version

v0.14.1 (202603032325)

### Platform

macOS

### OS Version/Distribution

_No response_

### Architecture

arm64

### Anything else?

_No response_
