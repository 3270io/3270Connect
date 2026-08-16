#!/usr/bin/env python3
"""Record a scripted terminal session to an asciinema v2 cast file.

The videos in the documentation are replays, not live screen grabs: this script
runs each command for real in a pty, keeps the bytes it wrote and the moment it
wrote them, and wraps that with a synthesised prompt and keystroke animation so
the replay looks like somebody typing. Nothing is faked except the typing — the
output is whatever the command actually printed.

Synthesising the prompt is what lets this work without a shell. A real bash in a
pty would mean prompt detection, which means parsing the escape sequences we are
trying to record. Running each command directly in its own pty sidesteps that,
and it makes a cast reproducible: the same script always produces the same
sequence of commands, and only the timings move.

Usage:
    python3 ptycast.py <script.json> <out.cast>

Script format:
    {
      "cols": 108, "rows": 30,
      "cwd": "…",                 # optional, relative paths resolve against it
      "env": {"KEY": "value"},    # optional, added to the child environment
      "steps": [
        {"cmd": "3270Connect -config first-run.json",
         "argv": ["./3270Connect", "-config", "first-run.json"],
         "wait_before": 0.6,      # pause on the prompt before typing
         "wait_after": 1.5,       # hold the finished screen
         "timeout": 90,           # SIGTERM the command after this long
         "type_speed": 0.045},    # mean seconds per keystroke
        {"comment": "# a line that is typed but never run"}
      ]
    }

`cmd` is the text that gets typed on screen and `argv` is what actually runs, so
a demo can show `3270Connect …` while executing `./3270Connect …` from a build
directory. Omit `argv` and the command is not run at all, which is how comment
lines and deliberate typos are staged.

Options:
    --max-gap SECONDS   collapse any idle stretch longer than this (default 1.2)
    --speed FACTOR      divide every timestamp by FACTOR (default 1.0)
"""
from __future__ import annotations

import argparse
import errno
import json
import os
import pty
import random
import select
import shutil
import signal
import subprocess
import sys
import time

# The prompt is drawn, not read from a shell, so it stays identical across
# machines and does not leak the recording box's hostname or paths.
PROMPT = "\x1b[1;92m3270\x1b[0m \x1b[36m▸\x1b[0m "


class Cast:
    """Accumulates (timestamp, output) events on a synthetic clock.

    The clock is advanced explicitly rather than read from `time.time()` for the
    parts we invent — typing, pauses — and set from the wall clock only while a
    command is running. That keeps typing speed independent of how loaded the
    recording machine is, while real command output keeps its real timing.
    """

    def __init__(self, cols: int, rows: int):
        self.cols = cols
        self.rows = rows
        self.events: list[tuple[float, str, bool]] = []
        self.marks: list[tuple[float, str]] = []
        self.resolved_marks: dict[str, float] = {}
        self.t = 0.0
        # Only silence *inside* a command's output is dead air worth trimming.
        # The pauses this script inserts on purpose — the beat before typing,
        # the hold on a finished screen — are the pacing of the video, and
        # collapsing those would undo the thing they were added for.
        self.trimmable = False

    def write(self, data: str) -> None:
        if data:
            self.events.append((round(self.t, 6), data, self.trimmable))

    def sleep(self, seconds: float) -> None:
        self.t += max(0.0, seconds)

    def type(self, text: str, speed: float) -> None:
        """Emit `text` one character at a time with human-ish jitter."""
        for ch in text:
            self.write(ch)
            # Spaces get a touch longer to suggest word boundaries; everything
            # else lands within ±40% of the mean so the rhythm is not metronomic.
            jitter = random.uniform(0.6, 1.4) * (1.6 if ch == " " else 1.0)
            self.sleep(speed * jitter)

    def mark(self, name: str) -> None:
        """Record a named point on the timeline.

        Captions are written against these rather than against raw seconds. A
        re-record moves every timestamp — a slower host, a longer workflow — but
        the marks move with it, so the overlay text stays in sync without anyone
        editing the scene.
        """
        self.marks.append((round(self.t, 6), name))

    def dump(self, path: str, max_gap: float, speed: float) -> None:
        """Write the cast, collapsing dead air and applying the speed factor.

        Marks are rewritten through the same collapse so they keep pointing at
        the frame they were placed on.
        """
        out: list[tuple[float, str]] = []
        marks: dict[str, float] = {}
        # Walk events and marks together on one clock so a gap collapsed
        # between two events also shifts any mark that fell inside it.
        timeline = sorted(
            [(t, "event", d, trim) for t, d, trim in self.events]
            + [(t, "mark", n, False) for t, n in self.marks],
            key=lambda item: item[0],
        )
        shift = 0.0
        previous = 0.0
        for stamp, kind, payload, trimmable in timeline:
            gap = stamp - previous
            if trimmable and gap > max_gap:
                shift += gap - max_gap
            previous = stamp
            when = (stamp - shift) / speed
            if kind == "event":
                out.append((when, payload))
            else:
                marks[payload] = round(when, 6)
        self.resolved_marks = marks
        self.trimmed_duration = out[-1][0] if out else 0.0


        header = {
            "version": 2,
            "width": self.cols,
            "height": self.rows,
            "env": {"TERM": "xterm-256color", "SHELL": "/bin/bash"},
        }
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(json.dumps(header) + "\n")
            for stamp, data in out:
                handle.write(json.dumps([round(stamp, 6), "o", data]) + "\n")

        with open(marks_path(path), "w", encoding="utf-8") as handle:
            json.dump({
                "duration": round(self.duration, 6),
                "marks": marks,
            }, handle, indent=2, sort_keys=True)
            handle.write("\n")

    @property
    def duration(self) -> float:
        """Length of the replay, which is the final hold, not the final byte."""
        return max([self.trimmed_duration] + list(self.resolved_marks.values()))


