/* Stage runtime.
 *
 * Exposes `window.stage`, which the recorder drives from Node. Two shapes of
 * scene use it differently:
 *
 *   terminal scenes  hand the page a whole timeline and let it run — the cast
 *                    replay and its captions share one clock, so a caption
 *                    lands on the frame it was written for
 *   web scenes       are driven step by step from Node, because the actions
 *                    are Playwright clicks and only Node knows when one landed
 *
 * Timing here is deliberately wall-clock rather than frame-locked. The recorder
 * captures whatever the page happens to be showing, so drift of a frame or two
 * is invisible, and a real clock keeps the replay honest about how long the
 * commands underneath actually took.
 */
(() => {
  const el = (id) => document.getElementById(id);

  const nodes = {
    stage: el('stage'),
    chapter: el('chapter'),
    window: el('window'),
    windowTitle: el('windowtitle'),
    viewport: el('viewport'),
    term: el('term'),
    web: el('web'),
    caption: el('caption'),
    captionText: el('captionText'),
    captionSub: el('captionSub'),
    pointer: el('pointer'),
    spotlight: el('spotlight'),
    progressBar: el('progressBar'),
    card: el('card'),
    cardKicker: el('cardKicker'),
    cardTitle: el('cardTitle'),
    cardSub: el('cardSub'),
  };

  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

  // xterm's own theme keys, filled from the phosphor palette. The ANSI slots
  // matter: the TUI paints itself with 92/96/36/32, so those four decide what
  // most of a terminal video actually looks like.
  const TERMINAL_THEME = {
    background: '#03110d',
    foreground: '#cafee9',
    cursor: '#4effb3',
    cursorAccent: '#03110d',
    selectionBackground: 'rgba(78,255,176,0.28)',
    black: '#03110d',
    red: '#ff6f82',
    green: '#4effb3',
    yellow: '#f7c36b',
    blue: '#5ad2ff',
    magenta: '#b98cff',
    cyan: '#7cf9d0',
    white: '#cafee9',
    brightBlack: '#5f9e86',
    brightRed: '#ff8b9a',
    brightGreen: '#6effc2',
    brightYellow: '#ffd98f',
    brightBlue: '#8adcff',
    brightMagenta: '#d0b0ff',
    brightWhite: '#e6fff5',
  };

  let terminal = null;

  const stage = {
    /* ------------------------------------------------------------ chrome */

    chapter(text) {
      nodes.chapter.textContent = text || '';
      nodes.chapter.classList.toggle('on', Boolean(text));
    },

    windowTitle(text) {
      nodes.windowTitle.textContent = text || '';
    },

    showWindow(on = true) {
      nodes.window.classList.toggle('on', on);
    },

    progress(fraction) {
      nodes.progressBar.style.width = `${Math.max(0, Math.min(1, fraction)) * 100}%`;
    },

    /* ----------------------------------------------------------- captions */

    // `text` and `sub` accept a single level of markup: `code` spans written as
    // backticks. Anything else is escaped, because captions come from scene
    // files and a stray angle bracket should not become an element.
    caption(text, sub) {
      nodes.captionText.innerHTML = markup(text || '');
      nodes.captionSub.innerHTML = markup(sub || '');
      nodes.caption.classList.add('on');
    },

    clearCaption() {
      nodes.caption.classList.remove('on');
    },

    /* -------------------------------------------------------------- cards */

    async card({ kicker = '', title = '', sub = '' } = {}) {
      nodes.cardKicker.textContent = kicker;
      nodes.cardTitle.textContent = title;
      nodes.cardSub.innerHTML = markup(sub);
      nodes.card.hidden = false;
      // A frame between unhide and the class toggle, or the transition is
      // skipped and the card cuts in instead of fading.
      await sleep(30);
      nodes.card.classList.add('on');
    },

    async hideCard() {
      nodes.card.classList.remove('on');
      await sleep(560);
      nodes.card.hidden = true;
    },

    /* ------------------------------------------------------ pointer + focus */

    pointer(x, y, { show = true } = {}) {
      nodes.pointer.hidden = !show;
      nodes.pointer.style.transform = `translate3d(${x}px, ${y}px, 0)`;
    },

    async click(x, y) {
      this.pointer(x, y);
      await sleep(500);
      nodes.pointer.classList.remove('click');
      void nodes.pointer.offsetWidth; // restart the animation
      nodes.pointer.classList.add('click');
      await sleep(620);
      nodes.pointer.classList.remove('click');
    },

    spotlight(rect, { pad = 10 } = {}) {
      if (!rect) {
        nodes.spotlight.classList.remove('on');
        nodes.spotlight.hidden = true;
        return;
      }
      let ring = nodes.spotlight.querySelector('.ring');
      if (!ring) {
        ring = document.createElement('div');
        ring.className = 'ring';
        nodes.spotlight.appendChild(ring);
      }
      Object.assign(ring.style, {
        left: `${rect.x - pad}px`,
        top: `${rect.y - pad}px`,
        width: `${rect.width + pad * 2}px`,
        height: `${rect.height + pad * 2}px`,
      });
      nodes.spotlight.hidden = false;
      nodes.spotlight.classList.add('on');
    },

    /* ------------------------------------------------------ terminal mode */

    /* Mount a terminal at the largest font that still fits the stage.
     *
     * Legibility is the whole game here: these videos are watched inline in a
     * docs page at roughly half size, so a terminal sized to "whatever fits at
     * 18px" comes out unreadable. Instead the geometry is fixed by the cast and
     * the type is grown until one of the two edges is reached. Keep casts small
     * enough (around 106x24) that this lands near the 22px ceiling. */
    async mountTerminal({ cols, rows, fontSize = 22, maxWidth = 1460, maxHeight = 720 }) {
      nodes.web.hidden = true;
      nodes.term.hidden = false;

      let size = fontSize;
      for (let attempt = 0; attempt < 4; attempt += 1) {
        const measured = await buildTerminal({ cols, rows, fontSize: size });
        const scale = Math.min(maxWidth / measured.width, maxHeight / measured.height);
        if (scale >= 1) break;
        const next = Math.max(11, Math.floor(size * scale));
        if (next === size) break;
        size = next;
      }

      const screen = nodes.term.querySelector('.xterm-screen');
      const box = screen.getBoundingClientRect();
      nodes.window.style.width = `${Math.ceil(box.width) + 40}px`;
      nodes.viewport.style.height = `${Math.ceil(box.height) + 36}px`;
      return { width: box.width, height: box.height, fontSize: size };
    },

    /* Replay a cast and its captions on one clock.
     *
     * `events` is [[seconds, data], …] and `cues` is
     * [{at, until, text, sub, chapter}, …] with times already resolved to
     * seconds by the scene. Resolves when the last of either has been shown. */
    async play({ events, cues = [], tail = 0 }) {
      const started = performance.now();
      const now = () => (performance.now() - started) / 1000;
      const total = Math.max(
        events.length ? events[events.length - 1][0] : 0,
        ...cues.map((cue) => cue.until ?? cue.at),
        0,
      ) + tail;

      let nextEvent = 0;
      let nextCue = 0;
      const sortedCues = [...cues].sort((a, b) => a.at - b.at);
      // Hides are kept separate so a cue can end without another starting —
      // otherwise the caption bar would stay lit through a long quiet stretch.
      const hides = sortedCues
        .filter((cue) => cue.until != null)
        .map((cue) => cue.until)
        .sort((a, b) => a - b);
      let nextHide = 0;

      return new Promise((resolve) => {
        const tick = () => {
          const t = now();

          while (nextEvent < events.length && events[nextEvent][0] <= t) {
            terminal.write(events[nextEvent][1]);
            nextEvent += 1;
          }

          while (nextCue < sortedCues.length && sortedCues[nextCue].at <= t) {
            const cue = sortedCues[nextCue];
            if (cue.chapter !== undefined) this.chapter(cue.chapter);
            if (cue.text || cue.sub) this.caption(cue.text, cue.sub);
            nextCue += 1;
          }

          while (nextHide < hides.length && hides[nextHide] <= t) {
            // Only hide if no later cue has already taken the bar over.
            const overlapping = sortedCues.some(
              (cue) => cue.at <= t && (cue.until ?? Infinity) > t && (cue.text || cue.sub));
            if (!overlapping) this.clearCaption();
            nextHide += 1;
          }

          this.progress(total ? t / total : 1);

          if (t >= total) {
            resolve();
            return;
          }
          requestAnimationFrame(tick);
        };
        requestAnimationFrame(tick);
      });
    },

    /* ----------------------------------------------------------- web mode */

    mountWeb({ url, width, height }) {
      nodes.term.hidden = true;
      nodes.web.hidden = false;
      nodes.web.style.width = `${width}px`;
      nodes.web.style.height = `${height}px`;
      nodes.window.style.width = `${width}px`;
      nodes.viewport.style.height = `${height}px`;
      return new Promise((resolve) => {
        nodes.web.addEventListener('load', () => resolve(), { once: true });
        nodes.web.src = url;
      });
    },

    // Where the browser frame sits on the stage, so Node can turn a coordinate
    // inside the console into a coordinate it can click.
    webOrigin() {
      const box = nodes.web.getBoundingClientRect();
      return { x: box.x, y: box.y, width: box.width, height: box.height };
    },
  };

  /* Create (or replace) the xterm instance and report the size it settled on.
   *
   * The measurement has to happen two frames later: xterm derives its cell box
   * from the rendered font on first paint, and anything read before that is the
   * pre-layout guess — which is how the window first came out taller than the
   * stage and covered the header. */
  async function buildTerminal({ cols, rows, fontSize }) {
    if (terminal) {
      terminal.dispose();
      nodes.term.innerHTML = '';
    }
    terminal = new Terminal({
      cols,
      rows,
      fontSize,
      fontFamily: '"JetBrains Mono", "DejaVu Sans Mono", monospace',
      lineHeight: 1.15,
      letterSpacing: 0,
      theme: TERMINAL_THEME,
      cursorBlink: false,
      cursorStyle: 'block',
      disableStdin: true,
      convertEol: false,
      // No scrollback: a replay should never be able to show a scrollbar, and
      // lines that roll off the top are gone in the video exactly as they are
      // gone in a terminal somebody is watching.
      scrollback: 0,
      allowProposedApi: true,
    });
    terminal.open(nodes.term);
    await new Promise((resolve) => requestAnimationFrame(
      () => requestAnimationFrame(resolve)));
    const screen = nodes.term.querySelector('.xterm-screen');
    const box = screen.getBoundingClientRect();
    return { width: box.width, height: box.height };
  }

  function markup(text) {
    const escaped = String(text)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
    return escaped.replace(/`([^`]+)`/g, '<code>$1</code>');
  }

  window.stage = stage;
  window.stageReady = true;
})();
