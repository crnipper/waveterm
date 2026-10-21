# Wave remote-browser Windows build — results (interim)

Status: **BUILD PASSED** · DEPLOYED: no (side-by-side app extracted, not launched) · LIVE TESTS: not run

Date: 2026-10-21. Host: Windows 11 Enterprise 10.0.26200, X64. Agent ran inside stock Wave
(block `882336b8-0e5c-489c-8a8d-c1c93252629d`), so it stopped before quitting Wave.

## Source
- `ssh vm hostname` → `remote-host`.
- Archive `waveterm-remote-browser-source.tar.gz` SHA256
  `8303eaade9013c05e3b9471026fa1d3c587bdc69b40baa10e9454f487f3e8c09` — matches.
- Extracted to `C:\Users\user\sources\wave-browser-handoff\waveterm-remote-browser`.
  All required files present; `getWidgetBlockDef`, `InheritCwd`/`inheritcwd` in Go, schema, TS types.
- Local-only git baseline commit `51a8825` (no remote) to track build-induced changes.

## Toolchain
| Tool | Version | Notes |
|---|---|---|
| Node | v22.23.4 (npm 10.9.9) | Portable, `C:\Users\user\Tools\node-v22.23.4-win-x64`, SHA256 verified vs nodejs.org. Used only for this build; system Node v26.4.0 untouched. |
| Go | 1.25.7 | existing (go.mod needs 1.25.6) |
| Zig | 0.17.0 | winget `zig.zig`, user scope (upstream CI uses unpinned latest via setup-zig) |
| Task | 3.54.0 | winget `Task.Task`, user scope |
| Python | 3.14.0 | existing |

Node 26 was tried first and rejected: npm 11.17 gates install scripts (`allowScripts`) and Electron's
extraction silently produced an incomplete `dist`. BUILD.md and upstream CI both specify Node 22.
Electron download needed `ELECTRON_GET_USE_PROXY=1` + `GLOBAL_AGENT_HTTP(S)_PROXY` from existing
proxy env (TLS validation unchanged).

## Commands and exit codes (logs in `wave-browser-handoff\logs\`)
| Step | Exit |
|---|---|
| `npm ci` (Node 26, no Electron proxy) | 1 — Electron download ETIMEDOUT |
| `npm ci` (Node 22, proxy env) | 0 |
| widget tests (widgetlaunch, widgetfilter) | 0 — 2 files, 5 tests passed |
| `py -3 scripts/test_remote_browser.py` | 0 — 3 tests OK |
| `npx tsc --noEmit` | 2 — exactly the 17 baseline preview-mock errors (15× `numthreads`, `version`/`buildtime`, `getPathForFile`); none in modified files |
| `task package -- --win zip --x64` | 0 |

Build-induced source change: only `package-lock.json` version `0.14.5-beta.1` → `0.14.5`
(npm install syncing to package.json). go.mod/go.sum and generated files unchanged.

## Package
- `make\Wave-win32-x64-0.14.5.zip`, 218,331,579 bytes,
  SHA256 `25350cfe77daecc5834f58115473fa7be7dda1caebe51bf3f57423553dc3951b`.
- Contains `Wave.exe`, `resources/app.asar`, `app.asar.unpacked/dist/bin/wavesrv.x64.exe`
  (SHA256 `e984973e…0feb`), all 8 `wsh-0.14.5-*` helpers, `dist/schema/widgets.json` with `inheritcwd`.
- `wsh-0.14.5-linux.x64` SHA256 `6a91406a8eaef5c8244664d1a7f8881952aab4d9f79e2e472821b0a3583bca87`.
- Frontend bundle in app.asar references `inheritcwd`.
- Unsigned: Authenticode `NotSigned` for Wave.exe and wavesrv (electron-builder signtool lines were no-ops; no cert env).

## Deployment prep done (non-interrupting)
- Side-by-side app extracted to `C:\Users\user\Apps\Wave-remote-browser-0.14.5\` (not launched).
  Stock install untouched: `C:\Users\user\AppData\Local\Programs\waveterm\Wave.exe` (0.14.5).
- Config backup (pre-change): `C:\Users\user\Backups\wave-cutover-<timestamp>\pre-change-config\`
  (exact path in `wave-browser-handoff\backup-dir.txt`; ACL user/SYSTEM/Administrators only).
- `settings.json`: added `"autoupdate:enabled": false`, `"autoupdate:installonquit": false`
  (originals: both absent → defaults). Running stock updater reads these only at startup;
  last check 14:13 = `update-not-available`, so no pending update on quit.
- Not yet done: `widgets.json` `inheritcwd` edit, cold data backup, cutover.

## Baseline (pre-cutover)
- Stock Wave PID 12968 + wavesrv PID 23104 from the stock install path.
- Config `C:\Users\user\.config\waveterm`; data `C:\Users\user\AppData\Local\waveterm\Data`
  (168 MB); Electron profile `C:\Users\user\AppData\Roaming\Wave` (102 MB).
- VM helpers match supplied sources byte-for-byte (`wave-remote-browser` 32fab8b7…,
  `wave-cwd.yazi/main.lua` 5226f545…); `.bashrc` sources `remote-browser.sh`; Yazi init has wave-cwd + zoxide.

## Open finding: remote `wsh` will NOT be upgraded automatically
VM `~/.waveterm/bin/wsh` is the stock binary (SHA256 `13bfbea2…936f`, identical to stock package).
`IsWshVersionUpToDate` compares semver only; custom build is also `v0.14.5`, so it will be treated as
up to date and **not installed**. The click feature does not need it (frontend + `cmd:env` + Python
helper), but the durable-freeze fixes in commit `70e1d115` are mostly remote-side
(`wshcmd-connserver.go`, `pkg/jobmanager/*`, `wshrpc.go`, nonblocking log writer) and will not be
active on the VM unless `wsh` is replaced deliberately. Needs Chris's decision.

## Rollback (current state)
Nothing deployed yet. To undo prep: restore `settings.json` from the pre-change backup (or delete the
two `autoupdate:*` keys); optionally delete `C:\Users\user\Apps\Wave-remote-browser-0.14.5`.