def marks_path(cast_path: str) -> str:
    """The sidecar that scenes anchor their captions to."""
    stem = cast_path[:-5] if cast_path.endswith(".cast") else cast_path
    return stem + ".marks.json"


def run_in_pty(cast: Cast, argv: list[str], cwd: str, env: dict[str, str],
               timeout: float, background: float = 0.0):
    """Run `argv` on a pty, appending everything it prints to `cast`.

    With `background` set, recording stops after that many seconds and the
    process is left running and handed back for the caller to tear down. That
    is how a step written as `3270Connect -api … &` behaves on screen: the
    banner prints, the prompt comes back, and the server is still there for the
    curl in the next step to talk to.

    Returns (exit_code, process). The code is None while a backgrounded process
    is still running.
    """
    primary, secondary = pty.openpty()
    # The child is told the exact geometry the replay will use, so line wrapping
    # in the recording matches line wrapping in the video.
    child_env = dict(os.environ)
    child_env.update(env)
    child_env.update({
        "TERM": "xterm-256color",
        "COLUMNS": str(cast.cols),
        "LINES": str(cast.rows),
        # Force colour: the programs being recorded see a pty, but the libraries
        # they use differ on whether that is enough.
        "FORCE_COLOR": "1",
        "CLICOLOR_FORCE": "1",
    })
    try:
        import fcntl
        import struct
        import termios
        fcntl.ioctl(secondary, termios.TIOCSWINSZ,
                    struct.pack("HHHH", cast.rows, cast.cols, 0, 0))
    except Exception:  # pragma: no cover - geometry is a nicety, not a failure
        pass

    proc = subprocess.Popen(
        argv, cwd=cwd, env=child_env,
        stdin=secondary, stdout=secondary, stderr=secondary,
        preexec_fn=os.setsid,
    )
    os.close(secondary)

    started = time.time()
    base = cast.t
    terminated = False
    cast.trimmable = True
    detached = False
    while True:
        elapsed = time.time() - started
        if background and elapsed > background:
            detached = True
            break
        if not terminated and elapsed > timeout:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
            terminated = True
        ready, _, _ = select.select([primary], [], [], 0.1)
        if ready:
            try:
                chunk = os.read(primary, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:  # the pty closed with the child
                    break
                raise
            if not chunk:
                break
            # Real output keeps real timing: the synthetic clock is pinned to
            # the wall clock for the duration of the command.
            cast.t = base + (time.time() - started)
            cast.write(chunk.decode("utf-8", "replace"))
        elif proc.poll() is not None:
            break
        if terminated and elapsed > timeout + 10:
            os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
            break

    cast.t = base + (time.time() - started)
    cast.trimmable = False
    if detached:
        # The pty is left open on purpose: closing it would send the process a
        # SIGHUP, and it has to survive to serve the rest of the script.
        return None, proc
    os.close(primary)
    return proc.wait(), proc


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("script")
    parser.add_argument("out")
    parser.add_argument("--max-gap", type=float, default=1.2)
    parser.add_argument("--speed", type=float, default=1.0)
    args = parser.parse_args()

    with open(args.script, encoding="utf-8") as handle:
        spec = json.load(handle)

    # Typing jitter is the only randomness here, and a fixed seed keeps a
    # re-record byte-identical when nothing else changed.
    random.seed(spec.get("seed", 20230217))

    cast = Cast(spec.get("cols", 108), spec.get("rows", 30))
    # Relative to the script file, not to wherever this was invoked from, so a
    # cast records the same session from any directory.
    cwd = os.path.abspath(os.path.join(
        os.path.dirname(os.path.abspath(args.script)), spec.get("cwd", ".")))
    # `${cwd}` in an env value expands to the working directory. Recordings use
    # it to point XDG_CONFIG_HOME at a throwaway folder: the real one is shared
    # by every 3270Connect on the machine, and a half-written metrics file from
    # some earlier run shows up in the next one's output as a warning.
    env = {str(k): str(v).replace("${cwd}", cwd) for k, v in spec.get("env", {}).items()}

    # A cast records what a first run looks like, so anything a previous take
    # left in the working directory has to go — otherwise `logs/` fills up and
    # the recording quietly stops matching the story it is telling.
    for stale in spec.get("clean", []):
        target = os.path.join(cwd, stale)
        if os.path.isdir(target):
            shutil.rmtree(target, ignore_errors=True)
        elif os.path.exists(target):
            os.remove(target)

    services = start_services(spec.get("services", []), cwd, env)
    try:
        # Anything a step backgrounded is torn down alongside the services —
        # a demo that leaves an API server listening would poison the next take.
        record_steps(cast, spec["steps"], cwd, env, services)
    finally:
        stop_services(services)

    cast.dump(args.out, args.max_gap, args.speed)
    print(f"{args.out} — {cast.duration:.1f}s, "
          f"{len(cast.events)} events, {cast.cols}x{cast.rows}")
    return 0


def start_services(specs: list[dict], cwd: str, env: dict[str, str]) -> list:
    """Launch the background processes a demo talks to (host, dashboard, API).

    They are started outside the cast so their own banners never reach the
    recording — the video shows the client side of the conversation only.
    """
    running = []
    for service in specs:
        child_env = dict(os.environ)
        child_env.update(env)
        child_env.update({str(k): str(v) for k, v in service.get("env", {}).items()})
        proc = subprocess.Popen(
            service["argv"], cwd=cwd, env=child_env,
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            preexec_fn=os.setsid,
        )
        running.append(proc)
        time.sleep(service.get("wait", 2.0))
    return running


def stop_services(running: list) -> None:
    for proc in running:
        try:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
        except ProcessLookupError:
            pass
    for proc in running:
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:  # pragma: no cover
            os.killpg(os.getpgid(proc.pid), signal.SIGKILL)


def record_steps(cast: Cast, steps: list[dict], cwd: str,
                 env: dict[str, str], started: list) -> None:
    for index, step in enumerate(steps, start=1):
        # Every step publishes four marks. `id` names them when a scene reads
        # better for it — "run.start" says more than "s2.start" in a caption
        # file that somebody has to maintain a year from now.
        name = step.get("id", f"s{index}")
        cast.mark(f"{name}.prompt")
        cast.write(PROMPT)
        cast.sleep(step.get("wait_before", 0.5))
        typed = step.get("cmd", step.get("comment", ""))
        cast.type(typed, step.get("type_speed", 0.045))
        cast.mark(f"{name}.typed")
        cast.sleep(step.get("wait_enter", 0.35))
        cast.write("\r\n")

        cast.mark(f"{name}.start")
        argv = step.get("argv")
        if argv:
            code, proc = run_in_pty(cast, argv, cwd, env, step.get("timeout", 120),
                                    background=step.get("background", 0.0))
            if code is None:
                started.append(proc)
            else:
                expected = step.get("expect_exit")
                if expected is not None and code != expected:
                    print(f"! {typed!r} exited {code}, expected {expected}",
                          file=sys.stderr)
                elif expected is None and code not in (0, 143):
                    print(f"! {typed!r} exited {code}", file=sys.stderr)
        cast.mark(f"{name}.done")
        cast.sleep(step.get("wait_after", 1.2))

    cast.mark("end")


if __name__ == "__main__":
    sys.exit(main())
