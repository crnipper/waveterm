#!/bin/bash
# Reproduce the durable-session freeze against one or more wsh builds.
#
#   cmd/test-jobfreeze/repro.sh <workdir> <label>=<wsh-binary> [<label>=<wsh-binary> ...]
#
# Everything (HOME, job sockets, logs, tmux socket) lives under <workdir>/runs/<label>-<scenario>;
# nothing touches ~/.waveterm, /tmp/waveterm-<uid>, or any running Wave/tmux process.
# Needs: go (to build the harness), node (probe), tmux (tmux scenario).
set -u
WORK=$(realpath -m "$1"); shift
REPO=$(cd "$(dirname "$0")/../.." && pwd)
mkdir -p "$WORK/bin" "$WORK/runs" "$WORK/home-harness"
(cd "$REPO" && go build -o "$WORK/bin/test-jobfreeze" ./cmd/test-jobfreeze) || exit 1

DRIP="-pertick 1 -tickms 1 -pad 20"   # tiny writes => tiny packets (like a TUI), fills the socket buffer
SCENARIOS=(
  "blackhole|-fault blackhole"
  "stopread-drip|-fault stopread $DRIP"
  "hiccup-drip|-fault hiccup -hiccup 7 $DRIP"
  "close|-fault close"
  "tmux-blackhole|-tmux -fault blackhole"
)
SUMMARY="$WORK/summary.txt"
: > "$SUMMARY"
for spec in "$@"; do
  label=${spec%%=*}; wsh=$(realpath "${spec#*=}")
  for sc in "${SCENARIOS[@]}"; do
    name=${sc%%|*}; flags=${sc#*|}
    out="$WORK/runs/$label-$name.out"
    rm -rf "$WORK/runs/$label-$name"
    HOME="$WORK/home-harness" timeout 120 "$WORK/bin/test-jobfreeze" -wsh "$wsh" -dir "$WORK/runs/$label-$name" \
      $flags -pre 3 -faultsecs 18 -post 4 > "$out" 2>&1
    frozen=$(grep -c "fault .*FROZEN" "$out")
    lastfault=$(grep "\] fault " "$out" | tail -1 | grep -o "probe [A-Z]*" | head -1)
    reatt=$(grep "\] reattach " "$out" | tail -1 | grep -o "probe [A-Z]*" | head -1)
    events=$(cat "$WORK"/runs/$label-$name/home/.waveterm/jobs/*.log 2>/dev/null \
      | grep -o "SendData: .*timeout sending request\|no longer running\|no ACK progress for\|send failed at\|client connection closed" \
      | sed 's/SendData: .*timeout/SendData timeout/' | sort | uniq -c | tr -s ' ' | tr '\n' ';')
    printf "%-7s %-15s frozen_samples=%2d/18  end_of_fault=%-14s after_reattach=%-14s joblog:%s\n" \
      "$label" "$name" "$frozen" "$lastfault" "$reatt" "$events" | tee -a "$SUMMARY"
  done
done
