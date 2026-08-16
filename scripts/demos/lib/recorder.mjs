/* Capture the stage page to an mp4.
 *
 * Frames come from CDP's screencast rather than Playwright's built-in video
 * recorder. Two reasons: the frames arrive as lossless-enough JPEG at full
 * resolution instead of going through a VP8 encode first, and each one carries
 * a timestamp, so a scene that pauses for four seconds produces four seconds of
 * video rather than however many frames the encoder felt like emitting. The
 * timestamps are fed to ffmpeg's concat demuxer, which is what keeps a caption
 * on screen for exactly as long as the scene said.
 */
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import fs from 'node:fs/promises';
import http from 'node:http';
import process from 'node:process';
import path from 'node:path';

export const STAGE_WIDTH = 1920;
export const STAGE_HEIGHT = 1080;

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.cast': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
};

/* The stage, the casts and node_modules all have to be same-origin for the
 * page to fetch them, so one server is rooted at scripts/demos and everything
 * is addressed from there. */
export async function serveDemos(root) {
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    const target = path.join(root, path.normalize(decodeURIComponent(url.pathname)));
    if (!target.startsWith(root)) {
      res.writeHead(403).end('no');
      return;
    }
    try {
      const body = await fs.readFile(target);
      res.writeHead(200, { 'content-type': MIME[path.extname(target)] ?? 'application/octet-stream' });
      res.end(body);
    } catch {
      res.writeHead(404).end('not found');
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  return { server, origin: `http://127.0.0.1:${port}` };
}

export class Capture {
  constructor(page, dir) {
    this.page = page;
    this.dir = dir;
    this.frames = [];
    this.writes = [];
    this.client = null;
    this.index = 0;
  }

  async start() {
    await fs.mkdir(this.dir, { recursive: true });
    this.client = await this.page.context().newCDPSession(this.page);
    this.client.on('Page.screencastFrame', ({ data, sessionId, metadata }) => {
      // Ack first: Chromium will not send another frame until the current one
      // is acknowledged, and awaiting the disk write before acking would drop
      // the capture rate to whatever the filesystem manages.
      this.client.send('Page.screencastFrameAck', { sessionId }).catch(() => {});
      const file = path.join(this.dir, `${String(this.index++).padStart(6, '0')}.jpg`);
      this.frames.push({ at: metadata.timestamp, file });
      this.writes.push(fs.writeFile(file, Buffer.from(data, 'base64')));
    });
    await this.client.send('Page.startScreencast', {
      format: 'jpeg',
      quality: 92,
      maxWidth: STAGE_WIDTH,
      maxHeight: STAGE_HEIGHT,
      everyNthFrame: 1,
    });
  }

  async stop() {
    await this.client.send('Page.stopScreencast');
    await Promise.all(this.writes);
    return this.frames;
  }
}

/* Turn timestamped frames into an mp4.
 *
 * The concat demuxer wants a duration after each file; the last frame gets one
 * too and is then repeated, because concat ignores the duration of a final
 * entry that is not followed by anything. */
export async function encode(frames, out, { fps = 30, crf = 21, tail = 0.6 } = {}) {
  if (frames.length < 2) throw new Error(`only ${frames.length} frames captured`);

  const lines = [];
  for (let i = 0; i < frames.length; i += 1) {
    const next = frames[i + 1]?.at ?? frames[i].at + tail;
    const duration = Math.max(1 / 240, next - frames[i].at);
    lines.push(`file '${path.basename(frames[i].file)}'`);
    lines.push(`duration ${duration.toFixed(6)}`);
  }
  lines.push(`file '${path.basename(frames[frames.length - 1].file)}'`);

  const listing = path.join(path.dirname(frames[0].file), 'frames.txt');
  await fs.writeFile(listing, lines.join('\n') + '\n');
  await fs.mkdir(path.dirname(out), { recursive: true });

  await run('ffmpeg', [
    '-y', '-hide_banner', '-loglevel', 'error',
    '-f', 'concat', '-safe', '0', '-i', listing,
    // fps here resamples the variable-rate capture onto a constant grid, which
    // is what browsers and GitHub Pages want; format keeps it playable in
    // Safari, which refuses anything but 4:2:0.
    '-vf', `fps=${fps},format=yuv420p`,
    '-c:v', 'libx264', '-preset', 'slow', '-crf', String(crf),
    '-profile:v', 'high', '-level', '4.0',
    '-movflags', '+faststart',
    '-an',
    out,
  ]);

  const { size } = await fs.stat(out);
  const seconds = frames[frames.length - 1].at - frames[0].at;
  return { size, seconds, frames: frames.length };
}

/* A still for the video's poster attribute, pulled from the finished mp4 so it
 * is guaranteed to match a frame the viewer will actually see. */
export async function poster(video, out, at) {
  await run('ffmpeg', [
    '-y', '-hide_banner', '-loglevel', 'error',
    '-ss', String(at), '-i', video, '-frames:v', '1',
    '-vf', 'scale=1280:-2',
    out,
  ]);
}

export function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: ['ignore', 'pipe', 'pipe'], ...options });
    let stderr = '';
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} exited ${code}\n${stderr.trim()}`));
    });
  });
}

export async function launchStage(origin) {
  const browser = await chromium.launch({
    // Set CHROMIUM_PATH when the machine already has a Chromium that
    // Playwright did not install — CI images usually do, and downloading a
    // second copy to record a video is not worth the 150 MB.
    executablePath: process.env.CHROMIUM_PATH || undefined,
    args: [
      // The stage is a fixed-size canvas; letting Chromium pick a scrollbar
      // width or a device pixel ratio would move every coordinate a scene uses.
      '--hide-scrollbars',
      '--force-device-scale-factor=1',
      '--font-render-hinting=none',
      '--disable-lcd-text',
    ],
  });
  const context = await browser.newContext({
    viewport: { width: STAGE_WIDTH, height: STAGE_HEIGHT },
    deviceScaleFactor: 1,
    reducedMotion: 'no-preference',
  });
  // The console sends `X-Frame-Options: SAMEORIGIN`, which is right for the
  // product and fatal for a stage that shows it in a frame. Dropping the header
  // for the recording is preferable to weakening it in the server, or to
  // proxying the whole console through the stage's static server.
  //
  // Only document responses are rewritten. Replaying every request through
  // route.fetch() also replays the console's multipart uploads, and the copy
  // that arrives is not the one the browser built — starting a run from the
  // console fails with "Invalid configuration file". The header only has any
  // meaning on the frame's own document, so nothing else is touched.
  await context.route('**/*', async (route) => {
    if (route.request().resourceType() !== 'document') {
      await route.continue();
      return;
    }
    const response = await route.fetch();
    const headers = { ...response.headers() };
    delete headers['x-frame-options'];
    // The sign-in and administration pages say the same thing again in a
    // policy: `frame-ancestors 'none'`. Only that directive is dropped — the
    // rest of the policy stays on, so the page in the video is still running
    // under the script and connect rules it ships with.
    const csp = headers['content-security-policy'];
    if (csp) {
      const kept = csp.split(';')
        .filter((directive) => !/^\s*frame-ancestors\b/i.test(directive))
        .join(';');
      headers['content-security-policy'] = kept;
    }
    await route.fulfill({ response, headers });
  });

  const page = await context.newPage();
  await page.goto(`${origin}/lib/stage/stage.html`, { waitUntil: 'load' });
  await page.waitForFunction(() => window.stageReady === true);
  await page.evaluate(() => document.fonts.ready);
  return { browser, context, page };
}
