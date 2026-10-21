# Remote browser setup and preservation

The fork stores source, generated types/schema, freeze reproduction/evidence,
launcher tests, the remote helper and plugin, and portable configuration examples.
Do not commit binaries, credentials, complete user profiles, backups, or absolute
private repository inventory. Installed tools are reproducible from release tags.

## Remote host

Install Yazi 26.9.1 (includes ya), Lazygit 0.66.2, Delta 0.20.1, Zoxide 0.10.0,
fzf 0.74.5, Python 3, Git, tmux, and diff from trusted release/package sources.
The referenced versions are the original tested deployment, not mandatory pins
for future upstream builds. Verify release digests when downloading binaries.

Copy `scripts/remote-browser.py` to `~/.local/bin/wave-remote-browser` and chmod +x.
Copy `scripts/wave-cwd.yazi.lua` to
`~/.config/yazi/plugins/wave-cwd.yazi/main.lua`.
Merge `scripts/remote-browser-config/yazi-init.lua` into Yazi init.lua; merge the
Yazi config/keymap examples rather than overwriting existing settings.
Install plugins with `ya pkg add Lil-Dank/lazygit` (tested e73fd74) and
`ya pkg add yazi-rs/plugins:diff` (tested 9166202).
Copy remote-browser.sh to `~/.config/waveterm/remote-browser.sh`. Merge the
bashrc-wave-tmux.sh fragment into interactive ~/.bashrc, preserving other setup.
Do not nest a second wrapper if the existing one is already installed.
Merge Lazygit config.yml example into the user's config.

Configure launch defaults in remote-browser.sh/helper for your home/repositories;
the helper currently defaults to ~/sources and ~/sources/qatwin-ai. Adapt these
on a different deployment. Never change repo state just to make a launch work.

## Wave client

Merge widgets/connections/settings examples into client configuration. Replace
`user@host` with the canonical Wave connection name. Preserve shell controller,
durability, WAVE_TMUX and per-block tmux behavior. Restart newly opened Yazi to
load its tracking plugin. Disable automatic upstream updates for custom builds.

Build from this fork with the checkout's BUILD.md/Taskfile and current go.mod
requirements, not historical 0.14.5 toolchain assumptions. Use `task package --
--win zip --x64` on Windows x64. Test a side-by-side ZIP without running stock
and custom apps concurrently against one profile. Cold-backup config, data and
Electron profile, quit gracefully (not closing blocks), then enable inheritcwd.
See remote-browser-widgets.md and the historical Windows build results for
context and the remote wsh version-detection caveat. Existing remote managers
keep their original binary; replacing wsh is a separate authorized deployment.

## Lazygit recent repositories

Quit ALL Lazygit instances before modifying ~/.local/state/lazygit/state.yml
(or the legacy state location reported by that version). Back up it privately.
Preserve all other YAML keys and merge absolute repository/worktree roots under
recentrepos, removing duplicates. Discover repositories while pruning .git,
node_modules, virtualenv/cache/build directories and stopping descent at roots.
For requested repos use `git worktree list --porcelain` to include worktrees.
The original setup seeded 59 paths; the machine-specific list is private runtime
state and is deliberately not committed. Lazygit updates it when repos open.

## What to retain from Windows

Retain the existing source snapshot and package/logs privately as rollback/build
evidence until the rebased build is verified. Historical package hashes and
build observations are in remote-browser-windows-build-results.md. No separate
Windows source fixes were reported: only npm synchronized package-lock version.
That build-induced change need not be replayed over upstream's current lockfile.
Keep private config/data backups and settings, not in the public repository.
The portable widget/update settings are saved here. No installed app binaries,
59-path inventory, complete dotfiles, tokens, or backups belong in the fork.

Validation after rebase: widget tests, resolver tests, freeze regression tests,
production frontend build; Windows packaging/click/session continuity must be
repeated on the rebased source. Historical Windows BUILD PASSED was not a
cutover or end-to-end success.
