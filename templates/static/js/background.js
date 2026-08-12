/* The drifting character field behind the sign-in and administration pages.
   -------------------------------------------------------------------------
   The console itself gets a static backdrop (dashboard.css, .backdrop). These
   pages get a moving one instead, because sign-in is the first thing an
   instance shows and a 3270 screen is a field of characters before it is
   anything else.

   Deliberately small: one mode, one theme, no dependency on the console's
   scripts, and it does nothing at all if the page has no canvas. The colour is
   read from the stylesheet's custom properties, so the light-mode courtesy in
   auth.css re-tints this with no work here.

   Motion is a choice. prefers-reduced-motion picks the default; clicking the
   backdrop — or pressing space or enter while it has focus — settles it, and
   the answer is remembered for next time. */
(function () {
  'use strict';

  var canvas = document.getElementById('bg-canvas');
  if (!canvas) {
    return;
  }
  var ctx = canvas.getContext('2d');
  if (!ctx) {
    return;
  }

  var overlay = canvas.parentElement;
  if (!overlay || !overlay.classList || !overlay.classList.contains('bg-overlay')) {
    overlay = null;
  }

  var STORAGE_KEY = '3270Connect.bgAnimation';
  var FONT = "'JetBrains Mono', ui-monospace, 'SFMono-Regular', Consolas, 'Courier New', monospace";
  var CHARS = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ#$*+-';

  /* Density is a count per unit of area rather than a fixed number, so a phone
     and a 4K monitor look the same rather than one of them looking empty. The
     divisor is tuned around a ~1200x700 viewport; the multiplier thins it to
     the weight the phosphor palette wants. */
  var AREA_PER_CHARACTER = 12000;
  var DENSITY = 0.7;
  var SPEED = 0.6;
  var MIN_CHARACTERS = 20;
  var MAX_CHARACTERS = 160;

  /* A tab left in the background returns with an enormous timestamp delta.
     Capping it stops every character on screen ageing out in the first frame
     after, which reads as the animation blinking. */
  var MAX_DELTA_SECONDS = 0.05;

  var MIN_SIZE = 12;
  var SIZE_RANGE = 18;
  var MIN_LIFE = 0.6;
  var LIFE_RANGE = 1.8;
  var DRIFT_RANGE = 12;
  var PEAK_ALPHA = 0.65;
  var PAUSE_KEYS = [' ', 'Enter'];

  var state = {
    width: 0,
    height: 0,
    items: [],
    running: true,
    last: 0,
    colour: '#4effb3'
  };

  function readColour() {
    var value = getComputedStyle(document.body).getPropertyValue('--accent');
    return (value && value.trim()) || '#4effb3';
  }

  function resize() {
    var ratio = window.devicePixelRatio || 1;
    var width = overlay ? overlay.clientWidth : window.innerWidth;
    var height = overlay ? overlay.clientHeight : window.innerHeight;
    if (!width || !height) {
      return;
    }
    state.width = width;
    state.height = height;
    canvas.width = width * ratio;
    canvas.height = height * ratio;
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  function newCharacter() {
    var life = MIN_LIFE + Math.random() * LIFE_RANGE;
    return {
      x: Math.random() * state.width,
      y: Math.random() * state.height,
      size: MIN_SIZE + Math.random() * SIZE_RANGE,
      character: CHARS.charAt(Math.floor(Math.random() * CHARS.length)),
      life: life,
      maxLife: life,
      drift: (Math.random() - 0.5) * DRIFT_RANGE
    };
  }

  function wantedCount() {
    var wanted = Math.round((state.width * state.height) / AREA_PER_CHARACTER * DENSITY);
    return Math.max(MIN_CHARACTERS, Math.min(MAX_CHARACTERS, wanted));
  }

  function draw(delta) {
    var wanted = wantedCount();
    while (state.items.length < wanted) {
      state.items.push(newCharacter());
    }
    ctx.clearRect(0, 0, state.width, state.height);
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = state.colour;
    for (var i = state.items.length - 1; i >= 0; i--) {
      var item = state.items[i];
      item.life -= delta * SPEED;
      item.y += item.drift * delta * SPEED;
      if (item.life <= 0) {
        state.items.splice(i, 1);
        continue;
      }
      /* Fade in and back out over the character's life rather than popping in:
         sine of the progress is nothing at both ends and everything halfway. */
      var progress = 1 - item.life / item.maxLife;
      ctx.globalAlpha = Math.sin(progress * Math.PI) * PEAK_ALPHA;
      ctx.font = item.size + 'px ' + FONT;
      ctx.fillText(item.character, item.x, item.y);
    }
    ctx.globalAlpha = 1;
  }

  function frame(timestamp) {
    if (!state.last) {
      state.last = timestamp;
    }
    var delta = Math.min((timestamp - state.last) / 1000, MAX_DELTA_SECONDS);
    state.last = timestamp;
    if (state.running) {
      draw(delta);
    }
    requestAnimationFrame(frame);
  }

  function setRunning(running) {
    state.running = running;
    document.body.classList.toggle('bg-paused', !running);
    if (overlay) {
      overlay.setAttribute('aria-pressed', running ? 'true' : 'false');
    }
  }

  /* Storage is wrapped because a browser in private mode, or one told to block
     site data, throws on both of these — and a backdrop is not worth a page
     that stops rendering. */
  function remembered() {
    try {
      return localStorage.getItem(STORAGE_KEY);
    } catch (err) {
      return null;
    }
  }

  function remember(value) {
    try {
      localStorage.setItem(STORAGE_KEY, value);
    } catch (err) {
      /* Nothing to do: the choice holds for this page and no longer. */
    }
  }

  function toggle() {
    setRunning(!state.running);
    remember(state.running ? 'on' : 'off');
  }

  function prefersReducedMotion() {
    return !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  }

  if (overlay) {
    overlay.addEventListener('click', function (event) {
      if (event.target === overlay || event.target === canvas) {
        toggle();
      }
    });
    overlay.addEventListener('keydown', function (event) {
      if (PAUSE_KEYS.indexOf(event.key) !== -1) {
        event.preventDefault();
        toggle();
      }
    });
  }

  window.addEventListener('resize', function () {
    resize();
    state.colour = readColour();
  });

  /* The palette follows the operating system's light or dark preference, so
     the drawing colour has to follow it too — nothing else would repaint it. */
  if (window.matchMedia) {
    var scheme = window.matchMedia('(prefers-color-scheme: light)');
    var onScheme = function () {
      state.colour = readColour();
    };
    if (scheme.addEventListener) {
      scheme.addEventListener('change', onScheme);
    } else if (scheme.addListener) {
      scheme.addListener(onScheme);
    }
  }

  resize();
  state.colour = readColour();

  /* An explicit choice wins in either direction; without one, an operating
     system asking for less motion gets a still page. */
  var stored = remembered();
  setRunning(stored === null ? !prefersReducedMotion() : stored !== 'off');

  requestAnimationFrame(frame);
})();
