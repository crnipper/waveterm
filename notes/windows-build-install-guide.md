# Building the fork on Windows

## Current source of truth

Clone `https://github.com/crnipper/waveterm.git` and check out
`durable-remote-browser`. Use the current branch's BUILD.md, go.mod, Taskfile.yml,
and `notes/remote-browser-installation.md`. This branch was rebased onto upstream
main `9f1c9670d407be3b93cf090319a63ef30bf54d0e`; Go now requires 1.26.8.
No tarball transfer is needed. Original history is preserved on
`archive/durable-browser-pre-rebase`; do not build that by accident.

The detailed runbook below documents the initial 0.14.5 snapshot workflow. Its
checksum, base SHA, user paths, tool versions, and snapshot instructions are
HISTORICAL, not current clone instructions. Adapt connection/user paths to the
actual deployment. The cold-backup, side-by-side cutover, config merge, test
matrix, and rollback precautions remain applicable. Build and live-test again:
the historical Windows package did not include the new upstream main changes.

---

# Windows agent handoff: custom Wave build, deployment, and live tests

## Objective and authority

Build the supplied modified Wave source on Windows, deploy it side-by-side, enable
selected-directory behavior for **VM Files** and **VM Git**, and live-verify it.
Keep the stock installation available for rollback. Do not publish, push, sign
with organizational credentials, uninstall stock Wave, or restart/terminate other
users' processes. Ask Chris before a step that would interrupt active work.

If the agent itself runs inside Wave, do all build/preparation first and arrange
a separate execution/reporting channel before quitting Wave. Do not kill the
application hosting your only agent session. A normal Wave quit should detach
remote durable sessions; closing a block/tab/workspace terminates its sessions.

Read this entire document before making changes. Report actual results, not
inferred success. Build failures are not permission to weaken validation.

## What exists now

- Linux host reached by Windows SSH alias `vm`: `remote-host`, user `crnipper`.
- Wave connection canonical name: **`user@host`**. Keep that exact spelling.
- Source: `/home/user/sources/waveterm`, upstream remote
  `https://github.com/wavetermdev/waveterm.git`, branch `durable-never-block`.
- Base HEAD: `70e1d11537fd5023b78e7afaa2dae29847f31288`.
- No fork remote is configured. Feature changes are uncommitted local edits.
- The snapshot below includes those edits; cloning upstream alone is incorrect.
- Version remains `0.14.5`; do not bump it gratuitously.
- This branch ALSO contains the earlier durable-session freeze fixes. A full
  package includes their backend/remote-helper changes, not merely the launcher.
  Do not cherry-pick away those fixes without discussing scope with Chris.
- Windows currently runs stock Wave. New click behavior is **not live yet**.
- Remote apps/config already installed: Yazi, Lazygit, Delta, Zoxide, fzf;
  Yazi plugins/bindings; directory resolver and Yazi tracking plugin.
- Lazygit recentrepos was seeded with 59 repo/worktree paths. Its state is
  `/home/user/.local/state/lazygit/state.yml`; do not overwrite it.

Verified on Linux: five widget tests, three resolver tests, production frontend
build, and live Yazi navigation/directory resolution. This is NOT a Windows
backend build or end-to-end Windows click test.

Full `tsc --noEmit` reported 17 existing preview-mock errors: missing `version`/
`buildtime` in `frontend/preview/mock/defaultconfig.ts`, `getPathForFile` in
`preview-electron-api.ts`, and `numthreads` in processviewer preview fixtures.
Those required fields already exist in base HEAD. Record these separately;
new errors in modified files must be fixed, not dismissed as baseline.

## 1. Transfer the exact source

On Windows, first verify `ssh vm hostname` reaches the expected host using the
existing trusted SSH configuration. Do not disable host-key checking.

Download these two files from the VM:

```powershell
$handoff = Join-Path $HOME 'sources\wave-browser-handoff'
New-Item -ItemType Directory -Force $handoff | Out-Null
scp 'vm:/home/user/sources/wave-remote-browser-windows-build-install-guide.md' $handoff
scp 'vm:/home/user/sources/waveterm-remote-browser-source.tar.gz' $handoff
Get-FileHash (Join-Path $handoff 'waveterm-remote-browser-source.tar.gz') -Algorithm SHA256
```

Expected SHA256: **8303eaade9013c05e3b9471026fa1d3c587bdc69b40baa10e9454f487f3e8c09**

Stop if it differs. Extract into a NEW directory, never over an existing clone:

```powershell
$archive = Join-Path $handoff 'waveterm-remote-browser-source.tar.gz'
tar -xzf $archive -C $handoff
if ($LASTEXITCODE -ne 0) { throw 'Source extraction failed' }
$repo = Join-Path $handoff 'waveterm-remote-browser'
Set-Location $repo
```

The snapshot has no `.git`, dependencies, or credentials. It contains the
complete selected source files, including generated schema/types and local fixes.
If git tooling is needed, initialize a LOCAL repository and baseline snapshot;
do not add/push a remote or accidentally run a reset that removes supplied edits.
Check these files exist and have expected content:

- `frontend/app/workspace/widgetlaunch.ts`: same-connection selected-block capture.
- `frontend/app/workspace/widgets.tsx`: calls `getWidgetBlockDef` before `createBlock`.
- `pkg/wconfig/settingsconfig.go`: `InheritCwd` field with `inheritcwd` JSON tag.
- `schema/widgets.json` and `frontend/types/gotypes.d.ts`: generated `inheritcwd`.
- `scripts/remote-browser.py`, `scripts/test_remote_browser.py`.
- `scripts/wave-cwd.yazi.lua` and `notes/remote-browser-widgets.md`.

## 2. Inspect Windows and prepare dependencies

Read `BUILD.md`, `Taskfile.yml`, `electron-builder.config.cjs`, and the local
project instructions before building. Preserve any existing tool installations.
Determine native architecture before selecting x64 vs ARM64. x64 examples below
are only for an x64 host; on ARM64 use the matching package target and Zig target.

Discover versions/paths:

```powershell
node --version
npm --version
go version
zig version
task --version
[System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
Get-Command node,npm,go,zig,task,git -ErrorAction SilentlyContinue
```

Required: Node 22 LTS or the compatible existing Node, Go satisfying `go.mod`
(`go 1.25.6`), Zig, Task, Git, and working network access to npm/Go/Electron.
Use trusted official/tooling sources; install missing tools only as authorized,
prefer user scope, and record versions. Zig compatibility is not pinned by the
handoff: investigate compiler errors against the project's build recipes rather
than guessing a version. Refresh PATH in this shell after installation.

Do not copy Linux node_modules/dist/bin to Windows. Allow the repository to
install Windows dependencies and rebuild native backend components. Keep proxy/
CA settings appropriate for corporate infrastructure; do not disable TLS validation.
Do not print tokens or include them in logs/results.

## 3. Tests and full Windows package

Run all native commands with exit-code checks. PowerShell does not automatically
throw on a nonzero native exit code. Capture logs under `$handoff`, outside source.

```powershell
Set-Location $repo
npm ci
if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
npm test -- --run frontend/app/workspace/widgetlaunch.test.ts frontend/app/workspace/widgetfilter.test.ts
if ($LASTEXITCODE -ne 0) { throw 'Widget tests failed' }
# Use the available Windows Python 3 interpreter, e.g. py -3 or python:
py -3 scripts/test_remote_browser.py
if ($LASTEXITCODE -ne 0) { throw 'Resolver tests failed' }
npx tsc --noEmit
# Record this exit code and errors; compare against the baseline described above.
# Do not treat new errors as baseline.
task package -- --win zip --x64
if ($LASTEXITCODE -ne 0) { throw 'Windows packaging failed' }
```

