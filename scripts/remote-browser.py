#!/usr/bin/env python3
"""Resolve a selected Wave block's live remote directory and start its browser."""
import argparse
import os
from pathlib import Path
import re
import subprocess


def capture(*args):
    try:
        result = subprocess.run(args, text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, timeout=3)
    except (OSError, subprocess.TimeoutExpired):
        return ""
    return result.stdout.strip() if result.returncode == 0 else ""


def resolve_directory(block, fallback=""):
    if re.fullmatch(r"[0-9a-fA-F-]{36}", block or ""):
        socket = "wave-" + block[:8]
        pane = capture("tmux", "-L", socket, "display-message", "-p",
                       "#{pane_pid}\t#{pane_current_path}\t#{@wave-yazi-cwd}")
        fields = pane.split("\t")
        if len(fields) >= 2:
            pid, cwd = fields[:2]
            yazi_cwd = fields[2] if len(fields) > 2 else ""
            # Only trust Yazi's tracked directory while Yazi is actually running.
            if yazi_cwd and capture("pgrep", "-P", pid, "-x", "yazi") and Path(yazi_cwd).is_dir():
                return yazi_cwd
            if Path(cwd).is_dir():
                return cwd
    return fallback if fallback and Path(fallback).is_dir() else ""


def repository_root(directory):
    return capture("git", "-C", directory, "rev-parse", "--show-toplevel")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tool", choices=["yazi", "lazygit"])
    parser.add_argument("--resolve-only", action="store_true")
    args = parser.parse_args()
    block = os.environ.pop("WAVE_BROWSER_SOURCE_BLOCK", "")
    fallback = os.environ.pop("WAVE_BROWSER_SOURCE_CWD", "")
    os.environ.pop("WAVE_BROWSER_STARTUP", None)
    directory = resolve_directory(block, fallback)
    if not directory:
        directory = str(Path.home() / "sources" / ("qatwin-ai" if args.tool == "lazygit" else ""))
    if args.resolve_only:
        print(directory)
        return
    if args.tool == "lazygit":
        root = repository_root(directory)
        if not root:
            print(f"No Git repository at {directory}. Use Yazi to select one; nothing was initialized.")
            return
        directory = root
    os.chdir(directory)
    os.execvp(args.tool, [args.tool])


if __name__ == "__main__":
    main()
