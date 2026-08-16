# Demonstration recordings

Everything under `docs/assets/video/` is produced from here. Nothing is edited
by hand afterwards, and nothing on screen is mocked: each recording runs the
real binary against the bundled sample 3270 application, and the numbers in the
reports and charts are that run's.

Re-record whenever the thing being shown changes. A video of the console from
two releases ago is worse than no video, because a reader who follows it and
sees something else assumes they have got it wrong.

## Recording one

```bash
scripts/demos/prepare.sh                     # build, and stage a working directory
cd scripts/demos && npm install              # first time only

node record.mjs terminal-first-workflow      # one scene
node record.mjs --all                        # all of them, in series
node record.mjs console-tour --keep-frames   # leave the JPEG frames for inspection
```

Each run writes `docs/assets/video/<scene>.mp4` and a `<scene>.jpg` poster
frame, both of which are committed.

### What you need

| | |
|---|---|
| Go | to build the binary the recordings drive |
| Node 20+ | `npm install` here pulls Playwright and xterm.js |
| Chromium | Playwright's own, or set `CHROMIUM_PATH` to one already on the box |
| ffmpeg | with `libx264` — the frames are encoded to h264 |
| Fonts | `fonts-inter` and `fonts-jetbrains-mono`, or the videos fall back to DejaVu |

## How a scene is put together

The recorder opens one page — the **stage** in `lib/stage/` — captures it with
CDP's screencast, and encodes the timestamped frames to mp4. The stage is a
fixed 1920×1080 canvas holding a window frame, a caption bar, a chapter chip
and the title and end cards. Both kinds of video use the same one, which is why
a terminal recording and a console recording look like they came from the same
place.

What goes inside the window frame is the difference between the two kinds:

**Terminal scenes** replay a recorded session into xterm.js. The session is
captured separately by `lib/ptycast.py`, which runs each command for real on a
pty and keeps what it printed and when. The typing is synthesised — everything
after the newline is the command's own output.

```bash
python3 lib/ptycast.py terminal/first-workflow.json casts/first-workflow.cast
```

That writes the cast and a `.marks.json` sidecar naming each step's boundaries
(`run.prompt`, `run.typed`, `run.start`, `run.done`, and `end`). Captions are
written against those marks rather than against seconds:

```js
{ at: 'run.done-0.2', until: 'artifacts.prompt-0.4',
  text: 'Every run ends with a report' }
```

Re-recording the cast on a slower machine moves every timestamp; because the
marks move with them, the captions stay where they were meant to be and the
scene file does not need touching.

**Console scenes** put the console itself in the frame and drive it with
Playwright — uploading a workflow, filling the load profile, clicking through
to the admin pages. They start their own `3270Connect` processes and are
therefore as slow as the run they are showing, which is the point.

## The files

```
record.mjs            the CLI, and the context a scene is handed
lib/recorder.mjs      browser, screencast capture, ffmpeg encode
lib/stage/            the stage page — markup, styles, and its runtime API
lib/ptycast.py        records a scripted terminal session to an asciinema cast
scenes/*.mjs          one per video: title, captions, and what to do
terminal/*.json       the command scripts the casts are recorded from
casts/*.cast          recorded terminal sessions, committed
workflows/*.json      the workflow files the recordings run
build/                staged binary and scratch space — disposable, untracked
```

## Notes on things that were not obvious

- **Terminal geometry decides the font size.** The stage grows the type until
  the terminal hits an edge, so fewer rows means bigger text. Casts are 24 rows
  and 100–120 columns, which lands around 20px — legible in a docs page without
  going full screen. A cast with more rows will be recorded correctly and be
  hard to read.
- **Recordings need their own state directory.** `3270Connect` keeps metrics
  under `os.UserConfigDir()`, shared by every copy on the machine, and a
  half-written file from an earlier run appears in the next one's output as a
  warning. Scenes call `ctx.freshState()`; cast scripts set `XDG_CONFIG_HOME`
  in their `env` block. That path is also on screen at the bottom of the audit
  page, which is why it prefers `/data`.
- **The console refuses to be framed**, twice — `X-Frame-Options` and a
  `frame-ancestors` directive on the auth pages. The recorder drops both from
  document responses only. Leave the rest of the policy alone, and leave other
  request types alone: replaying a multipart upload through the interceptor
  corrupts it, and starting a run from the console then fails.
- **`-headless` is what the terminal videos use.** Without it the emulator is
  the x3270 GUI, which needs an X display; with it the terminal UI still prints
  everything, which is what is being shown.