`task package` builds the native Windows `wavesrv`, versioned `wsh` helpers,
generated schema/types, tsunami scaffold, frontend, and Electron ZIP. It also
runs dependency/tidy tasks; record resulting source changes. Do not merely run
`npm run build:prod` and label it installable.

Package output: `make/`, conventionally `Wave-win-x64-0.14.5.zip`. Discover the
actual produced filename, record SHA256/size, and inspect its contents. Require:

- `Wave.exe`, Electron resources, `resources/app.asar`.
- Unpacked Windows backend `resources/app.asar.unpacked/dist/bin/wavesrv.x64.exe`.
- Remote Linux x64 `wsh-0.14.5-linux.x64` helper (plus other helpers from recipe).
- Updated widgets schema under unpacked `dist/schema`.

Code signing is not configured unless signing credentials are supplied. Expect
an unsigned private build. Do not disable SmartScreen/security policy globally;
route policy blocks to Chris. A ZIP package avoids NSIS/MSI installation changes.

## 4. Baseline, backup, and safe cutover

Current stock executable was observed at:
`C:\Users\user\AppData\Local\Programs\waveterm\Wave.exe`.
Verify the actual process path; do not assume a stale path. Record running Wave/
wavesrv processes, executable versions, active workspace/tab/block identities,
connections, and remote tmux sessions without capturing terminal secrets.

Find REAL Wave config/data paths before cutover. From a connected Wave terminal:

```bash
wsh wavepath config
wsh wavepath data
wsh wavepath log
```

Observed config is `C:\Users\user\.config\waveterm`. Modern Windows data
usually lives beneath `%LOCALAPPDATA%\waveterm` (env-paths data subdirectory),
but environment overrides and legacy `.waveterm` profiles can change it. Inspect
`emain/emain-platform.ts` and the reported paths; do not guess. Back up config,
full data including SQLite/WAL files, and Electron profile if distinct. These
may contain secrets: keep backups private, never commit/upload/report contents.

Before quitting stock Wave, merge into its settings (preserving all other keys):

```json
{
  "autoupdate:enabled": false,
  "autoupdate:installonquit": false
}
```

If an update is already pending, verify the app has consumed these settings and
coordinate exit with Chris. Disabling checks does not cancel an already-running
update. Record original values for rollback.

Arrange a graceful quit with Chris. **Do not close blocks, switch their
connections, delete tabs/workspaces, or use Force Restart Controller.** Verify
local processes are gone before copying live database files or opening another
build. Never run two copies against the same profile. If graceful quit hangs,
ask before force-killing; do not assume that is harmless.

Make a consistent full backup after shutdown. Extract the custom ZIP to a
versioned user directory such as `$HOME\Apps\Wave-remote-browser-0.14.5`.
Leave stock binaries untouched. Run extracted `Wave.exe` directly; a shortcut to
this exe is the side-by-side installation. Do not use `npm run dev` for cutover:
dev mode uses a different profile and is not proof of existing session recovery.

The custom production app intentionally uses the existing production profile.
Confirm its actual executable and child wavesrv paths after launch (the shared
app ID/single-instance behavior can otherwise route launches to stock Wave).

## 5. Enable widgets without changing the durability contract

After confirming the modified app is running, edit only these widget entries in
`C:\Users\user\.config\waveterm\widgets.json` (or the discovered config
path). Add top-level `"inheritcwd": true` to `remote-yazi` and `remote-lazygit`.
Preserve other entries and all existing blockdef keys. Expected shape:

```json
{
  "remote-yazi": {
    "icon": "folder-open",
    "label": "VM Files",
    "inheritcwd": true,
    "blockdef": {
      "meta": {
        "view": "term",
        "controller": "shell",
        "connection": "user@host",
        "term:durable": true,
        "cmd:env": { "WAVE_TMUX": "1", "WAVE_BROWSER_STARTUP": "yazi" }
      }
    }
  },
  "remote-lazygit": {
    "icon": "code-branch",
    "label": "VM Git",
    "inheritcwd": true,
    "blockdef": {
      "meta": {
        "view": "term",
        "controller": "shell",
        "connection": "user@host",
        "term:durable": true,
        "cmd:env": { "WAVE_TMUX": "1", "WAVE_BROWSER_STARTUP": "lazygit" }
      }
    }
  }
}
```

Do not replace the entire file with this example. Keep existing descriptions,
labels, ordering, and unrelated entries. Ensure JSON parses and Wave reports no
new config errors. Leave `connections.json` and the remote `.bashrc` tmux wrapper
unchanged: `user@host` has durability enabled and `WAVE_TMUX=1`.

## 6. Remote helper contract and deployment status

Already installed on VM:

- `~/.local/bin/wave-remote-browser` (stdlib Python, executable).
- `~/.config/waveterm/remote-browser.sh` sourced after the existing tmux wrapper.
- `~/.config/yazi/plugins/wave-cwd.yazi/main.lua`.
- `~/.config/yazi/init.lua`: `require("wave-cwd"):setup()` and zoxide setup.

Compare installed files with supplied scripts before testing. Do not redeploy or
restart anything just because it exists. Restart only a NEW/disposable Yazi
instance to load plugin changes; existing Yazi processes do not hot-load them.

How it works:
1. The UI captures the selected terminal before creating/focusing the new block.
2. Only the SAME connection inherits context. It passes source block ID and
   optional Wave cwd through `cmd:env`, never shell string interpolation.
3. The remote helper reads the selected block's private tmux socket,
   `wave-<first 8 characters of block UUID>`, matching the current wrapper.
4. It uses active Yazi's tracked directory when Yazi is a direct child of the
   shell; otherwise tmux's live pane cwd, then Wave cwd, then configured defaults.
5. Lazygit resolves the repository containing that directory with `git rev-parse`.
   Non-repo paths produce a message and a shell, NEVER `git init`.
6. Defaults remain `~/sources` for Files and `~/sources/qatwin-ai` for Git.

Limitations to test/report honestly:
- No selection, non-terminal, or different-host selection uses defaults.
- `wsh launch` does not capture UI focus; it still uses defaults. Test BUTTON CLICKS.
- Current resolver recognizes this user's existing tmux socket convention, not
  arbitrary tmux nesting/multiple-pane policies. It reads that socket's active pane.
- A Yazi nested below another process may fall back to shell cwd; direct launcher
  Yazi is the tested setup.
- These are launch buttons: they create new blocks. They do NOT retarget an
  already-open Lazygit window. Ctrl+R/worktrees still handle in-place switching.

## 7. Required live test matrix (record evidence for every row)

Use dedicated test blocks and known safe repos, e.g. `qatwin-ai`, `qatwin-ci`,
and their existing worktrees. Do not switch branches, commit, stage, push, delete
files, or create repositories during these tests. Record source block IDs,
expected paths, created block IDs, actual remote cwd/repo, and tmux metadata.
Do not expose file contents from private repos in reports.

A. **Existing session continuity:** existing workspaces/blocks reappear, remote
   durable shells reattach, previously running work survives. Capture baseline
   vs post-cutover IDs/processes. No Force Restart Controller.
B. **Shell → Files:** cd a selected remote shell to `~/sources/qatwin-ci`, click
   VM Files, confirm new Yazi opens exactly there.
C. **Shell → Git:** selected shell at a subdirectory inside `qatwin-ai`, click
   VM Git, confirm Lazygit opens its root, not the fallback/wrong repo.
D. **Two source panes:** A at qatwin-ai, B at qatwin-ci. Click after focusing A,
   then B, verify each uses the selected source; no last-used-shell heuristic.
