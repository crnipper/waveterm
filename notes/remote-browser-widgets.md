# Selected-directory remote browser widgets

The checkout is a local branch of upstream Wave (`durable-never-block`), not a
configured GitHub fork. Prior durable-session fix commits remain intact.

This change adds opt-in `inheritcwd` to widget definitions. Clicking a widget
captures the selected terminal before the new block takes focus. Only matching
connections inherit context. `wsh launch` keeps configured defaults.

Remote deployment on crnipper@vm:
- `scripts/remote-browser.py` is installed as `~/.local/bin/wave-remote-browser`.
- `scripts/wave-cwd.yazi.lua` is installed as
  `~/.config/yazi/plugins/wave-cwd.yazi/main.lua`; require `wave-cwd`:setup()
  from `~/.config/yazi/init.lua`. Restart Yazi to load it.
- The existing `~/.config/waveterm/remote-browser.sh` calls the helper after the
  unchanged per-block tmux wrapper. It keeps default locations without source
  context; non-repository paths never initialize Git repos.

After installing a Wave build containing BOTH the generated Go widget config
and the frontend change, add `"inheritcwd": true` to `remote-yazi` and
`remote-lazygit` in the Windows `~/.config/waveterm/widgets.json`. Keep the existing
remote connection, shell controller, `term:durable` and `cmd:env` settings.
Do not enable it on the stock app and claim the behavior is live.

The remote helper queries `wave-<first 8 source block ID chars>` (the existing
wrapper's socket convention), with Wave cwd metadata as fallback. Active Yazi
tracks the browsed directory in a tmux pane option; stale options are ignored
when Yazi has exited. No last-used-shell heuristics or reconnect hooks are used.

Validation:
- `npm test -- --run frontend/app/workspace/widgetlaunch.test.ts frontend/app/workspace/widgetfilter.test.ts`
- `node_modules/.bin/tsc --noEmit`
- `python3 scripts/test_remote_browser.py`
- `npm run build:prod`
- End-to-end click testing requires the modified Windows app; a frontend build
  alone does not replace the stock installed backend.
