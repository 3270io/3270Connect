#!/usr/bin/env node
/* Record one demo scene to docs/assets/video/<id>.mp4.
 *
 *   node scripts/demos/record.mjs terminal-first-workflow
 *   node scripts/demos/record.mjs --all
 *   node scripts/demos/record.mjs console-tour --keep-frames
 *
 * A scene is a module in scenes/ that exports `meta` and `run(ctx)`. Terminal
 * scenes hand a cast and a caption list to ctx.playCast and are done; console
 * scenes drive a real 3270Connect running on localhost through ctx.page.
 *
 * Run scripts/demos/prepare.sh first — every scene executes the real binary.
 */
import { spawn } from 'node:child_process';
import fs from 'node:fs/promises';
import net from 'node:net';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { Capture, encode, launchStage, poster, serveDemos, STAGE_HEIGHT, STAGE_WIDTH }
  from './lib/recorder.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(HERE, '../..');
const BUILD = path.join(HERE, 'build');
const VIDEO_OUT = path.join(REPO, 'docs/assets/video');

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/* ------------------------------------------------------------------ scenes */

async function listScenes() {
  const files = await fs.readdir(path.join(HERE, 'scenes'));
  return files.filter((name) => name.endsWith('.mjs')).map((name) => name.replace(/\.mjs$/, ''));
}

async function loadScene(id) {
  const file = path.join(HERE, 'scenes', `${id}.mjs`);
  const scene = await import(pathToFileURL(file).href);
  if (typeof scene.run !== 'function') throw new Error(`${id}: no run() export`);
  return scene;
}

/* -------------------------------------------------------------- cast timing */

/* Cue times are written as marks, not seconds: "run.start", "run.done+1.5",
 * "end-2". A re-record shifts every mark and the captions follow. */
export function resolveTime(marks, spec) {
  if (typeof spec === 'number') return spec;
  const match = /^([\w.]+)\s*([+-]\s*[\d.]+)?$/.exec(String(spec).trim());
  if (!match) throw new Error(`unparseable cue time: ${spec}`);
  const [, name, offset] = match;
  if (!(name in marks)) {
    throw new Error(`unknown mark "${name}" (have: ${Object.keys(marks).sort().join(', ')})`);
  }
  return marks[name] + (offset ? Number(offset.replace(/\s+/g, '')) : 0);
}

async function readCast(name) {
  const castFile = path.join(HERE, 'casts', `${name}.cast`);
  const text = await fs.readFile(castFile, 'utf8');
  const lines = text.split('\n').filter(Boolean);
  const header = JSON.parse(lines[0]);
  const events = lines.slice(1).map((line) => {
    const [at, , data] = JSON.parse(line);
    return [at, data];
  });
  const sidecar = JSON.parse(
    await fs.readFile(path.join(HERE, 'casts', `${name}.marks.json`), 'utf8'));
  return { header, events, marks: sidecar.marks, duration: sidecar.duration };
}

/* ---------------------------------------------------------------- services */

/* Scenes that need a live 3270Connect start it here so the runner can be sure
 * it is torn down, including when the scene throws half way through. */
class Services {
  constructor() { this.running = []; }

  async start({ argv, cwd = BUILD, env = {}, port, wait = 1500 }) {
    const child = spawn(argv[0], argv.slice(1), {
      cwd,
      env: { ...process.env, ...env },
      stdio: 'ignore',
      detached: true,
    });
    this.running.push(child);
    if (port) await waitForPort(port, 20000);
    else await sleep(wait);
    return child;
  }

  stopAll() {
    for (const child of this.running) {
      try { process.kill(-child.pid, 'SIGTERM'); } catch { /* already gone */ }
    }
    this.running = [];
  }
}

function waitForPort(port, timeout) {
  const deadline = Date.now() + timeout;
  return new Promise((resolve, reject) => {
    const attempt = () => {
      const socket = net.connect({ host: '127.0.0.1', port }, () => {
        socket.end();
        resolve();
      });
      socket.on('error', () => {
        socket.destroy();
        if (Date.now() > deadline) reject(new Error(`port ${port} never opened`));
        else setTimeout(attempt, 250);
      });
    };
    attempt();
  });
}

/* ------------------------------------------------------------------ context */