E. **Yazi → Files/Git:** open fresh launcher Yazi, browse to a DIFFERENT repo
   than its startup path, then click buttons. Confirm current browsing dir wins.
   Repeat after navigating out of that directory so stale data cannot pass.
F. **Worktree:** select a shell/Yazi in an existing qatwin-ai or qatwin-ci
   worktree. Git must open that worktree's root, not the main checkout.
G. **Non-repo safety:** selected shell at `~/sources`, click Git. It must display
   the non-repo message and leave a shell; verify no new `~/sources/.git` exists.
H. **Fallback/isolation:** select a non-terminal or other-host terminal; widgets
   retain VM defaults. Do not pass foreign-host paths to user@host.
I. **Metadata:** use `wsh getmeta -b <new-block-id>`: shell controller,
   `term:durable=true`, connection user@host, WAVE_TMUX=1. On VM check private
   tmux server, live app process and `exit-unattached on` (without changing it).
J. **App exit:** q from Yazi/Lazygit leaves a normal shell and does not relaunch
   recursively. Child shells must not inherit stale source-launch flags.
K. **Durability:** on disposable test blocks, perform a graceful Wave quit and
   reopen custom Wave. Verify IDs, tmux sessions, directory, app state survive.
   Network disconnect/reconnect test only if authorized and bounded; never reboot
   or kill production connservers/job managers to manufacture a disconnect.
L. **Regression:** Wave file preview/edit bindings and Delta/Lazygit rendering
   still work; avoid editing real files solely for tests. Recent repos include
   seeded repositories/worktrees. No unrelated config changes.

Useful remote diagnostics (quote arguments safely from Windows):

```bash
wsh getmeta -b <block-uuid>
tmux -L wave-<first8> list-panes -F '#{pane_pid} #{pane_current_path} #{@wave-yazi-cwd}'
tmux -L wave-<first8> show-options -s exit-unattached
```

The helper requires Python 3 and a valid UUID source; missing/timeout tmux queries
fall back safely. If tests fail, investigate deterministic evidence, patch only
necessary source/helper files, rebuild, and rerun failed rows. Do not declare
end-to-end success solely from unit tests or widget presence.

Bundled new `wsh` does not automatically mean every running remote job uses it:
old job managers retain their old binary. Capture helper versions/hashes and
state which sessions tested new vs existing helpers. Do not overwrite/kill
running job managers manually; preserve continuity.

## 8. Finish and rollback

If all applicable rows pass, keep the side-by-side custom app and create a clearly
named shortcut pointing at it. Keep automatic checks/install-on-quit disabled
for this profile; do not change version numbers to simulate update safety.
A conventional installer/uninstall replacement is unnecessary and requires a
separate decision. Keep the original install and backups until Chris is satisfied.

Rollback:
1. Gracefully quit custom Wave without deleting terminal blocks.
2. Restore widgets/settings from backup or remove only `inheritcwd` and restore
   original update settings. Stock schema does not know the new widget field.
3. Start the verified stock executable, confirm it is the actual running app.
4. If data compatibility problems occur, preserve the post-test data, then restore
   the FULL cold backup (not individual SQLite files). Explain that restoring old
   layout/data can orphan test blocks; coordinate cleanup, never mass-kill sessions.
5. Remote browser helper/plugins remain backward compatible with stock widgets;
   they fall back to original launch directories. Do not remove working tooling.

Write a concise result report beside this guide on Windows and copy it back to
`/home/user/sources/wave-remote-browser-windows-results.md` if SSH permits.
Include source/archive checksum, tool versions, commands/exit codes, package hash,
actual exe/backend paths, backup locations (no secrets), config changes, test
matrix results, unresolved failures, and precise rollback instructions. Distinguish
BUILD PASSED, DEPLOYED, and LIVE TESTS PASSED. Leave source fixes reviewable and
remove disposable test artifacts/blocks only after inspecting their contents and
confirming they belong to this task. Never remove the user's original blocks.
