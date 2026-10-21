# Remote Wave browser setup; sourced after the existing per-block tmux wrapper.
export PATH="$HOME/.local/bin:$PATH"
if command -v zoxide >/dev/null 2>&1; then
    eval "$(zoxide init bash)"
fi
if [ -n "${WAVETERM_BLOCKID:-}" ]; then
    export GIT_PAGER="$HOME/.local/bin/delta"
fi
# The launcher flag is consumed only in the inner tmux shell. Keep a normal
# shell when the browser exits, and do not relaunch in subsequent child shells.
if [ -n "${WAVE_BROWSER_STARTUP:-}" ] && [ -n "${TMUX:-}" ]; then
    _wave_browser="$WAVE_BROWSER_STARTUP"
    unset WAVE_BROWSER_STARTUP
    case "$_wave_browser" in
        yazi) wave-remote-browser yazi ;;
        lazygit) wave-remote-browser lazygit ;;
    esac
    unset _wave_browser
fi