function makeContext({ page, origin, services, scene }) {
  // Every stage method is available as ctx.stage.<name>(...), evaluated in the
  // page. Kept as a proxy so stage.js stays the single definition of the API.
  const stage = new Proxy({}, {
    get: (_, name) => (...args) =>
      page.evaluate(
        ([method, callArgs]) => window.stage[method](...callArgs),
        [name, args]),
  });

  return {
    page,
    origin,
    stage,
    services,
    sleep,
    build: BUILD,
    width: STAGE_WIDTH,
    height: STAGE_HEIGHT,

    /* Play a recorded terminal session with captions anchored to its marks. */
    async playCast(name, { cues = [], tail = 0.8, fontSize = 18, title } = {}) {
      const cast = await readCast(name);
      await stage.windowTitle(title ?? `bash — ${name}`);
      await stage.mountTerminal({
        cols: cast.header.width, rows: cast.header.height, fontSize,
      });
      await stage.showWindow(true);
      await sleep(700);
      const resolved = cues.map((cue) => ({
        ...cue,
        at: resolveTime(cast.marks, cue.at),
        until: cue.until == null ? null : resolveTime(cast.marks, cue.until),
      }));
      await page.evaluate(
        (payload) => window.stage.play(payload),
        { events: cast.events, cues: resolved, tail },
      );
    },

    /* Open the console inside the stage's window frame.
     *
     * `show: false` mounts it without revealing the window, so a scene can load
     * the console behind its title card. The console takes a few seconds to
     * come up, and those seconds are otherwise an empty stage. */
    async openConsole(url, { width = 1440, height = 760, title, show = true } = {}) {
      await stage.windowTitle(title ?? url.replace(/^https?:\/\//, ''));
      await stage.mountWeb({ url, width, height });
      if (show) {
        await stage.showWindow(true);
        await sleep(1200);
      }
    },

    /* Everything below addresses the console page, which lives in an iframe.
     * Playwright reaches into it directly; the stage only draws the pointer so
     * the video shows where the click landed. */
    frame() {
      return page.frameLocator('#web');
    },

    /* The Frame itself, for evaluating in the console's own document —
     * scrolling, mostly, which reads far better smooth than jumped. */
    consoleFrame() {
      const frame = page.frames().find((candidate) => candidate.name() === 'web'
        || candidate.url().includes('/dashboard'));
      if (!frame) throw new Error('the console frame is not attached');
      return frame;
    },

    async scrollTo(selector, { offset = -40, settle = 1100 } = {}) {
      const frame = this.consoleFrame();
      await frame.evaluate(([target, delta]) => {
        const node = document.querySelector(target);
        const top = node ? node.getBoundingClientRect().top + window.scrollY + delta : 0;
        window.scrollTo({ top: Math.max(0, top), behavior: 'smooth' });
      }, [selector, offset]);
      await sleep(settle);
    },

    /* A run started for the video needs somewhere to keep its metrics that is
     * not the operator's real state directory — otherwise the process table
     * fills with whatever else has run on this machine. */
    /* Run a command to completion as part of a scene's setup — seeding an
     * account, writing a file. Nothing it prints reaches the video. */
    run(command, args, { cwd = BUILD, env = {}, stdin = null } = {}) {
      return new Promise((resolve, reject) => {
        const child = spawn(command, args, {
          cwd,
          env: { ...process.env, ...env },
          stdio: [stdin == null ? 'ignore' : 'pipe', 'pipe', 'pipe'],
        });
        let stderr = '';
        child.stderr.on('data', (chunk) => { stderr += chunk; });
        child.stdout.resume();
        if (stdin != null) child.stdin.end(stdin);
        child.on('error', reject);
        child.on('close', (code) => (code === 0
          ? resolve()
          : reject(new Error(`${command} ${args.join(' ')} exited ${code}\n${stderr.trim()}`))));
      });
    },

    /* Prefer /data, which is where the container image keeps its state and so
     * the path an operator watching this actually has. The administration
     * pages print the state directory on screen; recorded from the build tree
     * it reads as whatever the recording machine's home directory was called,
     * which is both noise and somebody's username in a published video. */
    async freshState(name = scene.meta.id) {
      for (const dir of [path.join('/data/demo', name), path.join(BUILD, 'state', name)]) {
        try {
          await fs.rm(dir, { recursive: true, force: true });
          await fs.mkdir(dir, { recursive: true });
          return dir;
        } catch {
          // /data is not writable here; fall back to the disposable build tree.
        }
      }
      throw new Error('no writable state directory for the recording');
    },

    async pointAt(locator, { settle = 320 } = {}) {
      const box = await locator.boundingBox();
      if (!box) throw new Error('cannot point at an element with no box');
      const origin = await page.evaluate(() => window.stage.webOrigin());
      const x = origin.x + box.x + box.width / 2;
      const y = origin.y + box.y + box.height / 2;
      await stage.pointer(x, y);
      await sleep(settle);
      return { x, y, box, origin };
    },

    async clickThrough(locator, { settle = 320 } = {}) {
      const { x, y } = await this.pointAt(locator, { settle });
      await stage.click(x, y);
      await locator.click();
      return { x, y };
    },

    async spotlight(locator, { pad = 12 } = {}) {
      if (!locator) return stage.spotlight(null);
      const box = await locator.boundingBox();
      const origin = await page.evaluate(() => window.stage.webOrigin());
      return stage.spotlight(
        { x: origin.x + box.x, y: origin.y + box.y, width: box.width, height: box.height },
        { pad });
    },

    /* Show a caption for a fixed beat. Console scenes are driven from here, so
     * they say how long a line stays up rather than anchoring to a clock. */
    async say(text, sub, hold = 3.2) {
      await stage.caption(text, sub ?? '');
      await sleep(hold * 1000);
    },

    /* `during` runs while the card is up and the card stays until both it and
     * the hold have finished — the place to put slow setup a viewer should
     * never watch, like waiting for the console's first paint. */
    async titleCard(hold = 3.0, during = null) {
      await stage.card({
        kicker: scene.meta.kicker ?? '',
        title: scene.meta.title,
        sub: scene.meta.subtitle ?? '',
      });
      await Promise.all([sleep(hold * 1000), during ? during() : null]);
      await stage.hideCard();
    },

    async endCard(hold = 2.8) {
      await stage.clearCaption();
      await stage.spotlight(null);
      await sleep(400);
      await stage.card({
        kicker: 'Read the docs',
        title: '3270connect.3270.io',
        sub: scene.meta.endNote ?? '',
      });
      await sleep(hold * 1000);
    },
  };
}

/* -------------------------------------------------------------------- main */

async function record(id, { keepFrames = false } = {}) {
  const scene = await loadScene(id);
  const outFile = path.join(VIDEO_OUT, `${id}.mp4`);
  const frameDir = path.join(HERE, 'build/frames', id);
  await fs.rm(frameDir, { recursive: true, force: true });

  const { server, origin } = await serveDemos(HERE);
  const services = new Services();
  let browser;
  try {
    const launched = await launchStage(origin);
    browser = launched.browser;
    const { page } = launched;
    const ctx = makeContext({ page, origin, services, scene });

    const capture = new Capture(page, frameDir);
    await capture.start();
    await sleep(400); // a beat of stage before anything moves

    await scene.run(ctx);

    await sleep(500);
    const frames = await capture.stop();
    const result = await encode(frames, outFile, { crf: scene.meta.crf ?? 21 });

    if (scene.meta.poster !== false) {
      await poster(outFile, path.join(VIDEO_OUT, `${id}.jpg`), scene.meta.poster ?? 4);
    }

    console.log(
      `${path.relative(REPO, outFile)} — ${result.seconds.toFixed(1)}s, ` +
      `${(result.size / 1024 / 1024).toFixed(2)} MB, ${result.frames} frames`);
  } finally {
    services.stopAll();
    if (browser) await browser.close();
    server.close();
    if (!keepFrames) await fs.rm(frameDir, { recursive: true, force: true });
  }
}

async function main() {
  const args = process.argv.slice(2);
  const keepFrames = args.includes('--keep-frames');
  const ids = args.filter((arg) => !arg.startsWith('--'));

  const targets = args.includes('--all') || ids.length === 0 ? await listScenes() : ids;
  if (!targets.length) {
    console.error('no scenes found');
    process.exit(1);
  }

  try {
    await fs.access(path.join(BUILD, '3270Connect'));
  } catch {
    console.error('scripts/demos/build/3270Connect is missing — run scripts/demos/prepare.sh');
    process.exit(1);
  }

  for (const id of targets) {
    console.log(`recording ${id}…`);
    await record(id, { keepFrames });
  }
}

await main();
