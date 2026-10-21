# Source from interactive ~/.bashrc, after normal shell setup.
# Must run before remote-browser.sh so the tool starts in the inner tmux shell.
if [ "${WAVE_TMUX:-}" = "1" ] && [ -n "${WAVETERM_JOBID:-}" ] && [ -z "${TMUX:-}" ] \
   && command -v tmux >/dev/null 2>&1; then
    _wave_tmux="wave-${WAVETERM_BLOCKID:-$WAVETERM_JOBID}"
    _wave_tmux="${_wave_tmux:0:13}"
    exec tmux -L "$_wave_tmux" new-session -A -D -s "$_wave_tmux" \; set -s exit-unattached on
fi
[ -r "$HOME/.config/waveterm/remote-browser.sh" ] && source "$HOME/.config/waveterm/remote-browser.sh"
