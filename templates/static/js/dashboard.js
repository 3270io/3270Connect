/* ==========================================================================
   3270Connect — Operations Console
   Dashboard application logic.

   Boot data is provided by the Go template as window.__DASH__:
     { autoRefresh, refreshPeriod, metrics, version }

   Contents
   1.  Utilities
   2.  Preferences (localStorage)
   3.  Toasts
   4.  Tooltips
   5.  Micro-visuals (count-up, sparklines, gauges)
   6.  Derived statistics
   7.  Charts
   8.  KPI strip
   9.  Process table
   9b. Live screen flow
   10. Refresh engine
   11. Modals
   12. Command palette & keyboard shortcuts
   13. Boot
   ========================================================================== */

(function () {
  'use strict';

  var BOOT = window.__DASH__ || {};

  /* ======================================================================
     1. Utilities
     ====================================================================== */

  function $(sel, root) { return (root || document).querySelector(sel); }
  function $$(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  function el(tag, cls, text) {
    var node = document.createElement(tag);
    if (cls) { node.className = cls; }
    if (text !== undefined && text !== null) { node.textContent = String(text); }
    return node;
  }

  /* Icon markup helper — mirrors the inline sprite in the template. */
  function icon(name, extraClass) {
    return '<svg class="ic' + (extraClass ? ' ' + extraClass : '') + '" aria-hidden="true">' +
      '<use href="#i-' + name + '"></use></svg>';
  }

  function esc(value) {
    if (value === undefined || value === null) { return ''; }
    return String(value)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function clamp(value, lo, hi) { return Math.min(hi, Math.max(lo, value)); }

  function num(value, fallback) {
    var parsed = Number(value);
    return isFinite(parsed) ? parsed : (fallback === undefined ? 0 : fallback);
  }

  function fmtInt(value) {
    return num(value, 0).toLocaleString();
  }

  function fmtSeconds(value) {
    var total = Math.max(0, Math.round(num(value, 0)));
    var h = Math.floor(total / 3600);
    var m = Math.floor((total % 3600) / 60);
    var s = total % 60;
    if (h > 0) { return h + 'h ' + String(m).padStart(2, '0') + 'm'; }
    if (m > 0) { return m + 'm ' + String(s).padStart(2, '0') + 's'; }
    return s + 's';
  }

  function fmtDuration(value) {
    var v = num(value, 0);
    if (v >= 100) { return v.toFixed(0) + 's'; }
    if (v >= 10) { return v.toFixed(1) + 's'; }
    return v.toFixed(2) + 's';
  }

  function fmtClock(date) {
    return date.toLocaleTimeString([], { hour12: false });
  }

  function themeVar(name, fallback) {
    var value = getComputedStyle(document.documentElement).getPropertyValue(name);
    value = (value || '').trim();
    return value || fallback || '#4effb3';
  }

  /* Convert any CSS colour to rgba() with the requested alpha. */
  function alpha(color, a) {
    var probe = document.createElement('span');
    probe.style.color = color;
    probe.style.display = 'none';
    document.body.appendChild(probe);
    var resolved = getComputedStyle(probe).color;
    probe.remove();
    var parts = resolved.match(/[\d.]+/g);
    if (!parts || parts.length < 3) { return color; }
    return 'rgba(' + parts[0] + ',' + parts[1] + ',' + parts[2] + ',' + a + ')';
  }

  function copyText(text, label) {
    var done = function () { Toast.push('ok', 'Copied', (label || 'Content') + ' copied to clipboard.'); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { fallbackCopy(text, done); });
    } else {
      fallbackCopy(text, done);
    }
  }

  function fallbackCopy(text, done) {
    var area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    try { document.execCommand('copy'); done(); } catch (err) { Toast.push('bad', 'Copy failed', 'Clipboard unavailable in this browser.'); }
    area.remove();
  }

  function download(filename, content, mime) {
    var blob = new Blob([content], { type: mime || 'text/plain;charset=utf-8' });
    var url = URL.createObjectURL(blob);
    var link = el('a');
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
  }

  function csvCell(value) {
    var text = value === undefined || value === null ? '' : String(value);
    return /[",\n]/.test(text) ? '"' + text.replace(/"/g, '""') + '"' : text;
  }

  function toCSV(rows) {
    return rows.map(function (row) { return row.map(csvCell).join(','); }).join('\n');
  }

  /* ======================================================================
     2. Preferences
     ====================================================================== */

  var Prefs = (function () {
    var KEY = '3270connect.console.prefs.v1';
    var defaults = {
      theme: 'phosphor',
      density: 'comfortable',
      fx: 'on',
      view: 'table',
      wrap: 'on',
      // A control surface should be live out of the box; ?autoRefresh=true still
      // works, and whatever the operator chooses is remembered from then on.
      autoRefresh: true,
      refreshPeriod: String(BOOT.refreshPeriod || 5),
      chartWindow: '30',
      statusFilter: 'all',
      flowSort: 'onstep',
      flowStalledOnly: false
    };
    var data;

    try {
      data = Object.assign({}, defaults, JSON.parse(localStorage.getItem(KEY) || '{}'));
    } catch (err) {
      data = Object.assign({}, defaults);
    }

    function save() {
      try { localStorage.setItem(KEY, JSON.stringify(data)); } catch (err) { /* storage disabled */ }
    }

    return {
      get: function (key) { return data[key]; },
      set: function (key, value) { data[key] = value; save(); },
      all: function () { return data; },
      apply: function () {
        var root = document.documentElement;
        root.setAttribute('data-theme', data.theme);
        root.setAttribute('data-density', data.density);
        root.setAttribute('data-fx', data.fx);
        root.setAttribute('data-view', data.view);
        root.setAttribute('data-wrap', data.wrap);
      }
    };
  })();

  /* ======================================================================
     3. Toasts
     ====================================================================== */

  var Toast = (function () {
    var stack = null;
    var icons = { ok: 'circle-check', bad: 'triangle-exclamation', warn: 'circle-exclamation', info: 'circle-info' };

    function mount() {
      if (!stack) {
        stack = $('#toastStack');
      }
      return stack;
    }

    function push(tone, title, message, ttl) {
      var host = mount();
      if (!host) { return; }
      var life = ttl || (tone === 'bad' ? 8000 : 5000);

      var node = el('div', 'toast-x ' + (tone || 'info'));
      node.setAttribute('role', tone === 'bad' ? 'alert' : 'status');

      var ico = el('div', 'ico');
      ico.innerHTML = icon(icons[tone] || icons.info);

      var body = el('div', 'body');
      body.appendChild(el('div', 't', title));
      if (message) { body.appendChild(el('div', 'm', message)); }

      var close = el('button', 'x');
      close.type = 'button';
      close.setAttribute('aria-label', 'Dismiss notification');
      close.innerHTML = '<svg class="ic" aria-hidden="true"><use href="#i-xmark"></use></svg>';

      var timer = el('span', 'timer');
      timer.style.animationDuration = life + 'ms';

      node.appendChild(ico);
      node.appendChild(body);
      node.appendChild(close);
      node.appendChild(timer);
      host.appendChild(node);

      var dismissTimer = setTimeout(dismiss, life);
      function dismiss() {
        clearTimeout(dismissTimer);
        node.classList.add('leaving');
        setTimeout(function () { node.remove(); }, 260);
      }
      close.addEventListener('click', dismiss);

      // Cap the stack so a burst of errors cannot bury the UI.
      while (host.children.length > 5) { host.firstChild.remove(); }
    }

    return { push: push };
  })();

  /* ======================================================================
     4. Tooltips (replaces tippy/popper — no dependencies)
     ====================================================================== */

  var Tip = (function () {
    var node = null;
    var current = null;
    var showTimer = null;

    function ensure() {
      if (!node) {
        node = el('div', 'tip');
        node.setAttribute('role', 'tooltip');
        document.body.appendChild(node);
      }
      return node;
    }

    function place(target) {
      var tip = ensure();
      var rect = target.getBoundingClientRect();
      var box = tip.getBoundingClientRect();
      var top = rect.top - box.height - 9;
      var left = rect.left + (rect.width - box.width) / 2;
      if (top < 8) { top = rect.bottom + 9; }
      left = clamp(left, 8, window.innerWidth - box.width - 8);
      tip.style.top = Math.round(top) + 'px';
      tip.style.left = Math.round(left) + 'px';
    }

    function show(target) {
      var text = target.getAttribute('data-tip');
      if (!text) { return; }
      var tip = ensure();
      tip.textContent = text;
      tip.style.top = '-999px';
      tip.style.left = '-999px';
      tip.classList.add('show');
      current = target;
      requestAnimationFrame(function () { if (current === target) { place(target); } });
    }

    function hide() {
      current = null;
      clearTimeout(showTimer);
      if (node) { node.classList.remove('show'); }
    }

    function bind() {
      document.addEventListener('pointerover', function (event) {
        var target = event.target.closest ? event.target.closest('[data-tip]') : null;
        if (!target || target === current) { return; }
        clearTimeout(showTimer);
        showTimer = setTimeout(function () { show(target); }, 220);
      });
      document.addEventListener('pointerout', function (event) {
        var target = event.target.closest ? event.target.closest('[data-tip]') : null;
        if (target) { hide(); }
      });
      document.addEventListener('focusin', function (event) {
        var target = event.target.closest ? event.target.closest('[data-tip]') : null;
        if (target) { show(target); }
      });
      document.addEventListener('focusout', hide);
      window.addEventListener('scroll', hide, true);
      window.addEventListener('resize', hide);
      document.addEventListener('click', hide);
    }

    return { bind: bind, hide: hide };
  })();

  /* ======================================================================
     5. Micro-visuals
     ====================================================================== */

  var SVG_NS = 'http://www.w3.org/2000/svg';

  /* Animated number transition — respects prefers-reduced-motion. */
  function countTo(node, target) {
    var next = num(target, 0);
    var previous = num(node.getAttribute('data-value'), next);
    node.setAttribute('data-value', String(next));

    var reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced || previous === next || Math.abs(next - previous) > 100000) {
      node.textContent = fmtInt(next);
      return;
    }

    var start = performance.now();
    var span = 520;
    function frame(now) {
      var t = clamp((now - start) / span, 0, 1);
      var eased = 1 - Math.pow(1 - t, 3);
      node.textContent = fmtInt(Math.round(previous + (next - previous) * eased));
      if (t < 1) { requestAnimationFrame(frame); }
    }
    requestAnimationFrame(frame);
  }

  /* Dependency-free sparkline rendered straight into an <svg>. */
  function sparkline(svg, values, options) {
    var opts = options || {};
    var w = opts.width || 92;
    var h = opts.height || 34;
    var pad = 3;

    while (svg.firstChild) { svg.removeChild(svg.firstChild); }
    svg.setAttribute('viewBox', '0 0 ' + w + ' ' + h);
    svg.setAttribute('preserveAspectRatio', 'none');

    var data = (values || []).filter(function (v) { return v !== null && isFinite(v); });
    if (data.length < 2) { return; }

    var min = Math.min.apply(null, data);
    var max = Math.max.apply(null, data);
    var range = max - min || 1;
    var step = (w - pad * 2) / (data.length - 1);

    var points = data.map(function (value, index) {
      return {
        x: pad + index * step,
        y: pad + (h - pad * 2) * (1 - (value - min) / range)
      };
    });

    var line = points.map(function (p, i) {
      return (i === 0 ? 'M' : 'L') + p.x.toFixed(2) + ' ' + p.y.toFixed(2);
    }).join(' ');

    if (opts.area !== false) {
      var area = document.createElementNS(SVG_NS, 'path');
      area.setAttribute('class', 'area');
      area.setAttribute('d', line + ' L' + points[points.length - 1].x.toFixed(2) + ' ' + h + ' L' + points[0].x.toFixed(2) + ' ' + h + ' Z');
      svg.appendChild(area);
    }

    var path = document.createElementNS(SVG_NS, 'path');
    path.setAttribute('class', 'line');
    path.setAttribute('d', line);
    svg.appendChild(path);

    if (opts.head !== false) {
      var last = points[points.length - 1];
      var head = document.createElementNS(SVG_NS, 'circle');
      head.setAttribute('class', 'head');
      head.setAttribute('cx', last.x.toFixed(2));
      head.setAttribute('cy', last.y.toFixed(2));
      head.setAttribute('r', '2.2');
      svg.appendChild(head);
    }
  }

  /* Radial gauge (0–100). */
  function gauge(svg, percent) {
    var size = 88;
    var r = 36;
    var c = 2 * Math.PI * r;
    var value = clamp(num(percent, 0), 0, 100);

    if (!svg.firstChild) {
      svg.setAttribute('viewBox', '0 0 ' + size + ' ' + size);
      var track = document.createElementNS(SVG_NS, 'circle');
      track.setAttribute('class', 'track');
      track.setAttribute('cx', size / 2);
      track.setAttribute('cy', size / 2);
      track.setAttribute('r', r);
      svg.appendChild(track);

      var bar = document.createElementNS(SVG_NS, 'circle');
      bar.setAttribute('class', 'bar');
      bar.setAttribute('cx', size / 2);
      bar.setAttribute('cy', size / 2);
      bar.setAttribute('r', r);
      bar.setAttribute('transform', 'rotate(-90 ' + size / 2 + ' ' + size / 2 + ')');
      bar.setAttribute('stroke-dasharray', c.toFixed(2));
      bar.setAttribute('stroke-dashoffset', c.toFixed(2));
      svg.appendChild(bar);
    }

    svg.querySelector('.bar').setAttribute('stroke-dashoffset', (c * (1 - value / 100)).toFixed(2));
  }

  /* ======================================================================
     6. Derived statistics
     ====================================================================== */

  var state = {
    metrics: Array.isArray(BOOT.metrics) ? BOOT.metrics : [],
    history: [],          // rolling aggregate samples for sparklines/throughput
    previous: null,       // previous aggregate, for delta chips
    lastSampleAt: 0,
    sortKey: 'pid',
    sortDir: 'asc',
    query: '',
    statusFilter: Prefs.get('statusFilter') || 'all',
    flowSort: Prefs.get('flowSort') || 'onstep',
    flowStalledOnly: !!Prefs.get('flowStalledOnly'),
    /* Both transient on purpose: they answer "what is that one worker
       doing", and a run that has moved on should not still be filtered to a
       step it left ten minutes ago. */
    flowQuery: '',
    flowStep: null,
    flowShowAll: false,
    failures: 0,
    booted: false,
    seenPids: {}
  };

  function aggregate(metrics) {
    var agg = { active: 0, started: 0, completed: 0, failed: 0, processes: 0, running: 0 };
    (metrics || []).forEach(function (m) {
      agg.active += num(m.activeWorkflows);
      agg.started += num(m.totalWorkflowsStarted);
      agg.completed += num(m.totalWorkflowsCompleted);
      agg.failed += num(m.totalWorkflowsFailed);
      agg.processes += 1;
      if (m.isRunning) { agg.running += 1; }
    });
    agg.finished = agg.completed + agg.failed;
    agg.successRate = agg.finished > 0 ? (agg.completed / agg.finished) * 100 : null;
    return agg;
  }

  function allDurations(metrics) {
    var out = [];
    (metrics || []).forEach(function (m) {
      (m.durations || []).forEach(function (d) {
        if (d !== null && isFinite(d)) { out.push(d); }
      });
    });
    return out;
  }

  function percentile(sorted, p) {
    if (!sorted.length) { return null; }
    var idx = clamp(Math.ceil((p / 100) * sorted.length) - 1, 0, sorted.length - 1);
    return sorted[idx];
  }

  /* CPU is averaged across processes. Memory is host-wide, so every process
     records the same figure — take the longest series for the fullest history. */
  function resourceSeries(metrics) {
    var cpuTotals = [];
    var cpuCounts = [];
    var memory = [];

    (metrics || []).forEach(function (m) {
      if ((m.memoryUsage || []).length > memory.length) { memory = m.memoryUsage; }
      (m.cpuUsage || []).forEach(function (value, index) {
        cpuTotals[index] = (cpuTotals[index] || 0) + num(value);
        cpuCounts[index] = (cpuCounts[index] || 0) + 1;
      });
    });

    var cpu = cpuTotals.map(function (total, index) {
      return cpuCounts[index] ? total / cpuCounts[index] : 0;
    });
    return { cpu: cpu, memory: memory.slice() };
  }

  function pushHistorySample(agg, resources) {
    var now = Date.now();
    var cpu = resources.cpu.length ? resources.cpu[resources.cpu.length - 1] : null;
    var mem = resources.memory.length ? resources.memory[resources.memory.length - 1] : null;
    var previous = state.history[state.history.length - 1];

    var throughput = 0;
    if (previous) {
      var minutes = (now - previous.t) / 60000;
      if (minutes > 0.0005) {
        throughput = Math.max(0, (agg.completed - previous.completed) / minutes);
      } else {
        throughput = previous.throughput || 0;
      }
    }

    state.history.push({
      t: now,
      active: agg.active,
      started: agg.started,
      completed: agg.completed,
      failed: agg.failed,
      cpu: cpu,
      mem: mem,
      throughput: throughput
    });
    if (state.history.length > 180) { state.history.shift(); }
  }

  function historyOf(key) {
    return state.history.map(function (sample) { return sample[key]; });
  }

  /* ======================================================================
     7. Charts
     ====================================================================== */

  var Charts = (function () {
    var durationChart = null;
    var resourceChart = null;
    var outcomeChart = null;
    var available = typeof window.Chart !== 'undefined';

    var SERIES_COLORS = ['--accent', '--info', '--warn', '--violet', '--danger', '--ok'];

    function palette(index) {
      var fallbacks = ['#4effb3', '#5ad2ff', '#f7c36b', '#b98cff', '#ff6f82', '#4ee9b0'];
      return themeVar(SERIES_COLORS[index % SERIES_COLORS.length], fallbacks[index % fallbacks.length]);
    }

    function applyDefaults() {
      if (!available) { return; }
      var text3 = themeVar('--text-3', '#5f9e86');
      var line = themeVar('--line', 'rgba(78,255,176,0.16)');
      window.Chart.defaults.color = text3;
      window.Chart.defaults.borderColor = line;
      window.Chart.defaults.font.family = "'JetBrains Mono', ui-monospace, monospace";
      window.Chart.defaults.font.size = 11;
    }

    function axis(titleText, extra) {
      return Object.assign({
        border: { display: false },
        grid: { color: themeVar('--line', 'rgba(255,255,255,0.08)'), drawTicks: false },
        ticks: { padding: 8, maxRotation: 0, autoSkipPadding: 18 },
        title: {
          display: !!titleText,
          text: titleText,
          color: themeVar('--text-3', '#5f9e86'),
          font: { size: 10, weight: '700' },
          padding: { top: 6, bottom: 6 }
        }
      }, extra || {});
    }

    function tooltipStyle() {
      return {
        backgroundColor: themeVar('--surface-solid', '#06201a'),
        borderColor: themeVar('--line-strong', 'rgba(78,255,176,0.34)'),
        borderWidth: 1,
        titleColor: themeVar('--text', '#e6fff5'),
        bodyColor: themeVar('--text-2', '#9fe6c8'),
        padding: 10,
        cornerRadius: 10,
        displayColors: true,
        boxWidth: 8,
        boxHeight: 8,
        usePointStyle: true
      };
    }

    function zoomOptions() {
      if (!window.Chart || !window.Chart.registry || !window.Chart.registry.plugins.get('zoom')) { return undefined; }
      return {
        pan: { enabled: true, mode: 'x', threshold: 12 },
        zoom: { wheel: { enabled: true, speed: 0.08 }, pinch: { enabled: true }, mode: 'x' }
      };
    }

    function gradientFor(ctx, color) {
      var area = ctx.chart.chartArea;
      if (!area) { return alpha(color, 0.18); }
      var grad = ctx.chart.ctx.createLinearGradient(0, area.top, 0, area.bottom);
      grad.addColorStop(0, alpha(color, 0.34));
      grad.addColorStop(1, alpha(color, 0.01));
      return grad;
    }

    function lineDataset(label, data, color) {
      return {
        label: label,
        data: data,
        borderColor: color,
        backgroundColor: function (ctx) { return gradientFor(ctx, color); },
        fill: true,
        spanGaps: true,
        tension: 0.35,
        borderWidth: 2,
        pointRadius: 0,
        pointHoverRadius: 5,
        pointHitRadius: 14,
        pointBackgroundColor: color,
        pointBorderColor: themeVar('--surface-solid', '#06201a'),
        pointBorderWidth: 2
      };
    }

    /* --- data builders --- */

    function windowSize() {
      var value = Prefs.get('chartWindow');
      return value === 'all' ? Infinity : num(value, 30);
    }

    function buildDuration() {
      var limit = windowSize();
      var longest = 0;
      state.metrics.forEach(function (m) {
        longest = Math.max(longest, (m.durations || []).length);
      });
      if (!longest) { return { labels: [], datasets: [] }; }

      var take = Math.min(longest, limit === Infinity ? longest : limit);
      var offset = longest - take;
      var labels = [];
      for (var i = 0; i < take; i++) { labels.push(String(offset + i + 1)); }

      var datasets = state.metrics.map(function (m, index) {
        var durations = m.durations || [];
        var padded = new Array(longest - durations.length).fill(null).concat(durations);
        return lineDataset('PID ' + m.pid, padded.slice(offset), palette(index));
      });
      return { labels: labels, datasets: datasets };
    }

    function buildResources() {
      var limit = windowSize();
      var series = resourceSeries(state.metrics);
      var longest = Math.max(series.cpu.length, series.memory.length);
      if (!longest) { return { labels: [], datasets: [] }; }

      var take = Math.min(longest, limit === Infinity ? longest : limit);
      var offset = longest - take;
      var labels = [];
      for (var i = 0; i < take; i++) { labels.push(String((offset + i + 1) * 2) + 's'); }

      var cpu = new Array(longest - series.cpu.length).fill(null).concat(series.cpu).slice(offset);
      var mem = new Array(longest - series.memory.length).fill(null).concat(series.memory).slice(offset);

      return {
        labels: labels,
        datasets: [
          lineDataset('CPU %', cpu, themeVar('--info', '#5ad2ff')),
          lineDataset('Memory %', mem, themeVar('--violet', '#b98cff'))
        ]
      };
    }

    function buildOutcome() {
      var agg = aggregate(state.metrics);
      return {
        labels: ['Completed', 'Failed', 'In flight'],
        datasets: [{
          data: [agg.completed, agg.failed, agg.active],
          backgroundColor: [
            themeVar('--ok', '#4effb3'),
            themeVar('--danger', '#ff6f82'),
            themeVar('--info', '#5ad2ff')
          ],
          borderColor: themeVar('--surface-solid', '#06201a'),
          borderWidth: 3,
          hoverOffset: 8,
          spacing: 2
        }]
      };
    }

    /* --- lifecycle --- */

    function fallbackNotice() {
      $$('.chart-frame').forEach(function (frame) {
        frame.innerHTML =
          '<div class="empty"><div class="glyph"><svg class="ic" aria-hidden="true"><use href="#i-chart-area"></use></svg></div>' +
          '<div class="t">Charts unavailable</div>' +
          '<div class="d">The Chart.js library could not be loaded. Metrics tables, tiles and percentiles below remain fully functional.</div></div>';
      });
    }

    function init() {
      if (!available) { fallbackNotice(); return; }
      applyDefaults();

      durationChart = new window.Chart($('#durationChart').getContext('2d'), {
        type: 'line',
        data: buildDuration(),
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: { duration: 420, easing: 'easeOutQuart' },
          interaction: { mode: 'index', intersect: false },
          scales: {
            x: axis('Workflow #'),
            y: axis('Duration (s)', { beginAtZero: true })
          },
          plugins: {
            legend: { display: false },
            tooltip: Object.assign(tooltipStyle(), {
              callbacks: {
                label: function (ctx) { return ctx.dataset.label + ': ' + fmtDuration(ctx.parsed.y); }
              }
            }),
            zoom: zoomOptions()
          }
        }
      });

      resourceChart = new window.Chart($('#resourceChart').getContext('2d'), {
        type: 'line',
        data: buildResources(),
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: { duration: 420, easing: 'easeOutQuart' },
          interaction: { mode: 'index', intersect: false },
          scales: {
            x: axis('Elapsed'),
            y: axis('Utilisation %', { beginAtZero: true, max: 100 })
          },
          plugins: {
            legend: { display: false },
            tooltip: Object.assign(tooltipStyle(), {
              callbacks: {
                label: function (ctx) { return ctx.dataset.label + ': ' + num(ctx.parsed.y).toFixed(1) + '%'; }
              }
            }),
            zoom: zoomOptions()
          }
        }
      });

      outcomeChart = new window.Chart($('#outcomeChart').getContext('2d'), {
        type: 'doughnut',
        data: buildOutcome(),
        options: {
          responsive: true,
          maintainAspectRatio: false,
          cutout: '72%',
          animation: { duration: 520 },
          plugins: {
            legend: { display: false },
            tooltip: tooltipStyle()
          }
        }
      });

      renderLegends();
    }

    function update() {
      if (!available) { return; }

      if (durationChart) {
        var d = buildDuration();
        durationChart.data.labels = d.labels;
        durationChart.data.datasets = d.datasets.map(function (dataset, index) {
          var existing = durationChart.data.datasets[index];
          if (existing && existing.hidden) { dataset.hidden = true; }
          return dataset;
        });
        durationChart.update('none');
      }

      if (resourceChart) {
        var r = buildResources();
        resourceChart.data.labels = r.labels;
        r.datasets.forEach(function (dataset, index) {
          var existing = resourceChart.data.datasets[index];
          if (existing) {
            dataset.hidden = existing.hidden;
            resourceChart.data.datasets[index] = dataset;
          } else {
            resourceChart.data.datasets.push(dataset);
          }
        });
        resourceChart.update('none');
      }

      if (outcomeChart) {
        outcomeChart.data = buildOutcome();
        outcomeChart.update('none');
      }

      renderLegends();
    }

    /* Theme switch: rebuild colours from the new CSS variables. */
    function retheme() {
      if (!available) { return; }
      applyDefaults();
      [durationChart, resourceChart, outcomeChart].forEach(function (chart) {
        if (!chart) { return; }
        var scales = chart.options.scales;
        if (scales) {
          Object.keys(scales).forEach(function (key) {
            scales[key].grid.color = themeVar('--line', 'rgba(255,255,255,0.08)');
            if (scales[key].title) { scales[key].title.color = themeVar('--text-3', '#5f9e86'); }
          });
        }
        if (chart.options.plugins && chart.options.plugins.tooltip) {
          Object.assign(chart.options.plugins.tooltip, tooltipStyle());
        }
      });
      update();
    }

    function renderLegends() {
      buildLegend($('#durationLegend'), durationChart);
      buildLegend($('#resourceLegend'), resourceChart);
      buildOutcomeLegend();
    }

    function buildLegend(host, chart) {
      if (!host) { return; }
      host.innerHTML = '';
      if (!chart || !chart.data.datasets.length) { return; }

      chart.data.datasets.forEach(function (dataset, index) {
        var button = el('button');
        button.type = 'button';
        if (dataset.hidden) { button.classList.add('off'); }

        var swatch = el('span', 'swatch');
        swatch.style.background = typeof dataset.borderColor === 'string' ? dataset.borderColor : palette(index);
        button.appendChild(swatch);
        button.appendChild(document.createTextNode(dataset.label));

        button.addEventListener('click', function () {
          dataset.hidden = !dataset.hidden;
          button.classList.toggle('off', !!dataset.hidden);
          chart.update('none');
        });
        host.appendChild(button);
      });
    }

    function buildOutcomeLegend() {
      var host = $('#outcomeLegend');
      if (!host) { return; }
      var agg = aggregate(state.metrics);
      var rows = [
        { label: 'Completed', value: agg.completed, color: themeVar('--ok', '#4effb3') },
        { label: 'Failed', value: agg.failed, color: themeVar('--danger', '#ff6f82') },
        { label: 'In flight', value: agg.active, color: themeVar('--info', '#5ad2ff') }
      ];
      host.innerHTML = '';
      rows.forEach(function (row) {
        var button = el('button');
        button.type = 'button';
        button.disabled = true;
        var swatch = el('span', 'swatch');
        swatch.style.background = row.color;
        button.appendChild(swatch);
        button.appendChild(document.createTextNode(row.label + ' · ' + fmtInt(row.value)));
        host.appendChild(button);
      });

      var total = agg.finished;
      var big = $('#outcomeCenterValue');
      var small = $('#outcomeCenterLabel');
      if (big && small) {
        big.textContent = total ? Math.round((agg.completed / total) * 100) + '%' : '--';
        small.textContent = total ? 'success · ' + fmtInt(total) : 'no results yet';
      }
    }

    function resetZoom(which) {
      var chart = which === 'resource' ? resourceChart : durationChart;
      if (chart && typeof chart.resetZoom === 'function') { chart.resetZoom(); }
    }

    function exportPNG(which) {
      var chart = which === 'resource' ? resourceChart : (which === 'outcome' ? outcomeChart : durationChart);
      if (!chart) { Toast.push('warn', 'Unavailable', 'Chart library is not loaded.'); return; }
      var link = el('a');
      link.href = chart.toBase64Image('image/png', 1);
      link.download = '3270connect-' + which + '-' + Date.now() + '.png';
      link.click();
      Toast.push('ok', 'Exported', 'Chart saved as PNG.');
    }

    function exportCSV(which) {
      var chart = which === 'resource' ? resourceChart : durationChart;
      if (!chart) { Toast.push('warn', 'Unavailable', 'Chart library is not loaded.'); return; }
      var rows = [['index'].concat(chart.data.datasets.map(function (d) { return d.label; }))];
      chart.data.labels.forEach(function (label, i) {
        rows.push([label].concat(chart.data.datasets.map(function (d) {
          var v = d.data[i];
          return v === null || v === undefined ? '' : v;
        })));
      });
      download('3270connect-' + which + '-' + Date.now() + '.csv', toCSV(rows), 'text/csv;charset=utf-8');
      Toast.push('ok', 'Exported', 'Chart data saved as CSV.');
    }

    return {
      init: init,
      update: update,
      retheme: retheme,
      resetZoom: resetZoom,
      exportPNG: exportPNG,
      exportCSV: exportCSV,
      available: function () { return available; }
    };
  })();

  /* ======================================================================
     8. KPI strip + latency panel
     ====================================================================== */

  function renderDelta(node, delta) {
    if (!node) { return; }
    node.className = 'kpi-delta ' + (delta > 0 ? 'up' : (delta < 0 ? 'down' : 'flat'));
    var arrow = delta > 0 ? '▲' : (delta < 0 ? '▼' : '—');
    node.textContent = delta === 0 ? arrow : arrow + ' ' + fmtInt(Math.abs(delta));
  }

  function renderKPIs() {
    var agg = aggregate(state.metrics);
    var previous = state.previous;

    countTo($('#kpiActive'), agg.active);
    countTo($('#kpiStarted'), agg.started);
    countTo($('#kpiCompleted'), agg.completed);
    countTo($('#kpiFailed'), agg.failed);

    renderDelta($('#kpiActiveDelta'), previous ? agg.active - previous.active : 0);
    renderDelta($('#kpiStartedDelta'), previous ? agg.started - previous.started : 0);
    renderDelta($('#kpiCompletedDelta'), previous ? agg.completed - previous.completed : 0);

    // For failures an increase is bad, so invert the tone.
    var failedDelta = previous ? agg.failed - previous.failed : 0;
    var failedNode = $('#kpiFailedDelta');
    if (failedNode) {
      failedNode.className = 'kpi-delta ' + (failedDelta > 0 ? 'down' : (failedDelta < 0 ? 'up' : 'flat'));
      failedNode.textContent = failedDelta === 0 ? '—' : (failedDelta > 0 ? '▲ ' : '▼ ') + fmtInt(Math.abs(failedDelta));
    }

    var failedTile = $('#kpiFailedTile');
    if (failedTile) {
      failedTile.style.setProperty('--kpi-tint', agg.failed > 0 ? 'var(--danger)' : 'var(--text-3)');
    }

    sparkline($('#sparkActive'), historyOf('active'));
    sparkline($('#sparkStarted'), historyOf('started'));
    sparkline($('#sparkCompleted'), historyOf('completed'));
    sparkline($('#sparkFailed'), historyOf('failed'));

    // Success-rate gauge
    var rate = agg.successRate;
    var gaugeSvg = $('#successGauge');
    var gaugeValue = $('#kpiSuccessValue');
    var gaugeTile = $('#kpiSuccessTile');
    if (gaugeSvg && gaugeValue) {
      gauge(gaugeSvg, rate === null ? 0 : rate);
      gaugeValue.textContent = rate === null ? '--' : rate.toFixed(1);
      if (gaugeTile) {
        var tint = rate === null ? 'var(--text-3)' : (rate >= 99 ? 'var(--ok)' : (rate >= 90 ? 'var(--warn)' : 'var(--danger)'));
        gaugeTile.style.setProperty('--kpi-tint', tint);
      }
    }
    var gaugeNote = $('#kpiSuccessNote');
    if (gaugeNote) {
      gaugeNote.textContent = agg.finished ? fmtInt(agg.finished) + ' finished workflows' : 'awaiting first result';
    }

    // Throughput
    var latest = state.history[state.history.length - 1];
    var throughput = latest ? latest.throughput : 0;
    var tpNode = $('#kpiThroughput');
    if (tpNode) { tpNode.textContent = throughput >= 100 ? throughput.toFixed(0) : throughput.toFixed(1); }
    sparkline($('#sparkThroughput'), historyOf('throughput'));
    var tpNote = $('#kpiThroughputNote');
    if (tpNote) {
      tpNote.textContent = agg.running ? agg.running + ' live process' + (agg.running === 1 ? '' : 'es') : 'no live processes';
    }

    // Topbar live readout
    var liveProcesses = $('#liveProcesses');
    if (liveProcesses) { liveProcesses.textContent = fmtInt(agg.running); }
    var liveActive = $('#liveActive');
    if (liveActive) { liveActive.textContent = fmtInt(agg.active); }
    var liveDot = $('#liveDot');
    if (liveDot) {
      liveDot.className = 'dot ' + (agg.running ? 'live' : '');
    }
    var liveState = $('#liveState');
    if (liveState) { liveState.textContent = agg.running ? 'RUNNING' : 'IDLE'; }

    state.previous = agg;
  }

  function renderLatency() {
    var durations = allDurations(state.metrics).sort(function (a, b) { return a - b; });
    var host = $('#pctList');
    var histo = $('#histogram');
    var axisHost = $('#histogramAxis');
    var stats = $('#latencyStats');

    if (!durations.length) {
      if (host) {
        host.innerHTML = '<div class="empty" style="padding:24px 8px"><div class="t">No samples</div>' +
          '<div class="d">Workflow durations appear here once a run completes.</div></div>';
      }
      if (histo) { histo.innerHTML = ''; }
      if (axisHost) { axisHost.innerHTML = ''; }
      if (stats) { stats.innerHTML = ''; }
      return;
    }

    var rows = [
      { key: 'p50', value: percentile(durations, 50), tint: 'var(--ok)' },
      { key: 'p90', value: percentile(durations, 90), tint: 'var(--info)' },
      { key: 'p99', value: percentile(durations, 99), tint: 'var(--warn)' },
      { key: 'max', value: durations[durations.length - 1], tint: 'var(--danger)' }
    ];
    var peak = rows[rows.length - 1].value || 1;

    if (host) {
      host.innerHTML = '';
      rows.forEach(function (row) {
        var wrap = el('div', 'pct-row');
        wrap.style.setProperty('--pct-tint', row.tint);
        wrap.appendChild(el('span', 'k', row.key));
        var meter = el('div', 'meter');
        var fill = el('i');
        fill.style.width = clamp((row.value / peak) * 100, 2, 100) + '%';
        meter.appendChild(fill);
        wrap.appendChild(meter);
        wrap.appendChild(el('span', 'v', fmtDuration(row.value)));
        host.appendChild(wrap);
      });
    }

    // Histogram over 18 buckets
    if (histo) {
      var min = durations[0];
      var max = durations[durations.length - 1];
      var span = (max - min) || 1;
      var buckets = new Array(18).fill(0);
      durations.forEach(function (value) {
        var idx = clamp(Math.floor(((value - min) / span) * 18), 0, 17);
        buckets[idx] += 1;
      });
      var tallest = Math.max.apply(null, buckets) || 1;

      histo.innerHTML = '';
      buckets.forEach(function (count, index) {
        var bar = el('div', 'bar');
        bar.style.height = Math.max(2, (count / tallest) * 92) + 'px';
        var lo = min + (span / 18) * index;
        var hi = min + (span / 18) * (index + 1);
        bar.setAttribute('data-tip', fmtInt(count) + ' run' + (count === 1 ? '' : 's') + ' · ' + fmtDuration(lo) + ' – ' + fmtDuration(hi));
        histo.appendChild(bar);
      });

      if (axisHost) {
        axisHost.innerHTML = '';
        axisHost.appendChild(el('span', null, fmtDuration(min)));
        axisHost.appendChild(el('span', null, fmtDuration((min + max) / 2)));
        axisHost.appendChild(el('span', null, fmtDuration(max)));
      }
    }

    if (stats) {
      var sum = durations.reduce(function (a, b) { return a + b; }, 0);
      var mean = sum / durations.length;
      var variance = durations.reduce(function (acc, v) { return acc + Math.pow(v - mean, 2); }, 0) / durations.length;
      var pairs = [
        ['Samples', fmtInt(durations.length)],
        ['Mean', fmtDuration(mean)],
        ['Min', fmtDuration(durations[0])],
        ['Std dev', fmtDuration(Math.sqrt(variance))]
      ];
      stats.innerHTML = '';
      pairs.forEach(function (pair) {
        var box = el('div', 'mini-stat');
        box.appendChild(el('div', 'k', pair[0]));
        box.appendChild(el('div', 'v', pair[1]));
        stats.appendChild(box);
      });
    }
  }

  /* ======================================================================
     9. Process table
     ====================================================================== */

  var metaByPid = {};

  function avgOf(durations) {
    if (!durations || !durations.length) { return NaN; }
    return durations.reduce(function (a, b) { return a + b; }, 0) / durations.length;
  }

  function visibleMetrics() {
    var query = state.query.trim().toLowerCase();
    var filter = state.statusFilter;

    var rows = state.metrics.filter(function (m) {
      if (filter === 'running' && !m.isRunning) { return false; }
      if (filter === 'ended' && m.isRunning) { return false; }
      if (filter === 'failing' && num(m.totalWorkflowsFailed) === 0) { return false; }
      if (!query) { return true; }
      var haystack = [m.pid, m.status, m.params, m.configFilePath, m.outputFilePath].join(' ').toLowerCase();
      return haystack.indexOf(query) !== -1;
    });

    var key = state.sortKey;
    var dir = state.sortDir === 'desc' ? -1 : 1;
    rows.sort(function (a, b) {
      var av, bv;
      if (key === 'avg') {
        av = avgOf(a.durations); bv = avgOf(b.durations);
        av = isNaN(av) ? -1 : av; bv = isNaN(bv) ? -1 : bv;
      } else if (key === 'status') {
        av = a.isRunning ? 1 : 0; bv = b.isRunning ? 1 : 0;
      } else if (key === 'params') {
        av = String(a.params || ''); bv = String(b.params || '');
        return av.localeCompare(bv) * dir;
      } else {
        av = num(a[key]); bv = num(b[key]);
      }
      return (av - bv) * dir;
    });
    return rows;
  }

  function statusChip(metric) {
    var running = !!metric.isRunning;
    var label = metric.status || (running ? 'Running' : 'Ended');
    var chip = el('span', 'chip ' + (running ? 'ok' : ''));
    var dot = el('span', 'dot ' + (running ? 'live' : ''));
    chip.appendChild(dot);
    chip.appendChild(document.createTextNode(label));
    return chip;
  }

  function progressCell(metric) {
    var cell = el('td', 'progress-cell');
    var total = num(metric.runtimeDuration);
    var left = num(metric.timeLeft);

    if (!metric.isRunning || total <= 0) {
      var chip = el('span', 'chip', metric.isRunning ? 'unbounded' : 'finished');
      cell.appendChild(chip);
      return cell;
    }

    var elapsed = clamp(total - left, 0, total);
    var pct = clamp((elapsed / total) * 100, 0, 100);

    var bar = el('div', 'bar');
    var fill = el('i');
    fill.style.width = pct.toFixed(1) + '%';
    bar.appendChild(fill);

    var label = el('div', 'lbl');
    label.appendChild(el('span', null, Math.round(pct) + '%'));
    label.appendChild(el('span', null, fmtSeconds(left) + ' left'));

    cell.appendChild(bar);
    cell.appendChild(label);
    return cell;
  }

  function ratioCell(metric) {
    var cell = el('td');
    var completed = num(metric.totalWorkflowsCompleted);
    var failed = num(metric.totalWorkflowsFailed);
    var total = completed + failed;

    var wrap = el('div', 'ratio-bar');
    if (total === 0) {
      wrap.setAttribute('data-tip', 'No finished workflows yet');
    } else {
      var ok = el('i', 'ok');
      ok.style.width = ((completed / total) * 100) + '%';
      var bad = el('i', 'bad');
      bad.style.width = ((failed / total) * 100) + '%';
      wrap.appendChild(ok);
      wrap.appendChild(bad);
      wrap.setAttribute('data-tip', fmtInt(completed) + ' ok / ' + fmtInt(failed) + ' failed (' + ((completed / total) * 100).toFixed(1) + '%)');
    }
    cell.appendChild(wrap);
    return cell;
  }

  function avgCell(metric) {
    var cell = el('td');
    var durations = metric.durations || [];
    var avg = avgOf(durations);

    var wrap = el('div');
    wrap.style.display = 'flex';
    wrap.style.alignItems = 'center';
    wrap.style.gap = '8px';

    var value = el('span', 'num', isNaN(avg) ? '--' : fmtDuration(avg));
    value.style.fontWeight = '700';
    wrap.appendChild(value);

    if (durations.length > 1) {
      var svg = document.createElementNS(SVG_NS, 'svg');
      svg.setAttribute('class', 'cell-spark');
      wrap.appendChild(svg);
      sparkline(svg, durations.slice(-24), { width: 74, height: 22, area: false, head: false });
    }
    cell.appendChild(wrap);
    return cell;
  }

  function actionsCell(metric) {
    var cell = el('td');
    var wrap = el('div', 'row-actions');
    var params = String(metric.params || '').trim() || '-dashboard';
    var hasConfig = !!(metric.configFilePath && metric.configFilePath.trim());
    var hasOutput = !!(metric.outputFilePath && metric.outputFilePath.trim());

    function add(kind, glyph, tip) {
      var button = el('button', 'act a-' + kind);
      button.type = 'button';
      button.setAttribute('data-act', kind);
      button.setAttribute('data-pid', metric.pid);
      button.setAttribute('data-tip', tip);
      button.setAttribute('aria-label', tip);
      button.innerHTML = icon(glyph);
      wrap.appendChild(button);
    }

    if (hasConfig && params !== '-dashboard') { add('workflow', 'file-code', 'View workflow JSON'); }
    if (hasOutput) { add('output', 'film', 'Browse captured screens'); }
    if (hasConfig && metric.status === 'Ended') { add('summary', 'file-lines', 'View performance summary'); }
    add('logs', 'terminal', 'View logs for this PID');
    add('kill', 'skull-crossbones', 'Terminate process');

    cell.appendChild(wrap);
    return cell;
  }

  function renderTable() {
    var rows = visibleMetrics();
    var tbody = $('#procBody');
    var empty = $('#procEmpty');
    var wrap = $('#procGridWrap');
    var cards = $('#procCards');
    if (!tbody) { return; }

    metaByPid = {};
    state.metrics.forEach(function (m) {
      metaByPid[m.pid] = { configPath: m.configFilePath || '', outputPath: m.outputFilePath || '', params: m.params || '' };
    });

    var count = $('#procCount');
    if (count) {
      count.textContent = rows.length === state.metrics.length
        ? fmtInt(rows.length) + ' process' + (rows.length === 1 ? '' : 'es')
        : fmtInt(rows.length) + ' of ' + fmtInt(state.metrics.length);
    }

    if (!rows.length) {
      tbody.innerHTML = '';
      if (cards) { cards.innerHTML = ''; }
      if (empty) {
        empty.hidden = false;
        var known = state.metrics.length > 0;
        $('#procEmptyTitle').textContent = known ? 'No matching processes' : 'No processes yet';
        $('#procEmptyBody').textContent = known
          ? 'Nothing matches the current search or status filter. Clear the filters to see all processes.'
          : 'Start a 3270Connect run or a sample app to populate the console. Press Ctrl+K for the command palette.';
      }
      if (wrap) { wrap.hidden = true; }
      return;
    }

    if (empty) { empty.hidden = true; }
    if (wrap) { wrap.hidden = false; }

    tbody.innerHTML = '';
    rows.forEach(function (metric) {
      var tr = el('tr');
      if (!metric.isRunning) { tr.classList.add('is-dead'); }
      if (!state.seenPids[metric.pid] && state.booted) { tr.classList.add('flash-new'); }
      state.seenPids[metric.pid] = true;

      tr.appendChild(actionsCell(metric));

      var pidCell = el('td');
      var pidWrap = el('div', 'pid-cell');
      pidWrap.appendChild(el('span', 'pid num', metric.pid));
      pidCell.appendChild(pidWrap);
      tr.appendChild(pidCell);

      var statusCell = el('td');
      statusCell.appendChild(statusChip(metric));
      tr.appendChild(statusCell);

      tr.appendChild(progressCell(metric));

      ['activeWorkflows', 'totalWorkflowsStarted', 'totalWorkflowsCompleted', 'totalWorkflowsFailed'].forEach(function (key) {
        var cell = el('td', 'num', fmtInt(metric[key]));
        if (key === 'totalWorkflowsFailed' && num(metric[key]) > 0) { cell.style.color = 'var(--danger)'; }
        if (key === 'totalWorkflowsCompleted' && num(metric[key]) > 0) { cell.style.color = 'var(--ok)'; }
        tr.appendChild(cell);
      });

      tr.appendChild(ratioCell(metric));
      tr.appendChild(avgCell(metric));

      var paramsCell = el('td');
      var params = String(metric.params || '').trim() || '-dashboard';
      var chip = el('div', 'params-chip');
      chip.appendChild(el('span', null, params));
      var copyBtn = el('button');
      copyBtn.type = 'button';
      copyBtn.setAttribute('data-act', 'copy-params');
      copyBtn.setAttribute('data-pid', metric.pid);
      copyBtn.setAttribute('data-tip', 'Copy parameters');
      copyBtn.setAttribute('aria-label', 'Copy parameters');
      copyBtn.innerHTML = '<svg class="ic" aria-hidden="true"><use href="#i-copy"></use></svg>';
      chip.appendChild(copyBtn);
      chip.setAttribute('data-tip', params);
      paramsCell.appendChild(chip);
      tr.appendChild(paramsCell);

      tbody.appendChild(tr);
    });

    renderCards(rows);
  }

  function renderCards(rows) {
    var host = $('#procCards');
    if (!host) { return; }
    host.innerHTML = '';

    rows.forEach(function (metric) {
      var card = el('div', 'proc-card');

      var top = el('div', 'top');
      top.appendChild(el('span', 'pid num', 'PID ' + metric.pid));
      top.appendChild(statusChip(metric));
      card.appendChild(top);

      var metrics = el('div', 'metrics');
      [
        ['Active', fmtInt(metric.activeWorkflows)],
        ['Start', fmtInt(metric.totalWorkflowsStarted)],
        ['Done', fmtInt(metric.totalWorkflowsCompleted)],
        ['Fail', fmtInt(metric.totalWorkflowsFailed)]
      ].forEach(function (pair) {
        var box = el('div');
        box.appendChild(el('div', 'k', pair[0]));
        box.appendChild(el('div', 'v num', pair[1]));
        metrics.appendChild(box);
      });
      card.appendChild(metrics);

      var actions = actionsCell(metric).firstChild;
      card.appendChild(actions);
      host.appendChild(card);
    });
  }

  function bindTable() {
    $$('#procTable thead th.sortable').forEach(function (th) {
      th.addEventListener('click', function () {
        var key = th.getAttribute('data-key');
        if (state.sortKey === key) {
          state.sortDir = state.sortDir === 'asc' ? 'desc' : 'asc';
        } else {
          state.sortKey = key;
          state.sortDir = key === 'pid' ? 'asc' : 'desc';
        }
        $$('#procTable thead th').forEach(function (other) { other.removeAttribute('aria-sort'); });
        th.setAttribute('aria-sort', state.sortDir === 'asc' ? 'ascending' : 'descending');
        renderTable();
      });
    });

    var search = $('#procSearch');
    if (search) {
      search.addEventListener('input', function () {
        state.query = search.value;
        $('#procSearchClear').hidden = !search.value;
        renderTable();
      });
    }
    var clear = $('#procSearchClear');
    if (clear) {
      clear.addEventListener('click', function () {
        search.value = '';
        state.query = '';
        clear.hidden = true;
        renderTable();
        search.focus();
      });
    }

    $$('#statusFilter button').forEach(function (button) {
      button.addEventListener('click', function () {
        state.statusFilter = button.getAttribute('data-filter');
        Prefs.set('statusFilter', state.statusFilter);
        $$('#statusFilter button').forEach(function (other) {
          other.setAttribute('aria-pressed', String(other === button));
        });
        renderTable();
      });
    });

    // Event delegation for row actions — keeps markup free of inline handlers.
    document.addEventListener('click', function (event) {
      var button = event.target.closest ? event.target.closest('[data-act]') : null;
      if (!button) { return; }
      var pid = button.getAttribute('data-pid');
      switch (button.getAttribute('data-act')) {
        case 'workflow': Modals.workflow(pid); break;
        case 'output': Modals.output(pid); break;
        case 'summary': Modals.summary(pid); break;
        case 'logs': Modals.logsFor(pid); break;
        case 'kill': Modals.confirmKill(pid); break;
        case 'copy-params':
          copyText((metaByPid[pid] && metaByPid[pid].params) || '-dashboard', 'Parameters');
          break;
        default: break;
      }
    });

    var exportBtn = $('#exportProcesses');
    if (exportBtn) {
      exportBtn.addEventListener('click', function () {
        var rows = [['pid', 'status', 'running', 'timeLeft', 'active', 'started', 'completed', 'failed', 'avgDuration', 'params', 'config', 'output']];
        visibleMetrics().forEach(function (m) {
          var avg = avgOf(m.durations);
          rows.push([
            m.pid, m.status || '', m.isRunning ? 'yes' : 'no', num(m.timeLeft),
            num(m.activeWorkflows), num(m.totalWorkflowsStarted), num(m.totalWorkflowsCompleted),
            num(m.totalWorkflowsFailed), isNaN(avg) ? '' : avg.toFixed(3),
            m.params || '', m.configFilePath || '', m.outputFilePath || ''
          ]);
        });
        download('3270connect-processes-' + Date.now() + '.csv', toCSV(rows), 'text/csv;charset=utf-8');
        Toast.push('ok', 'Exported', 'Process table saved as CSV.');
      });
    }
  }

  /* ======================================================================
     9b. Live screen flow

     Every other panel counts workflows. This one shows the virtual users
     themselves: which step of the screen flow each is on, and — the figure
     that matters — how long it has been sitting there.

     Time on a single step is what separates a slow run from a stuck one. A
     worker two minutes into its workflow is ordinary; a worker two minutes
     into one CheckValue is a host that has stopped painting screens. So the
     rows are tinted and sorted by it, and the fleet distribution beside them
     shows the pile-up that a stall produces.

     Runs older than this field do not publish it (onStepSeconds is absent).
     Those workers are shown plainly with a dash rather than a fabricated
     zero, which would read as "just started".
     ====================================================================== */

  /* Seconds on one step before a worker is called slow, then stalled. A 3270
     transaction is a fraction of a second when the host is healthy, so these
     are generous rather than tight: the point is to catch a stop, not to
     grumble about a busy mainframe. */
  var FLOW_SLOW_SECONDS = 10;
  var FLOW_STALLED_SECONDS = 30;

  /* Most rows the worker list draws. Beyond this the list is a wall rather
     than a view, and the fleet column answers the question better anyway. */
  var FLOW_MAX_ROWS = 60;

  /* Steps that are expected to take their time. Connect waits on a TCP
     session and a host greeting, and a deliberate delay is a delay — neither
     is a stall however long it sits, so neither is coloured as one. */
  var FLOW_PATIENT_STEPS = { Connect: true, Disconnect: true, StepDelay: true, starting: true };

  function flowWorkers() {
    var workers = [];
    (state.metrics || []).forEach(function (metric) {
      if (!metric || !metric.isRunning) { return; }
      (metric.liveSteps || []).forEach(function (step) {
        workers.push({
          pid: metric.pid,
          scriptPort: step.scriptPort || '',
          host: step.host || '',
          port: num(step.port),
          currentStep: num(step.currentStep),
          totalSteps: num(step.totalSteps),
          stepType: step.stepType || '',
          stepDetail: step.stepDetail || '',
          stepStartedAt: num(step.stepStartedAt),
          startedAt: num(step.startedAt),
          /* Absent rather than zero when the run does not publish it. */
          onStep: sinceEpoch(step.stepStartedAt),
          elapsed: sinceEpoch(step.startedAt)
        });
      });
    });
    return workers;
  }

  function nowSeconds() { return Math.floor(Date.now() / 1000); }

  /* Seconds since a unix timestamp, or null where there is none to measure
     from. Null travels all the way to the rendered dash: a run that cannot
     report the figure must not be shown a zero, which reads as "just
     started" — the opposite of what an unknown means here. */
  function sinceEpoch(timestamp) {
    var value = num(timestamp);
    if (value <= 0) { return null; }
    return Math.max(0, nowSeconds() - value);
  }

  /* 'ok' | 'slow' | 'stalled' | 'unknown' — drives both colour and filtering. */
  function flowSeverity(worker) {
    if (worker.onStep === null) { return 'unknown'; }
    if (FLOW_PATIENT_STEPS[worker.stepType]) { return 'ok'; }
    if (worker.onStep >= FLOW_STALLED_SECONDS) { return 'stalled'; }
    if (worker.onStep >= FLOW_SLOW_SECONDS) { return 'slow'; }
    return 'ok';
  }

  var FLOW_TONES = {
    ok: 'var(--ok)',
    slow: 'var(--warn)',
    stalled: 'var(--danger)',
    unknown: 'var(--text-3)'
  };

  function sortFlowWorkers(workers) {
    var mode = state.flowSort;
    return workers.slice().sort(function (a, b) {
      if (mode === 'port') {
        return String(a.scriptPort).localeCompare(String(b.scriptPort), undefined, { numeric: true });
      }
      if (mode === 'step') {
        return (a.currentStep - b.currentStep) ||
          String(a.scriptPort).localeCompare(String(b.scriptPort), undefined, { numeric: true });
      }
      /* Longest on its current step first: whatever is wrong surfaces at the
         top without the operator scrolling for it. Workers whose run cannot
         report the figure sort last rather than first, so an old run does not
         crowd out a real stall. */
      var av = a.onStep === null ? -1 : a.onStep;
      var bv = b.onStep === null ? -1 : b.onStep;
      return bv - av;
    });
  }

  /* One entry per step position workers are sitting on, busiest first. */
  function flowStages(workers) {
    var byKey = {};
    workers.forEach(function (worker) {
      var key = worker.currentStep + '|' + worker.stepType;
      if (!byKey[key]) {
        byKey[key] = {
          currentStep: worker.currentStep,
          totalSteps: worker.totalSteps,
          stepType: worker.stepType,
          detail: worker.stepDetail,
          count: 0,
          slowest: null
        };
      }
      var stage = byKey[key];
      stage.count += 1;
      if (worker.onStep !== null && (stage.slowest === null || worker.onStep > stage.slowest)) {
        stage.slowest = worker.onStep;
      }
      /* Details can differ between workers on the same step only when their
         workflows differ; first one wins and the count carries the rest. */
      if (!stage.detail && worker.stepDetail) { stage.detail = worker.stepDetail; }
    });

    return Object.keys(byKey).map(function (key) { return byKey[key]; })
      .sort(function (a, b) { return (b.count - a.count) || (a.currentStep - b.currentStep); });
  }

  function fmtOnStep(seconds) {
    if (seconds === null) { return '--'; }
    return fmtSeconds(seconds);
  }

  function renderFlowSummary(workers, stages) {
    var host = $('#flowSummary');
    if (!host) { return; }
    host.innerHTML = '';
    if (!workers.length) { return; }

    var stalled = workers.filter(function (w) { return flowSeverity(w) === 'stalled'; });
    var slow = workers.filter(function (w) { return flowSeverity(w) === 'slow'; });
    var busiest = stages[0];
    var hosts = {};
    workers.forEach(function (w) { if (w.host) { hosts[w.host + ':' + w.port] = true; } });
    var hostNames = Object.keys(hosts);

    function stat(key, value, tone, tip) {
      var node = el('span', 'flow-stat' + (tone ? ' ' + tone : ''));
      node.appendChild(el('span', 'k', key));
      node.appendChild(el('span', 'v', value));
      if (tip) { node.setAttribute('data-tip', tip); }
      host.appendChild(node);
    }

    stat('in flight', fmtInt(workers.length), null, 'Virtual users currently executing a step');
    if (busiest) {
      stat('busiest step', stepLabel(busiest) + ' × ' + fmtInt(busiest.count),
        busiest.count > 1 && busiest.count === workers.length ? 'warn' : null,
        busiest.count === workers.length && workers.length > 1
          ? 'Every worker is on this step — the host is slow at this transaction, not slow in general'
          : 'The step the most workers are sitting on');
    }
    if (stalled.length) {
      stat('stalled', fmtInt(stalled.length), 'bad',
        'On one step for ' + FLOW_STALLED_SECONDS + 's or more');
    } else if (slow.length) {
      stat('slow', fmtInt(slow.length), 'warn',
        'On one step for ' + FLOW_SLOW_SECONDS + 's or more');
    }
    if (hostNames.length === 1) {
      stat('host', hostNames[0], null, 'The host under test');
    } else if (hostNames.length > 1) {
      stat('hosts', fmtInt(hostNames.length), null, hostNames.join(', '));
    }
  }

  function stepLabel(stage) {
    var position = stage.totalSteps > 0
      ? stage.currentStep + '/' + stage.totalSteps
      : String(stage.currentStep);
    return position + ' ' + (stage.stepType || '—');
  }

  function renderFlowStage(workers, stages) {
    var host = $('#flowStage');
    if (!host) { return; }
    host.innerHTML = '';
    if (!stages.length) { return; }

    var max = stages[0].count || 1;
    stages.forEach(function (stage) {
      var isolated = state.flowStep === stage.currentStep;
      var row = el('button', 'stage-row' +
        (stage.count > 1 && stage.count >= workers.length / 2 ? ' hot' : '') +
        (isolated ? ' isolated' : ''));
      row.type = 'button';
      row.setAttribute('aria-pressed', String(isolated));
      /* The pile-up is the question; the workers in it are the answer. One
         click puts them beside it rather than leaving the operator to filter
         the list by eye. */
      row.addEventListener('click', function () {
        state.flowStep = isolated ? null : stage.currentStep;
        state.flowShowAll = false;
        renderFlow();
      });

      var idx = el('span', 'idx', stage.totalSteps > 0
        ? stage.currentStep + '/' + stage.totalSteps
        : String(stage.currentStep));
      row.appendChild(idx);

      var meter = el('div', 'meter');
      var fill = el('i');
      fill.style.width = ((stage.count / max) * 100).toFixed(1) + '%';
      meter.appendChild(fill);

      var label = el('div', 'lbl');
      label.appendChild(el('span', 'type', stage.stepType || '—'));
      if (stage.detail) { label.appendChild(el('span', 'detail', stage.detail)); }
      meter.appendChild(label);

      var tip = fmtInt(stage.count) + ' worker' + (stage.count === 1 ? '' : 's') + ' on ' + stepLabel(stage);
      if (stage.slowest !== null) { tip += ' · longest ' + fmtSeconds(stage.slowest) + ' on this step'; }
      tip += isolated ? ' · click to show every worker again' : ' · click to show only these workers';
      row.setAttribute('data-tip', tip);

      row.appendChild(meter);
      row.appendChild(el('span', 'n', fmtInt(stage.count)));
      host.appendChild(row);
    });
  }

  function renderFlowWorkers(workers) {
    var host = $('#flowWorkers');
    var note = $('#flowWorkerNote');
    if (!host) { return; }
    host.innerHTML = '';

    flowTicking = [];

    var rows = sortFlowWorkers(filterFlowWorkers(workers));

    if (note) {
      note.textContent = rows.length === workers.length
        ? ''
        : fmtInt(rows.length) + ' of ' + fmtInt(workers.length);
    }

    if (!rows.length) {
      host.appendChild(el('div', 'flow-note', flowFilterActive()
        ? 'No worker matches this filter.'
        : (state.flowStalledOnly
          ? 'Nothing is stalled — every worker has moved on within ' + FLOW_SLOW_SECONDS + 's.'
          : 'No workers are reporting a step yet.')));
      return;
    }

    /* A 500-user run would put 500 rows on the page every poll, which is a
       list nobody scrolls and a lot of DOM to rebuild. The rows are already
       sorted worst-first, so the cut falls on the healthy end — and it is
       said out loud rather than left to be inferred from a short list. The
       fleet column beside this one still counts every worker. */
    var hidden = 0;
    if (rows.length > FLOW_MAX_ROWS && !state.flowShowAll) {
      hidden = rows.length - FLOW_MAX_ROWS;
      rows = rows.slice(0, FLOW_MAX_ROWS);
    }

    var multiplePids = {};
    workers.forEach(function (w) { multiplePids[w.pid] = true; });
    var showPid = Object.keys(multiplePids).length > 1;

    rows.forEach(function (worker) {
      var severity = flowSeverity(worker);
      var screens = workerHasScreens(worker);
      var row = el(screens ? 'button' : 'div',
        'worker-row' + (severity === 'stalled' ? ' stalled' : '') + (screens ? ' linked' : ''));
      if (screens) {
        row.type = 'button';
        /* The row says a worker has been on CheckValue for forty seconds.
           The next question is always the same — what is on its screen —
           and this is the shortest path to the answer. */
        row.addEventListener('click', function () {
          Captures.open(worker.pid, { port: worker.scriptPort });
        });
      }
      row.style.setProperty('--tone', FLOW_TONES[severity]);

      var who = el('div', 'who');
      who.appendChild(el('span', 'port', (showPid ? worker.pid + ' · ' : '') + (worker.scriptPort || '—')));
      who.appendChild(el('span', 'host', worker.host ? worker.host + ':' + worker.port : '—'));
      row.appendChild(who);

      var what = el('div', 'what');
      var line = el('div', 'line');
      line.appendChild(el('span', 'step-type', worker.stepType || '—'));
      if (worker.stepDetail) { line.appendChild(el('span', 'step-detail', worker.stepDetail)); }
      line.appendChild(el('span', 'step-count', worker.totalSteps > 0
        ? worker.currentStep + '/' + worker.totalSteps
        : 'step ' + worker.currentStep));
      what.appendChild(line);

      var track = el('div', 'track');
      var fill = el('i');
      fill.style.width = worker.totalSteps > 0
        ? clamp((worker.currentStep / worker.totalSteps) * 100, 0, 100).toFixed(1) + '%'
        : '0%';
      track.appendChild(fill);
      what.appendChild(track);
      row.appendChild(what);

      var when = el('div', 'when');
      when.appendChild(el('span', 'on-step', fmtOnStep(worker.onStep)));
      when.appendChild(el('span', 'total', worker.elapsed === null ? '' : fmtSeconds(worker.elapsed) + ' total'));
      row.appendChild(when);

      var tip = 'Script port ' + (worker.scriptPort || '—') + ' · pid ' + worker.pid;
      if (worker.onStep === null) {
        tip += ' · this run does not report time on step';
      } else {
        tip += ' · ' + fmtSeconds(worker.onStep) + ' on ' + (worker.stepType || 'this step');
      }
      if (screens) { tip += ' · click for this worker’s screens'; }
      row.setAttribute('data-tip', tip);
      host.appendChild(row);
      flowTicking.push({ node: row, worker: worker, onStepNode: when.firstChild, totalNode: when.lastChild });
    });

    if (hidden) {
      var more = el('div', 'flow-note');
      more.appendChild(el('span', null,
        fmtInt(hidden) + ' more worker' + (hidden === 1 ? '' : 's') +
        ' not shown — the list keeps the ' + FLOW_MAX_ROWS + ' longest on their current step. '));
      var all = el('button', 'flow-link', 'Show all ' + fmtInt(rows.length + hidden));
      all.type = 'button';
      all.addEventListener('click', function () { state.flowShowAll = true; renderFlow(); });
      more.appendChild(all);
      host.appendChild(more);
    } else if (state.flowShowAll && rows.length > FLOW_MAX_ROWS) {
      var fewer = el('div', 'flow-note');
      var collapse = el('button', 'flow-link', 'Show fewer');
      collapse.type = 'button';
      collapse.addEventListener('click', function () { state.flowShowAll = false; renderFlow(); });
      fewer.appendChild(collapse);
      host.appendChild(fewer);
    }
  }

  /* Does the run this worker belongs to write screens anywhere? Offering a
     drill-down into a capture file that was never configured would be a
     click that leads to an apology. */
  function workerHasScreens(worker) {
    var meta = metaByPid[worker.pid];
    return !!(meta && meta.outputPath);
  }

  function flowFilterActive() {
    return !!(state.flowQuery || state.flowStep !== null || state.flowStalledOnly);
  }

  function filterFlowWorkers(workers) {
    var query = state.flowQuery.trim().toLowerCase();
    return workers.filter(function (worker) {
      if (state.flowStalledOnly) {
        var severity = flowSeverity(worker);
        if (severity !== 'stalled' && severity !== 'slow') { return false; }
      }
      if (state.flowStep !== null && worker.currentStep !== state.flowStep) { return false; }
      if (query) {
        var haystack = [
          worker.scriptPort, worker.pid, worker.host, worker.port,
          worker.stepType, worker.stepDetail
        ].join(' ').toLowerCase();
        if (haystack.indexOf(query) === -1) { return false; }
      }
      return true;
    });
  }

  /* One line saying what the panel is currently hiding, and one click to
     stop hiding it. A filtered view that does not admit it is filtered is
     how an operator concludes the fleet is smaller than it is. */
  function renderFlowFilterNote(workers) {
    var host = $('#flowFilterNote');
    if (!host) { return; }
    host.innerHTML = '';

    var bits = [];
    if (state.flowStep !== null) {
      var stage = flowStages(workers).filter(function (s) { return s.currentStep === state.flowStep; })[0];
      bits.push('step ' + (stage ? stepLabel(stage) : state.flowStep));
    }
    if (state.flowQuery.trim()) { bits.push('“' + state.flowQuery.trim() + '”'); }
    if (state.flowStalledOnly) { bits.push('slow and stalled only'); }

    host.hidden = !bits.length;
    if (!bits.length) { return; }

    host.appendChild(el('span', 'f-label', 'Showing ' + bits.join(' · ')));
    var clear = el('button', 'f-clear', 'Show every worker');
    clear.type = 'button';
    clear.addEventListener('click', function () {
      state.flowStep = null;
      state.flowQuery = '';
      state.flowStalledOnly = false;
      Prefs.set('flowStalledOnly', false);
      var search = $('#flowSearch');
      if (search) { search.value = ''; }
      var searchClear = $('#flowSearchClear');
      if (searchClear) { searchClear.hidden = true; }
      var toggle = $('#flowStalledOnly');
      if (toggle) { toggle.checked = false; }
      renderFlow();
    });
    host.appendChild(clear);
  }

  /* Rows whose clocks are advanced every second between polls.

     Only the times and the tint are refreshed, never the order: re-sorting
     under the pointer would move the row an operator is reading. The next
     poll re-sorts, which is soon enough for a figure measured in seconds. */
  var flowTicking = [];
  var flowTickId = null;

  function tickFlow() {
    var panel = $('#flowPanel');
    if (!panel || panel.hidden || !flowTicking.length) { return; }
    if (document.hidden) { return; }

    flowTicking.forEach(function (entry) {
      var worker = entry.worker;
      if (worker.stepStartedAt <= 0) { return; }
      worker.onStep = sinceEpoch(worker.stepStartedAt);
      worker.elapsed = sinceEpoch(worker.startedAt);

      var severity = flowSeverity(worker);
      entry.node.style.setProperty('--tone', FLOW_TONES[severity]);
      entry.node.classList.toggle('stalled', severity === 'stalled');
      if (entry.onStepNode) { entry.onStepNode.textContent = fmtOnStep(worker.onStep); }
      if (entry.totalNode && worker.elapsed !== null) {
        entry.totalNode.textContent = fmtSeconds(worker.elapsed) + ' total';
      }
    });
  }

  function renderFlow() {
    var panel = $('#flowPanel');
    if (!panel) { return; }

    var workers = flowWorkers();
    /* Hidden rather than shown empty: the runs table below already says when
       nothing is running, and a blank panel above it only repeats that. */
    panel.hidden = workers.length === 0;
    if (!workers.length) { flowTicking = []; return; }

    var stages = flowStages(workers);
    renderFlowFilterNote(workers);
    var count = $('#flowCount');
    if (count) {
      var stalled = workers.filter(function (w) { return flowSeverity(w) === 'stalled'; }).length;
      count.textContent = fmtInt(workers.length) + ' worker' + (workers.length === 1 ? '' : 's') +
        ' across ' + fmtInt(stages.length) + ' step' + (stages.length === 1 ? '' : 's') +
        (stalled ? ' · ' + fmtInt(stalled) + ' stalled' : '');
    }

    renderFlowSummary(workers, stages);
    renderFlowStage(workers, stages);
    renderFlowWorkers(workers);
  }

  function bindFlow() {
    var sort = $('#flowSort');
    if (sort) {
      $$('button', sort).forEach(function (button) {
        button.setAttribute('aria-pressed', String(button.getAttribute('data-sort') === state.flowSort));
        button.addEventListener('click', function () {
          state.flowSort = button.getAttribute('data-sort');
          Prefs.set('flowSort', state.flowSort);
          $$('button', sort).forEach(function (other) {
            other.setAttribute('aria-pressed', String(other === button));
          });
          renderFlow();
        });
      });
    }

    var stalledOnly = $('#flowStalledOnly');
    if (stalledOnly) {
      stalledOnly.checked = state.flowStalledOnly;
      stalledOnly.addEventListener('change', function () {
        state.flowStalledOnly = stalledOnly.checked;
        Prefs.set('flowStalledOnly', state.flowStalledOnly);
        renderFlow();
      });
    }

    var search = $('#flowSearch');
    var searchClear = $('#flowSearchClear');
    if (search) {
      search.addEventListener('input', function () {
        state.flowQuery = search.value;
        if (searchClear) { searchClear.hidden = !search.value; }
        renderFlow();
      });
    }
    if (searchClear) {
      searchClear.addEventListener('click', function () {
        search.value = '';
        state.flowQuery = '';
        searchClear.hidden = true;
        renderFlow();
        search.focus();
      });
    }

    /* Independent of the refresh interval on purpose. At 30s or 60s polling
       the snapshot is stale but the clocks are not, and a frozen timer on a
       stalling worker is precisely the wrong thing to show. */
    if (flowTickId === null) { flowTickId = setInterval(tickFlow, 1000); }
  }

  /* ======================================================================
     10. Refresh engine
     ====================================================================== */

  var Refresh = (function () {
    var timerId = null;
    var tickId = null;
    var nextAt = 0;
    var pausedByModal = false;
    var openModals = 0;
    var inFlight = false;

    var RING_CIRCUM = 2 * Math.PI * 12;

    function periodMs() {
      return Math.max(1, num(Prefs.get('refreshPeriod'), 5)) * 1000;
    }

    function enabled() { return !!Prefs.get('autoRefresh'); }

    function start() {
      stop();
      if (!enabled() || pausedByModal) { paintRing(0); return; }
      nextAt = Date.now() + periodMs();
      timerId = setInterval(function () { fetchData(true); nextAt = Date.now() + periodMs(); }, periodMs());
      tickId = setInterval(paintCountdown, 200);
      paintCountdown();
    }

    function stop() {
      if (timerId) { clearInterval(timerId); timerId = null; }
      if (tickId) { clearInterval(tickId); tickId = null; }
    }

    function paintCountdown() {
      var remaining = clamp(nextAt - Date.now(), 0, periodMs());
      paintRing(1 - remaining / periodMs());
    }

    function paintRing(progress) {
      var bar = $('#refreshRingBar');
      if (!bar) { return; }
      var value = enabled() && !pausedByModal ? clamp(progress, 0, 1) : 0;
      bar.setAttribute('stroke-dasharray', RING_CIRCUM.toFixed(2));
      bar.setAttribute('stroke-dashoffset', (RING_CIRCUM * (1 - value)).toFixed(2));
    }

    function setStatus(text) {
      var node = $('#refreshStatus');
      if (node) { node.textContent = text; }
    }

    function setStamp(date) {
      var node = $('#refreshStamp');
      if (node) { node.textContent = 'Updated ' + fmtClock(date); }
    }

    function pulse() {
      var ring = $('#refreshRing');
      if (!ring) { return; }
      ring.classList.add('pulsing');
      setTimeout(function () { ring.classList.remove('pulsing'); }, 900);
    }

    function fetchData(silent) {
      if (inFlight) { return Promise.resolve(); }
      inFlight = true;
      if (!silent) { pulse(); setStatus('Refreshing…'); }

      return fetch('/dashboard/data', { cache: 'no-store' })
        .then(function (response) {
          if (!response.ok) { throw new Error('HTTP ' + response.status); }
          return response.json();
        })
        .then(function (payload) {
          state.failures = 0;
          document.body.classList.remove('is-offline');
          state.metrics = (payload && payload.extendedMetrics) || [];
          applySnapshot();
          setStamp(payload && payload.timestamp ? new Date(payload.timestamp * 1000) : new Date());
          setStatus(enabled() ? 'Live · every ' + Prefs.get('refreshPeriod') + 's' : 'Auto-refresh off');
        })
        .catch(function (error) {
          state.failures += 1;
          setStatus('Connection error');
          if (state.failures >= 2) { document.body.classList.add('is-offline'); }
          if (state.failures === 1) {
            Toast.push('bad', 'Refresh failed', 'Could not reach /dashboard/data — ' + error.message);
          }
        })
        .finally(function () { inFlight = false; });
    }

    function applySnapshot() {
      var agg = aggregate(state.metrics);
      pushHistorySample(agg, resourceSeries(state.metrics));
      renderKPIs();
      renderFlow();
      renderTable();
      renderLatency();
      Charts.update();
      Modals.syncPidOptions(state.metrics);
      state.booted = true;
    }

    function modalOpened() {
      openModals += 1;
      pausedByModal = true;
      stop();
      paintRing(0);
    }

    function modalClosed() {
      openModals = Math.max(0, openModals - 1);
      if (openModals === 0) {
        pausedByModal = false;
        start();
      }
    }

    return {
      start: start,
      stop: stop,
      now: function () { return fetchData(false); },
      applySnapshot: applySnapshot,
      modalOpened: modalOpened,
      modalClosed: modalClosed,
      setStatus: setStatus
    };
  })();

  /* ======================================================================
     11. Modals
     ====================================================================== */

  /* Minimal, accessible dialog manager: no framework, full control of the
     animation, focus trap, scroll lock and stacking order. */
  var Dialog = (function () {
    var stack = [];
    var FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), ' +
                    'textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])';

    function emit(node, type) {
      node.dispatchEvent(new CustomEvent(type, { bubbles: false }));
    }

    function open(id) {
      var node = document.getElementById(id);
      if (!node || node.classList.contains('open')) { return null; }

      emit(node, 'dialog:show');
      node.__restoreFocus = document.activeElement;
      node.classList.add('open');
      node.removeAttribute('aria-hidden');
      node.setAttribute('role', 'dialog');
      node.setAttribute('aria-modal', 'true');
      document.body.classList.add('modal-open');
      stack.push(node);

      var first = node.querySelector(FOCUSABLE);
      if (first) { setTimeout(function () { first.focus(); }, 40); }
      return node;
    }

    function close(id) {
      var node = typeof id === 'string' ? document.getElementById(id) : id;
      if (!node || !node.classList.contains('open')) { return; }

      node.classList.remove('open');
      node.setAttribute('aria-hidden', 'true');
      node.removeAttribute('aria-modal');
      stack = stack.filter(function (other) { return other !== node; });
      if (!stack.length) { document.body.classList.remove('modal-open'); }
      if (node.__restoreFocus && node.__restoreFocus.focus) { node.__restoreFocus.focus(); }
      emit(node, 'dialog:hidden');
    }

    function top() { return stack[stack.length - 1] || null; }

    function bind() {
      // Backdrop click and any [data-close] control dismiss the topmost dialog.
      document.addEventListener('click', function (event) {
        var node = event.target;
        if (node.classList && node.classList.contains('modal')) { close(node); return; }
        var closer = node.closest ? node.closest('[data-close]') : null;
        if (closer) {
          var owner = closer.closest('.modal');
          if (owner) { close(owner); }
        }
      });

      document.addEventListener('keydown', function (event) {
        var current = top();
        if (!current) { return; }

        if (event.key === 'Escape') {
          event.preventDefault();
          close(current);
          return;
        }

        if (event.key === 'Tab') {
          var items = $$(FOCUSABLE, current).filter(function (node) {
            return node.offsetParent !== null || node === document.activeElement;
          });
          if (!items.length) { return; }
          var first = items[0];
          var last = items[items.length - 1];
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
          }
        }
      });
    }

    return { open: open, close: close, bind: bind, top: top };
  })();

  var Modals = (function () {

    function show(id) { return Dialog.open(id); }
    function hide(id) { Dialog.close(id); }

    /* --- JSON syntax highlighting (no dependency) --- */
    function highlightJSON(text) {
      var pretty = text;
      try { pretty = JSON.stringify(JSON.parse(text), null, 2); } catch (err) { /* leave as-is */ }

      var html = esc(pretty).replace(
        /("(\\u[a-fA-F0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false)\b|\bnull\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
        function (match) {
          var cls = 'tok-num';
          if (/^"/.test(match)) {
            cls = /:$/.test(match) ? 'tok-key' : 'tok-str';
          } else if (/true|false/.test(match)) {
            cls = 'tok-bool';
          } else if (/null/.test(match)) {
            cls = 'tok-null';
          }
          return '<span class="' + cls + '">' + match + '</span>';
        }
      );

      return html.split('\n').map(function (line) {
        return '<span class="ln">' + (line || ' ') + '</span>';
      }).join('');
    }

    /* --- Workflow JSON --- */
    var workflowRaw = '';

    function workflow(pid) {
      var meta = metaByPid[pid] || {};
      $('#workflowPath').textContent = meta.configPath || 'Path unavailable';
      $('#workflowCode').textContent = 'Loading workflow…';
      workflowRaw = '';
      show('workflowModal');

      if (!meta.configPath) {
        $('#workflowCode').textContent = 'No configuration path recorded for PID ' + pid + '.';
        return;
      }

      fetch('/dashboard/workflow?pid=' + encodeURIComponent(pid), { cache: 'no-store' })
        .then(readOrThrow)
        .then(function (text) {
          workflowRaw = text;
          $('#workflowCode').innerHTML = highlightJSON(text);
        })
        .catch(function (error) {
          $('#workflowCode').textContent = 'Unable to load workflow: ' + error.message;
        });
    }

    function readOrThrow(response) {
      if (!response.ok) {
        return response.text().then(function (body) {
          throw new Error((body || response.statusText || 'Request failed').trim());
        });
      }
      return response.text();
    }

    /* --- Screen captures ---

       The viewer itself lives in section 11b; this is the door into it, so
       that a process row, the live flow panel and the command palette all
       open the same thing. */
    function output(pid, options) {
      Captures.open(pid, options);
    }

    /* --- Summary --- */
    var summaryRaw = '';

    function summary(pid) {
      $('#summaryPid').textContent = pid;
      $('#summaryCode').textContent = 'Loading summary…';
      summaryRaw = '';
      show('summaryModal');

      fetch('/dashboard/summary?pid=' + encodeURIComponent(pid), { cache: 'no-store' })
        .then(readOrThrow)
        .then(function (text) {
          summaryRaw = text;
          $('#summaryCode').textContent = text;
        })
        .catch(function (error) {
          $('#summaryCode').textContent = 'Unable to load summary: ' + error.message;
        });
    }

    /* --- Console logs --- */
    var logEntries = [];
    var logTimer = null;
    var logFollow = true;

    function levelOf(message) {
      var text = String(message || '').toLowerCase();
      if (/\b(error|fail|failed|fatal|panic|refused|timeout)\b/.test(text)) { return 'error'; }
      if (/\b(warn|warning|retry|retrying|skip)\b/.test(text)) { return 'warn'; }
      if (/\b(success|completed|connected|done|ok)\b/.test(text)) { return 'ok'; }
      return '';
    }

    function loadLogs() {
      var pid = $('#logPidFilter') ? $('#logPidFilter').value : '';
      var url = '/console' + (pid ? '?pid=' + encodeURIComponent(pid) : '');

      return fetch(url, { cache: 'no-store' })
        .then(function (response) { return response.json(); })
        .then(function (data) {
          logEntries = Array.isArray(data) ? data : [];
          renderLogs();
        })
        .catch(function (error) {
          var host = $('#logStream');
          if (host) {
            host.innerHTML = '<div class="empty"><div class="t">Log stream unavailable</div><div class="d">' + esc(error.message) + '</div></div>';
          }
        });
    }

    function renderLogs() {
      var host = $('#logStream');
      if (!host) { return; }

      var query = ($('#logSearch') ? $('#logSearch').value : '').trim().toLowerCase();
      var level = $('#logLevel') ? $('#logLevel').value : 'all';

      var rows = logEntries.filter(function (entry) {
        var message = String(entry.log || '');
        if (level !== 'all' && levelOf(message) !== level) { return false; }
        if (!query) { return true; }
        return (message + ' ' + entry.pid + ' ' + (entry.parameters || '')).toLowerCase().indexOf(query) !== -1;
      });

      // Newest-first from the server; show oldest-first so tailing reads naturally.
      rows = rows.slice(0, 2000).reverse();

      var counter = $('#logCount');
      if (counter) {
        counter.textContent = fmtInt(rows.length) + ' / ' + fmtInt(logEntries.length) + ' lines';
      }

      if (!rows.length) {
        host.innerHTML = '<div class="empty"><div class="glyph"><svg class="ic" aria-hidden="true"><use href="#i-terminal"></use></svg></div>' +
          '<div class="t">No log lines</div><div class="d">Nothing matches the current PID, level or search filter.</div></div>';
        return;
      }

      var html = rows.map(function (entry) {
        var message = String(entry.log || '');
        var level = levelOf(message);
        var stamp = new Date(entry.timestamp);
        var time = isNaN(stamp.getTime()) ? '--:--:--' : fmtClock(stamp);
        var body = esc(message);
        if (query) {
          var safe = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
          body = body.replace(new RegExp('(' + safe + ')', 'ig'), '<mark>$1</mark>');
        }
        return '<div class="log-line' + (level ? ' lvl-' + level : '') + '">' +
          '<span class="ts">' + time + '</span>' +
          '<span class="pid">' + esc(entry.pid) + '</span>' +
          '<span class="msg">' + body + '</span></div>';
      }).join('');

      host.innerHTML = html;
      if (logFollow) { host.scrollTop = host.scrollHeight; }

      renderPidStrip();
    }

    function renderPidStrip() {
      var host = $('#logPidStrip');
      if (!host) { return; }
      var byPid = {};
      logEntries.forEach(function (entry) {
        if (!byPid[entry.pid]) { byPid[entry.pid] = {}; }
        if (entry.parameters) { byPid[entry.pid][entry.parameters] = true; }
      });

      host.innerHTML = '';
      Object.keys(byPid).sort(function (a, b) { return num(a) - num(b); }).forEach(function (pid) {
        var params = Object.keys(byPid[pid]).join(' · ') || '-';
        var chip = el('span', 'chip accent');
        chip.appendChild(document.createTextNode('PID ' + pid));
        var sep = el('span', null, '·');
        sep.style.opacity = '0.5';
        chip.appendChild(sep);
        var text = el('span', null, params);
        text.style.color = 'var(--text-2)';
        text.style.maxWidth = '340px';
        text.style.overflow = 'hidden';
        text.style.textOverflow = 'ellipsis';
        chip.appendChild(text);
        chip.setAttribute('data-tip', params);
        host.appendChild(chip);
      });
    }

    function startLogAutoRefresh() {
      stopLogAutoRefresh();
      var toggle = $('#logAutoRefresh');
      var interval = $('#logInterval');
      if (toggle && toggle.checked) {
        logTimer = setInterval(loadLogs, Math.max(1, num(interval ? interval.value : 5, 5)) * 1000);
      }
    }

    function stopLogAutoRefresh() {
      if (logTimer) { clearInterval(logTimer); logTimer = null; }
    }

    function logsFor(pid) {
      var select = $('#logPidFilter');
      if (select) {
        syncPidOptions(state.metrics);
        select.value = String(pid);
      }
      show('consoleModal');
      loadLogs();
    }

    function syncPidOptions(metrics) {
      var select = $('#logPidFilter');
      if (!select) { return; }
      var current = select.value;
      var pids = [];
      (metrics || []).forEach(function (m) {
        if (m.pid !== null && m.pid !== undefined && pids.indexOf(m.pid) === -1) { pids.push(m.pid); }
      });
      pids.sort(function (a, b) { return num(a) - num(b); });

      select.innerHTML = '';
      var all = el('option', null, 'All PIDs');
      all.value = '';
      select.appendChild(all);
      pids.forEach(function (pid) {
        var option = el('option', null, String(pid));
        option.value = String(pid);
        select.appendChild(option);
      });
      select.value = pids.map(String).indexOf(String(current)) !== -1 ? current : '';
    }

    /* --- Kill confirmation --- */
    var killPid = null;

    function confirmKill(pid) {
      killPid = pid;
      var meta = metaByPid[pid] || {};
      $('#killPid').textContent = pid;
      $('#killParams').textContent = meta.params || '-dashboard';
      show('killModal');
    }

    function doKill() {
      if (!killPid) { return; }
      var pid = killPid;
      hide('killModal');

      fetch('/kill?pid=' + encodeURIComponent(pid), { method: 'POST' })
        .then(function (response) {
          return response.text().then(function (text) {
            if (response.ok) {
              Toast.push('ok', 'Process terminated', text || ('PID ' + pid + ' was signalled.'));
            } else {
              Toast.push('bad', 'Kill failed', text || 'The process could not be terminated.');
            }
          });
        })
        .catch(function (error) {
          Toast.push('bad', 'Kill failed', error.message);
        })
        .finally(function () {
          setTimeout(function () { Refresh.now(); }, 900);
        });
    }

    /* --- Start process wizard --- */
    var parsedConfig = null;

    function readFileText(file) {
      return new Promise(function (resolve, reject) {
        var reader = new FileReader();
        reader.onload = function (event) { resolve(String(event.target.result || '')); };
        reader.onerror = function () { reject(new Error('Unable to read ' + file.name)); };
        reader.readAsText(file);
      });
    }

    function resetConfigPanel() {
      parsedConfig = null;
      $('#configDetails').hidden = true;
      $('#configMeta').innerHTML = '';
      $('#configCode').textContent = '';
      $('#overrideGroup').hidden = true;
      $('#connectionResult').hidden = true;
      ['overrideHost', 'overridePort', 'overrideOutputFilePath', 'overrideRampUpBatchSize', 'overrideRampUpDelay'].forEach(function (id) {
        var input = document.getElementById(id);
        if (input) { input.value = ''; }
      });
      var zone = $('#configDrop');
      if (zone) { zone.classList.remove('filled'); }
      $('#configFileName').textContent = '';
      $('#configFileName').hidden = true;
      paintSteps(0);
    }

    function paintSteps(done) {
      $$('#startSteps .step').forEach(function (step, index) {
        step.classList.toggle('done', index < done);
      });
    }

    function metaRow(list, label, value) {
      var dt = el('dt', null, label);
      var dd = el('dd', null, value === undefined || value === null || value === '' ? '—' : String(value));
      list.appendChild(dt);
      list.appendChild(dd);
    }

    function handleConfigFile(file) {
      if (!file) { return; }
      readFileText(file).then(function (text) {
        $('#configDetails').hidden = false;
        $('#configFileName').hidden = false;
        $('#configFileName').textContent = file.name;
        var zone = $('#configDrop');
        if (zone) { zone.classList.add('filled'); }

        try {
          parsedConfig = JSON.parse(text);
        } catch (error) {
          parsedConfig = null;
          $('#configMeta').innerHTML = '';
          $('#configCode').textContent = text;
          $('#overrideGroup').hidden = true;
          Toast.push('warn', 'Invalid JSON', 'The file was uploaded but could not be parsed: ' + error.message);
          paintSteps(1);
          return;
        }

        $('#configCode').innerHTML = highlightJSON(text);

        var list = $('#configMeta');
        list.innerHTML = '';
        metaRow(list, 'Host', parsedConfig.Host);
        metaRow(list, 'Port', parsedConfig.Port);
        metaRow(list, 'Code page', parsedConfig.CodePage);
        metaRow(list, 'Output file', parsedConfig.OutputFilePath);
        metaRow(list, 'Steps', Array.isArray(parsedConfig.Steps) ? parsedConfig.Steps.length : (parsedConfig.Workflow && Array.isArray(parsedConfig.Workflow.Steps) ? parsedConfig.Workflow.Steps.length : '—'));
        metaRow(list, 'Ramp-up batch', parsedConfig.RampUpBatchSize);
        metaRow(list, 'Ramp-up delay', parsedConfig.RampUpDelay);

        $('#overrideGroup').hidden = false;
        setValue('overrideHost', parsedConfig.Host);
        setValue('overridePort', parsedConfig.Port);
        setValue('overrideOutputFilePath', parsedConfig.OutputFilePath);
        setValue('overrideRampUpBatchSize', parsedConfig.RampUpBatchSize);
        setValue('overrideRampUpDelay', parsedConfig.RampUpDelay);
        paintSteps(2);
        Toast.push('ok', 'Configuration loaded', file.name + ' parsed successfully.');
      }).catch(function (error) {
        Toast.push('bad', 'Read failed', error.message);
      });
    }

    function setValue(id, value) {
      var input = document.getElementById(id);
      if (input) { input.value = value === undefined || value === null ? '' : String(value); }
    }

    function testConnection() {
      var host = ($('#overrideHost').value || '').trim() || (parsedConfig && parsedConfig.Host ? String(parsedConfig.Host) : '');
      var portRaw = ($('#overridePort').value || '').trim() || (parsedConfig && parsedConfig.Port !== undefined ? String(parsedConfig.Port) : '');
      var result = $('#connectionResult');

      function fail(message) {
        result.hidden = false;
        result.className = 'banner bad';
        result.innerHTML = '<svg class="ic" aria-hidden="true"><use href="#i-triangle-exclamation"></use></svg><span>' + esc(message) + '</span>';
      }

      if (!host) { fail('A host is required. Load a configuration file or enter an override.'); return; }
      var port = Number(portRaw);
      if (!Number.isInteger(port) || port <= 0) { fail('A positive integer port is required.'); return; }

      var button = $('#testConnection');
      button.disabled = true;
      var original = button.innerHTML;
      button.innerHTML = '<svg class="ic spin" aria-hidden="true"><use href="#i-circle-notch"></use></svg><span class="txt">Testing</span>';
      result.hidden = false;
      result.className = 'banner';
      result.innerHTML = '<svg class="ic spin" aria-hidden="true"><use href="#i-circle-notch"></use></svg><span>Contacting ' + esc(host) + ':' + port + '…</span>';

      fetch('/test-connection', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host: host, port: port })
      })
        .then(function (response) {
          return response.json().catch(function () { return {}; }).then(function (data) {
            if (!response.ok) { throw new Error(data.message || 'Connection refused.'); }
            return data;
          });
        })
        .then(function (data) {
          result.className = 'banner ok';
          result.innerHTML = '<svg class="ic" aria-hidden="true"><use href="#i-circle-check"></use></svg><span>' + esc(data.message || ('Reached ' + host + ':' + port)) + '</span>';
          paintSteps(3);
        })
        .catch(function (error) { fail(error.message); })
        .finally(function () {
          button.disabled = false;
          button.innerHTML = original;
        });
    }

    function startProcess() {
      var form = $('#startProcessForm');
      var fileInput = $('#configFile');

      if (!fileInput.files || !fileInput.files.length) {
        Toast.push('bad', 'Configuration required', 'Choose a workflow JSON file before starting.');
        return;
      }
      if (!form.checkValidity()) {
        form.reportValidity();
        return;
      }

      var data = new FormData();
      data.append('configFile', fileInput.files[0]);

      var injection = $('#injectionFile');
      if (injection.files && injection.files.length) { data.append('injectionConfig', injection.files[0]); }

      data.append('concurrent', $('#concurrent').value);
      data.append('runtime', $('#runtime').value);
      data.append('startPort', $('#startPort').value);
      data.append('headless', $('#headless').checked ? 'on' : 'off');

      var token = ($('#token').value || '').trim();
      if (token) { data.append('token', token); }

      [['overrideHost', 'overrideHost'], ['overridePort', 'overridePort'],
       ['overrideOutputFilePath', 'overrideOutputFilePath'],
       ['overrideRampUpBatchSize', 'overrideRampUpBatchSize'],
       ['overrideRampUpDelay', 'overrideRampUpDelay']].forEach(function (pair) {
        var input = document.getElementById(pair[0]);
        var value = input && input.value ? input.value.trim() : '';
        if (value) { data.append(pair[1], value); }
      });

      var button = $('#startProcessSubmit');
      button.disabled = true;
      var original = button.innerHTML;
      button.innerHTML = '<svg class="ic spin" aria-hidden="true"><use href="#i-circle-notch"></use></svg><span class="txt">Starting</span>';

      fetch('/start-process', { method: 'POST', body: data })
        .then(function (response) {
          return response.text().then(function (text) {
            if (!response.ok) { throw new Error(text || 'The server rejected the request.'); }
            return text;
          });
        })
        .then(function () {
          Toast.push('ok', 'Process started', 'The run is spinning up — metrics will appear shortly.');
          hide('startProcessModal');
          setTimeout(function () { Refresh.now(); }, 1200);
          setTimeout(function () { Refresh.now(); }, 4000);
        })
        .catch(function (error) {
          Toast.push('bad', 'Start failed', error.message);
        })
        .finally(function () {
          button.disabled = false;
          button.innerHTML = original;
        });
    }

    function startApp() {
      var selected = $('#appPicker [aria-pressed="true"]');
      if (!selected) {
        Toast.push('bad', 'Choose an app', 'Select one of the bundled sample applications first.');
        return;
      }
      var data = new FormData();
      data.append('runApp', selected.getAttribute('data-app'));
      data.append('runAppPort', $('#appPort').value);

      var button = $('#startAppSubmit');
      button.disabled = true;

      fetch('/start-process', { method: 'POST', body: data })
        .then(function (response) {
          return response.text().then(function (text) {
            if (!response.ok) { throw new Error(text || 'The server rejected the request.'); }
            return text;
          });
        })
        .then(function () {
          Toast.push('ok', 'Sample app started', 'Listening on port ' + $('#appPort').value + '.');
          hide('startAppModal');
          setTimeout(function () { Refresh.now(); }, 1200);
        })
        .catch(function (error) { Toast.push('bad', 'Start failed', error.message); })
        .finally(function () { button.disabled = false; });
    }

    /* --- wiring --- */
    function bind() {
      // Pause polling while a modal is open so the UI never shifts underfoot.
      $$('.modal').forEach(function (node) {
        node.addEventListener('dialog:show', function () { Refresh.modalOpened(); });
        node.addEventListener('dialog:hidden', function () { Refresh.modalClosed(); });
      });

      var consoleModal = $('#consoleModal');
      if (consoleModal) {
        consoleModal.addEventListener('dialog:show', function () { syncPidOptions(state.metrics); loadLogs(); });
        consoleModal.addEventListener('dialog:hidden', stopLogAutoRefresh);
      }

      on('#logPidFilter', 'change', loadLogs);
      on('#logSearch', 'input', renderLogs);
      on('#logLevel', 'change', renderLogs);
      on('#logRefresh', 'click', function () { loadLogs(); Toast.push('info', 'Reloaded', 'Log stream refreshed.'); });
      on('#logAutoRefresh', 'change', startLogAutoRefresh);
      on('#logInterval', 'change', startLogAutoRefresh);
      on('#logFollow', 'click', function () {
        logFollow = !logFollow;
        this.setAttribute('aria-pressed', String(logFollow));
        if (logFollow) { var host = $('#logStream'); host.scrollTop = host.scrollHeight; }
      });
      on('#logWrap', 'click', function () {
        var next = Prefs.get('wrap') === 'on' ? 'off' : 'on';
        Prefs.set('wrap', next);
        Prefs.apply();
        this.setAttribute('aria-pressed', String(next === 'on'));
      });
      on('#logCopy', 'click', function () {
        copyText(logEntries.map(function (e) { return e.timestamp + ' [' + e.pid + '] ' + e.log; }).reverse().join('\n'), 'Log stream');
      });
      on('#logDownload', 'click', function () {
        var text = logEntries.slice().reverse().map(function (e) { return e.timestamp + ' [' + e.pid + '] ' + e.log; }).join('\n');
        download('3270connect-logs-' + Date.now() + '.log', text);
        Toast.push('ok', 'Downloaded', 'Log stream saved.');
      });
      on('#consoleMaximize', 'click', function () {
        var dialog = $('#consoleModalDialog');
        var maximized = dialog.classList.toggle('maximized');
        var label = maximized ? 'Restore console' : 'Maximise console';
        this.setAttribute('aria-pressed', String(maximized));
        this.setAttribute('aria-label', label);
        this.setAttribute('data-tip', label);
        this.innerHTML = icon(maximized ? 'compress' : 'expand');
      });

      on('#workflowCopy', 'click', function () { copyText(workflowRaw || $('#workflowCode').textContent, 'Workflow JSON'); });
      on('#workflowDownload', 'click', function () {
        download('workflow-' + Date.now() + '.json', workflowRaw || $('#workflowCode').textContent, 'application/json');
      });

      on('#summaryCopy', 'click', function () { copyText(summaryRaw || $('#summaryCode').textContent, 'Summary'); });
      on('#summaryDownload', 'click', function () {
        download('summary-' + Date.now() + '.txt', summaryRaw || $('#summaryCode').textContent);
      });

      on('#confirmKill', 'click', doKill);

      // Start-process modal
      var fileInput = $('#configFile');
      if (fileInput) {
        fileInput.addEventListener('change', function () {
          if (this.files && this.files.length) { handleConfigFile(this.files[0]); } else { resetConfigPanel(); }
        });
      }
      var zone = $('#configDrop');
      if (zone) {
        ['dragenter', 'dragover'].forEach(function (type) {
          zone.addEventListener(type, function (event) { event.preventDefault(); zone.classList.add('dragging'); });
        });
        ['dragleave', 'drop'].forEach(function (type) {
          zone.addEventListener(type, function (event) { event.preventDefault(); zone.classList.remove('dragging'); });
        });
        zone.addEventListener('drop', function (event) {
          var files = event.dataTransfer && event.dataTransfer.files;
          if (files && files.length) {
            fileInput.files = files;
            handleConfigFile(files[0]);
          }
        });
      }
      on('#testConnection', 'click', testConnection);
      on('#startProcessSubmit', 'click', startProcess);
      var startModal = $('#startProcessModal');
      if (startModal) {
        startModal.addEventListener('dialog:show', function () { paintSteps(parsedConfig ? 2 : 0); });
      }

      // Start-app modal
      $$('#appPicker .app-option').forEach(function (option) {
        option.addEventListener('click', function () {
          $$('#appPicker .app-option').forEach(function (other) { other.setAttribute('aria-pressed', 'false'); });
          option.setAttribute('aria-pressed', 'true');
        });
      });
      on('#startAppSubmit', 'click', startApp);
    }

    function on(selector, event, handler) {
      var node = $(selector);
      if (node) { node.addEventListener(event, handler); }
    }

    return {
      bind: bind,
      show: show,
      hide: hide,
      workflow: workflow,
      output: output,
      summary: summary,
      logsFor: logsFor,
      confirmKill: confirmKill,
      syncPidOptions: syncPidOptions,
      loadLogs: loadLogs
    };
  })();

  /* ======================================================================
     11b. Screen captures

     A run appends every worker's screens to one file, one after another, so
     the file is the whole fleet's terminal history in a single column. Shown
     raw — which is what this used to do — finding the screen that mattered
     meant scrolling past hundreds of near-identical ones with nothing to say
     which worker or which step any of them came from.

     So the file is read as what it is: a list of captures. Each one carries
     the worker, the step and the moment it was taken (see captureAttrs in
     the emulator), which is enough to narrow the strip to one virtual user,
     one step or one phrase, and to tint what changed between one screen and
     that worker's previous one.

     The screen itself is rebuilt as a screen — 24 rows, 80 columns, a ruler
     and the row/column under the pointer. Those coordinates are the reason
     an operator opens a capture at all: workflow steps are written in
     1-based Row/Column/Length, and counting characters off a screenshot is
     how they get written wrong.

     Older capture files carry no attributes. They still parse — the screens
     are all there — they simply cannot say whose they are, and the filters
     that need that metadata stay out of the way rather than showing empty.
     ====================================================================== */

  var Captures = (function () {
    /* A long soak run captures without end; the strip keeps a rolling window
       and says so rather than growing until the tab dies. */
    var MAX_CAPTURES = 2000;
    var MIN_COLS = 80;
    var PREVIEW_CHARS = 46;

    var PRE_RE = /<pre\b([^>]*)>([\s\S]*?)<\/pre>/gi;
    var ATTR_RE = /([a-zA-Z-]+)\s*=\s*"([^"]*)"/g;

    var cs = {
      pid: null,
      path: '',
      isOpen: false,
      /* Unparsed tail of the file. Everything already turned into a capture
         is dropped, so this stays the size of one screen no matter how long
         the run goes on. */
      raw: '',
      offset: 0,          // bytes of the file consumed, for incremental polls
      dropped: 0,         // captures aged out of the rolling window
      counter: 0,         // captures seen, for files that carry no sequence
      all: [],
      view: [],           // filtered, with runs of identical screens collapsed
      index: -1,
      query: '',
      worker: '',
      step: '',
      follow: true,
      diff: true,
      interval: 4000,
      timer: null,
      cols: MIN_COLS,
      selection: null,    // { row, from, to } — 1-based, inclusive
      dragging: false
    };

    /* ---------- parsing ---------- */

    function decodeAttr(value) {
      return String(value)
        .replace(/&quot;/g, '"')
        .replace(/&#39;/g, "'")
        .replace(/&lt;/g, '<')
        .replace(/&gt;/g, '>')
        .replace(/&amp;/g, '&');
    }

    function readAttrs(chunk) {
      var map = {};
      var match;
      ATTR_RE.lastIndex = 0;
      while ((match = ATTR_RE.exec(chunk)) !== null) {
        map[match[1].toLowerCase()] = decodeAttr(match[2]);
      }
      return map;
    }

    /* s3270 answers Ascii() in its script protocol: every screen row comes
       back behind a "data: " prefix, and the last line is the emulator's
       status rather than anything the host painted. Both are stripped here,
       and stripping them is not cosmetic — six characters of prefix is six
       columns of error in every coordinate read off this screen, which is
       the one number the panel exists to get right. */
    var STATUS_RE = /^[A-Z?] [A-Z?] [A-Z?] C\(/;

    function splitScreen(body) {
      var text = String(body).replace(/^\r?\n/, '').replace(/\s+$/, '');
      var lines = [];
      var status = null;

      text.split('\n').forEach(function (raw) {
        var line = raw.replace(/\r$/, '');
        if (line.indexOf('data: ') === 0) { lines.push(line.slice(6)); return; }
        if (line === 'data:') { lines.push(''); return; }
        if (!status && STATUS_RE.test(line)) { status = parseStatus(line); return; }
        if (line.trim() === 'ok') { return; }
        /* Anything else is taken at face value: a capture file written by
           hand, or by a future emulator, is still a screen. */
        lines.push(line);
      });

      return { lines: lines, status: status };
    }

    /* The s3270 status line, which carries the one thing the screen text
       cannot: where the cursor was left sitting. Its row and column are
       0-based; everything this panel shows an operator is 1-based, because
       that is what a workflow step is written in. */
    function parseStatus(line) {
      var fields = line.split(/\s+/);
      if (fields.length < 10) { return null; }
      var row = num(fields[8], -1);
      var col = num(fields[9], -1);
      return {
        rows: num(fields[6], 0),
        cols: num(fields[7], 0),
        cursorRow: row >= 0 ? row + 1 : 0,
        cursorCol: col >= 0 ? col + 1 : 0,
        keyboardLocked: fields[0] === 'L',
        connected: fields[3] !== 'N'
      };
    }

    /* The screen text inside <pre> is written unescaped, so it is taken as
       it stands — decoding it would corrupt an ampersand a host really
       painted. */
    function toCapture(attrChunk, body) {
      var attrs = readAttrs(attrChunk);
      var screen = splitScreen(body);
      var text = screen.lines.join('\n');
      return {
        seq: num(attrs['data-capture'], 0),
        at: num(attrs['data-at'], 0),
        port: attrs['data-port'] || '',
        host: attrs['data-host'] || '',
        hostPort: num(attrs['data-hostport'], 0),
        step: num(attrs['data-step'], 0),
        steps: num(attrs['data-steps'], 0),
        type: attrs['data-type'] || '',
        lines: screen.lines,
        status: screen.status,
        text: text,
        haystack: text.toLowerCase()
      };
    }

    /* Only complete <pre>…</pre> blocks become captures: the tail of the
       file is very often a screen a worker is still writing. */
    function ingest(chunk) {
      cs.raw += chunk;
      PRE_RE.lastIndex = 0;
      var match;
      var consumed = 0;
      var added = 0;
      while ((match = PRE_RE.exec(cs.raw)) !== null) {
        var capture = toCapture(match[1], match[2]);
        cs.counter += 1;
        /* The run numbers its own captures; only a file written before it
           did needs the console to count them. */
        capture.ordinal = capture.seq || cs.counter;
        cs.all.push(capture);
        consumed = PRE_RE.lastIndex;
        added += 1;
      }
      if (consumed > 0) { cs.raw = cs.raw.slice(consumed); }
      if (cs.all.length > MAX_CAPTURES) {
        var excess = cs.all.length - MAX_CAPTURES;
        cs.all = cs.all.slice(excess);
        cs.dropped += excess;
      }
      return added;
    }

    /* ---------- loading ---------- */

    function load(fromScratch) {
      if (!cs.pid || !cs.isOpen) { return; }
      var url = '/dashboard/output?pid=' + encodeURIComponent(cs.pid);
      if (!fromScratch && cs.offset > 0) { url += '&from=' + cs.offset; }

      fetch(url, { cache: 'no-store' })
        .then(function (response) {
          if (!response.ok) {
            return response.text().then(function (body) {
              throw new Error((body || response.statusText || 'Request failed').trim());
            });
          }
          var reset = response.headers.get('X-Output-Reset') === '1';
          var from = num(response.headers.get('X-Output-From'), 0);
          return response.arrayBuffer().then(function (buffer) {
            return { reset: reset, from: from, buffer: buffer };
          });
        })
        .then(function (payload) {
          /* Offsets are counted in bytes, as the server counts them: the
             decoded string is a different length the moment a host paints a
             character outside ASCII. */
          if (payload.reset || payload.from === 0) { clearCaptures(); }
          cs.offset = payload.from + payload.buffer.byteLength;
          var text = new TextDecoder('utf-8').decode(payload.buffer);
          var added = ingest(text);
          setStatus(cs.all.length
            ? fmtInt(cs.all.length + cs.dropped) + ' captured · updated ' + fmtClock(new Date())
            : 'No screens captured yet · checked ' + fmtClock(new Date()));
          if (added || payload.reset || payload.from === 0) { rebuild(); }
        })
        .catch(function (error) {
          setStatus('Error: ' + error.message, true);
        });
    }

    function clearCaptures() {
      cs.raw = '';
      cs.all = [];
      cs.view = [];
      cs.dropped = 0;
      cs.counter = 0;
      cs.index = -1;
      cs.selection = null;
    }

    function schedule() {
      if (cs.timer) { clearInterval(cs.timer); cs.timer = null; }
      if (cs.isOpen) { cs.timer = setInterval(function () { load(false); }, cs.interval); }
    }

    /* ---------- filtering ---------- */

    function matches(capture) {
      if (cs.worker && capture.port !== cs.worker) { return false; }
      if (cs.step && String(capture.step) !== cs.step) { return false; }
      if (cs.query && capture.haystack.indexOf(cs.query) === -1) { return false; }
      return true;
    }

    /* Consecutive identical screens from the same worker are one entry with
       a count. A workflow that grabs a screen either side of a step that
       changed nothing produces those by the dozen, and they are noise in a
       strip you are scanning for the screen that looks wrong. */
    function collapse(list) {
      var out = [];
      list.forEach(function (capture) {
        var last = out[out.length - 1];
        if (last && last.capture.port === capture.port && last.capture.text === capture.text) {
          last.repeats += 1;
          last.capture = capture;
          return;
        }
        out.push({ capture: capture, repeats: 1 });
      });
      return out;
    }

    function rebuild() {
      var previousSeq = cs.index >= 0 && cs.view[cs.index] ? cs.view[cs.index].capture.seq : null;

      cs.view = collapse(cs.all.filter(matches));
      syncFilterOptions();
      renderList();

      if (cs.follow) {
        select(cs.view.length - 1, true);
      } else if (previousSeq !== null) {
        var found = -1;
        cs.view.forEach(function (entry, i) { if (entry.capture.seq === previousSeq) { found = i; } });
        select(found >= 0 ? found : Math.min(cs.index, cs.view.length - 1), true);
      } else {
        select(cs.view.length - 1, true);
      }
      renderCount();
    }

    function syncFilterOptions() {
      var ports = {};
      var steps = {};
      cs.all.forEach(function (capture, i) {
        if (capture.port) { ports[capture.port] = i; }
        if (capture.step) { steps[capture.step] = capture.type || ''; }
      });

      /* Most recently active first. A script port belongs to one workflow
         execution, not to a virtual user for the life of the run, so a soak
         test leaves hundreds behind — and the one worth looking at is
         invariably the one that captured a screen most recently. */
      var portList = Object.keys(ports).sort(function (a, b) { return ports[b] - ports[a]; });
      /* A worker whose screens have aged out of the window is still the
         worker the operator asked for. Dropping it from the list would leave
         the control saying "all workers" over a list showing none. */
      if (cs.worker && portList.indexOf(cs.worker) === -1) { portList.unshift(cs.worker); }
      fillSelect('#capturesWorker', 'All workers', portList.map(function (port) {
        return { value: port, label: 'Worker ' + port };
      }), cs.worker);

      var stepList = Object.keys(steps).sort(function (a, b) { return num(a) - num(b); });
      if (cs.step && stepList.indexOf(cs.step) === -1) { stepList.push(cs.step); }
      fillSelect('#capturesStepFilter', 'All steps', stepList.map(function (step) {
        return { value: step, label: 'Step ' + step + (steps[step] ? ' · ' + steps[step] : '') };
      }), cs.step);

      /* Nothing to choose between is not a choice: a single-worker run, or a
         file written before captures carried a worker at all, hides the
         control rather than offering one empty option. */
      var selects = $('#capturesModal .rail-selects');
      if (selects) {
        var showWorker = portList.length > 1 || !!cs.worker;
        var showStep = stepList.length > 1 || !!cs.step;
        $('#capturesWorker').hidden = !showWorker;
        $('#capturesStepFilter').hidden = !showStep;
        selects.hidden = !showWorker && !showStep;
      }
    }

    function fillSelect(selector, allLabel, options, current) {
      var node = $(selector);
      if (!node) { return; }
      var wanted = current || '';
      var signature = allLabel + '|' + options.map(function (o) { return o.value + ':' + o.label; }).join(',');
      if (node.getAttribute('data-signature') !== signature) {
        node.innerHTML = '';
        node.appendChild(new Option(allLabel, ''));
        options.forEach(function (option) { node.appendChild(new Option(option.label, option.value)); });
        node.setAttribute('data-signature', signature);
      }
      node.value = wanted;
      if (node.value !== wanted) { node.value = ''; }
    }

    /* ---------- the strip ---------- */

    function shorten(line) {
      return line.length > PREVIEW_CHARS ? line.slice(0, PREVIEW_CHARS - 1) + '…' : line;
    }

    /* What to say about a screen in one line. The first line of a 3270 screen
       is nearly always the application's title, which is the same on every
       screen it paints — a strip of forty rows all reading "3270 Example
       Application" tells an operator nothing. So the line that changed is
       preferred, and the title is the fallback rather than the answer. */
    function previewLine(capture) {
      var previous = previousFor(capture);
      if (previous) {
        for (var i = 0; i < capture.lines.length; i++) {
          var line = capture.lines[i].trim();
          if (line && line !== String(previous.lines[i] || '').trim()) { return shorten(line); }
        }
      }
      for (var j = 0; j < capture.lines.length; j++) {
        var first = capture.lines[j].trim();
        if (first) { return shorten(first); }
      }
      return 'blank screen';
    }

    function captureClock(capture) {
      return capture.at > 0 ? fmtClock(new Date(capture.at)) : '';
    }

    function renderList() {
      var host = $('#capturesList');
      if (!host) { return; }
      host.innerHTML = '';

      if (!cs.view.length) {
        var note = el('div', 'rail-empty', cs.all.length
          ? 'No screen matches this filter.'
          : 'No screens captured yet.');
        host.appendChild(note);
        return;
      }

      if (cs.dropped) {
        host.appendChild(el('div', 'rail-note',
          fmtInt(cs.dropped) + ' earlier screen' + (cs.dropped === 1 ? '' : 's') +
          ' aged out — the strip keeps the most recent ' + fmtInt(MAX_CAPTURES) + '.'));
      }

      cs.view.forEach(function (entry, i) {
        var capture = entry.capture;
        var row = el('button', 'strip-item' + (i === cs.index ? ' active' : ''));
        row.type = 'button';
        row.setAttribute('role', 'option');
        row.setAttribute('aria-selected', String(i === cs.index));
        row.setAttribute('data-index', String(i));

        var head = el('div', 'strip-head');
        head.appendChild(el('span', 'n', '#' + fmtInt(capture.ordinal)));
        var clock = captureClock(capture);
        if (clock) { head.appendChild(el('span', 'time', clock)); }
        if (capture.port) { head.appendChild(el('span', 'port', capture.port)); }
        if (entry.repeats > 1) {
          var repeat = el('span', 'repeat', '×' + entry.repeats);
          repeat.setAttribute('data-tip', entry.repeats + ' identical screens in a row — shown once');
          head.appendChild(repeat);
        }
        row.appendChild(head);

        row.appendChild(el('div', 'strip-preview', previewLine(capture)));

        if (capture.step) {
          var step = el('div', 'strip-step',
            'step ' + capture.step + (capture.steps ? '/' + capture.steps : '') +
            (capture.type ? ' · ' + capture.type : ''));
          row.appendChild(step);
        }
        host.appendChild(row);
      });

      var active = $('.strip-item.active', host);
      if (active && active.scrollIntoView) { active.scrollIntoView({ block: 'nearest' }); }
    }

    function renderCount() {
      var node = $('#capturesCount');
      if (!node) { return; }
      var shown = cs.view.length;
      var total = cs.all.length;
      var filtered = cs.query || cs.worker || cs.step;
      node.textContent = !total
        ? 'No screens yet'
        : fmtInt(shown) + ' screen' + (shown === 1 ? '' : 's') +
          (filtered ? ' of ' + fmtInt(total) : '') +
          (shown < total && !filtered ? ' (' + fmtInt(total) + ' captured, repeats folded)' : '');

      var note = $('#capturesFilterNote');
      if (note) {
        var bits = [];
        if (cs.worker) { bits.push('worker ' + cs.worker); }
        if (cs.step) { bits.push('step ' + cs.step); }
        if (cs.query) { bits.push('“' + cs.query + '”'); }
        note.hidden = !bits.length;
        if (bits.length) {
          note.innerHTML = '';
          note.appendChild(el('span', 'f-label', 'Filtered to ' + bits.join(' · ')));
          var clear = el('button', 'f-clear', 'Clear');
          clear.type = 'button';
          clear.addEventListener('click', clearFilters);
          note.appendChild(clear);
        }
      }
    }

    function clearFilters() {
      cs.query = '';
      cs.worker = '';
      cs.step = '';
      var search = $('#capturesSearch');
      if (search) { search.value = ''; }
      var clear = $('#capturesSearchClear');
      if (clear) { clear.hidden = true; }
      rebuild();
    }

    /* ---------- the screen ---------- */

    /* The worker's own previous screen, which is what "what changed" means
       when twenty of them are writing to the same file. Without a worker to
       key on — an older capture file — the screen before it is the best
       available answer. */
    function previousFor(capture) {
      var index = cs.all.indexOf(capture);
      if (index <= 0) { return null; }
      if (!capture.port) { return cs.all[index - 1]; }
      for (var i = index - 1; i >= 0; i--) {
        if (cs.all[i].port === capture.port) { return cs.all[i]; }
      }
      return null;
    }

    var CHANGED = 1;
    var HIT = 2;
    var SELECTED = 4;
    var CURSOR = 8;

    function padded(line, cols) {
      var text = line || '';
      return text.length >= cols ? text : text + new Array(cols - text.length + 1).join(' ');
    }

    function currentCapture() {
      var entry = cs.view[cs.index];
      return entry ? entry.capture : null;
    }

    function renderScreen() {
      var grid = $('#capturesScreen');
      var frame = $('#capturesFrame');
      var empty = $('#capturesEmpty');
      if (!grid) { return; }

      var capture = currentCapture();
      if (frame) { frame.hidden = !capture; }
      if (empty) { empty.hidden = !!capture; }
      if (!capture) {
        grid.innerHTML = '';
        paintMeta(null);
        return;
      }

      var cols = MIN_COLS;
      capture.lines.forEach(function (line) { cols = Math.max(cols, line.length); });
      cs.cols = cols;

      var previous = cs.diff ? previousFor(capture) : null;
      var query = cs.query;

      grid.style.setProperty('--cols', String(cols));
      grid.innerHTML = '';
      capture.lines.forEach(function (line, rowIndex) {
        var text = padded(line, cols);
        var flags = new Uint8Array(cols);

        if (previous) {
          var before = padded(previous.lines[rowIndex] || '', cols);
          for (var c = 0; c < cols; c++) {
            if (text.charAt(c) !== before.charAt(c)) { flags[c] |= CHANGED; }
          }
        }
        if (query) {
          var haystack = text.toLowerCase();
          var at = haystack.indexOf(query);
          while (at !== -1) {
            for (var h = at; h < at + query.length && h < cols; h++) { flags[h] |= HIT; }
            at = haystack.indexOf(query, at + Math.max(1, query.length));
          }
        }
        if (cs.selection && cs.selection.row === rowIndex + 1) {
          for (var s = cs.selection.from - 1; s < cs.selection.to; s++) { flags[s] |= SELECTED; }
        }
        /* Where the host left the cursor. On a screen that is waiting for
           input this is the field it is waiting on, which is usually the
           field the next step has to fill. */
        if (capture.status && capture.status.cursorRow === rowIndex + 1) {
          var cursorCol = capture.status.cursorCol - 1;
          if (cursorCol >= 0 && cursorCol < cols) { flags[cursorCol] |= CURSOR; }
        }

        grid.appendChild(lineNode(rowIndex, text, flags));
      });

      renderRuler(cols);
      renderGutter(capture.lines.length);
      paintMeta(capture);
    }

    function lineNode(rowIndex, text, flags) {
      var node = el('div', 'scr-line');
      node.setAttribute('data-row', String(rowIndex + 1));
      var start = 0;
      for (var i = 1; i <= text.length; i++) {
        if (i === text.length || flags[i] !== flags[start]) {
          node.appendChild(runNode(text.slice(start, i), flags[start]));
          start = i;
        }
      }
      if (!text.length) { node.appendChild(document.createTextNode(' ')); }
      return node;
    }

    function runNode(text, flag) {
      if (!flag) { return document.createTextNode(text); }
      var cls = 'r';
      if (flag & CHANGED) { cls += ' chg'; }
      if (flag & HIT) { cls += ' hit'; }
      if (flag & SELECTED) { cls += ' sel'; }
      if (flag & CURSOR) { cls += ' cur'; }
      return el('span', cls, text);
    }

    /* x3270's own ruler: a dot a column, a plus every five, the tens digit
       every ten. Counting to column 47 by eye is exactly the mistake this
       panel exists to stop. */
    function renderRuler(cols) {
      var node = $('#capturesRuler');
      if (!node) { return; }
      var out = '';
      for (var c = 1; c <= cols; c++) {
        if (c % 10 === 0) { out += String((c / 10) % 10); }
        else if (c % 5 === 0) { out += '+'; }
        else { out += '·'; }
      }
      node.textContent = out;
      node.style.setProperty('--cols', String(cols));
    }

    function renderGutter(rows) {
      var node = $('#capturesGutter');
      if (!node) { return; }
      node.innerHTML = '';
      for (var r = 1; r <= rows; r++) {
        node.appendChild(el('span', 'g-row', String(r).padStart(2, ' ')));
      }
    }

    function paintMeta(capture) {
      var who = $('#capturesWho');
      var when = $('#capturesWhen');
      var stepChip = $('#capturesStepChip');
      var pos = $('#capturesPos');

      if (pos) {
        pos.textContent = cs.view.length
          ? fmtInt(cs.index + 1) + ' / ' + fmtInt(cs.view.length)
          : '—';
      }
      if (who) {
        who.textContent = !capture ? '—'
          : (capture.port ? 'worker ' + capture.port : 'worker unknown') +
            (capture.host ? ' · ' + capture.host + (capture.hostPort ? ':' + capture.hostPort : '') : '');
      }
      if (when) {
        when.textContent = !capture ? '—' : (captureClock(capture) || 'time not recorded');
      }
      if (stepChip) {
        var hasStep = !!(capture && capture.step);
        stepChip.hidden = !hasStep;
        if (hasStep) {
          stepChip.textContent = 'step ' + capture.step + (capture.steps ? '/' + capture.steps : '') +
            (capture.type ? ' · ' + capture.type : '');
        }
      }

      var cursorChip = $('#capturesCursorChip');
      if (cursorChip) {
        var status = capture && capture.status;
        var hasCursor = !!(status && status.cursorRow > 0);
        cursorChip.hidden = !hasCursor;
        if (hasCursor) {
          cursorChip.textContent = 'cursor R' + status.cursorRow + ' C' + status.cursorCol;
          cursorChip.setAttribute('data-tip',
            'Where the host left the cursor when this screen was captured' +
            (status.keyboardLocked ? ' · keyboard locked' : ''));
        }
      }

      var prev = $('#capturesPrev');
      var next = $('#capturesNext');
      if (prev) { prev.disabled = cs.index <= 0; }
      if (next) { next.disabled = cs.index < 0 || cs.index >= cs.view.length - 1; }
    }

    function select(index, keepFollow) {
      var bounded = cs.view.length ? clamp(index, 0, cs.view.length - 1) : -1;
      var changed = bounded !== cs.index;
      cs.index = bounded;
      if (changed) { cs.selection = null; }
      if (!keepFollow) {
        /* Picking a screen by hand is a statement that this is the screen
           you want to look at, so the strip stops jumping to the newest. */
        setFollow(cs.index === cs.view.length - 1);
      }
      renderScreen();
      paintSelectionReadout();
      if (changed) { renderList(); } else { markActive(); }
    }

    function markActive() {
      $$('#capturesList .strip-item').forEach(function (node) {
        var active = num(node.getAttribute('data-index'), -1) === cs.index;
        node.classList.toggle('active', active);
        node.setAttribute('aria-selected', String(active));
      });
    }

    function step(delta) {
      if (!cs.view.length) { return; }
      select(cs.index < 0 ? cs.view.length - 1 : cs.index + delta);
    }

    function setFollow(value) {
      cs.follow = !!value;
      var button = $('#capturesFollow');
      if (button) {
        button.setAttribute('aria-pressed', String(cs.follow));
        button.classList.toggle('primary', cs.follow);
        button.setAttribute('data-tip', cs.follow
          ? 'Following the run — every new screen is selected as it lands (F)'
          : 'Jump to each new screen as the run captures it (F)');
      }
    }

    function setStatus(text, isError) {
      var node = $('#capturesStatus');
      if (!node) { return; }
      node.textContent = text;
      node.classList.toggle('bad', !!isError);
    }

    /* ---------- coordinates ---------- */

    function charWidth() {
      var grid = $('#capturesScreen');
      if (!grid) { return 0; }
      var line = $('.scr-line', grid);
      var width = (line || grid).getBoundingClientRect().width;
      return cs.cols > 0 ? width / cs.cols : 0;
    }

    function positionFrom(event) {
      var line = event.target && event.target.closest ? event.target.closest('.scr-line') : null;
      if (!line) { return null; }
      var width = charWidth();
      if (!width) { return null; }
      var rect = line.getBoundingClientRect();
      var col = Math.floor((event.clientX - rect.left) / width) + 1;
      return {
        row: num(line.getAttribute('data-row'), 1),
        col: clamp(col, 1, cs.cols)
      };
    }

    function selectedText() {
      var capture = currentCapture();
      if (!capture || !cs.selection) { return ''; }
      var line = padded(capture.lines[cs.selection.row - 1] || '', cs.cols);
      return line.slice(cs.selection.from - 1, cs.selection.to);
    }

    function paintSelectionReadout(hover) {
      var coord = $('#capturesCoord span');
      var text = $('#capturesSelText');
      var buttons = ['#capturesCopyCoords', '#capturesCopyFill', '#capturesCopyCheck']
        .map(function (selector) { return $(selector); });

      if (cs.selection) {
        var length = cs.selection.to - cs.selection.from + 1;
        if (coord) {
          coord.textContent = 'Row ' + cs.selection.row + ' · Column ' + cs.selection.from + ' · Length ' + length;
        }
        var value = selectedText();
        if (text) {
          text.hidden = false;
          text.textContent = value.trim() ? '“' + value + '”' : '(blank field)';
        }
        buttons.forEach(function (button) { if (button) { button.hidden = false; } });
        return;
      }

      buttons.forEach(function (button) { if (button) { button.hidden = true; } });
      if (text) { text.hidden = true; }
      if (coord) {
        coord.textContent = hover
          ? 'Row ' + hover.row + ' · Column ' + hover.col
          : 'Hover the screen for row and column · drag across a field for its length';
      }
    }

    function coordinatesJSON() {
      if (!cs.selection) { return ''; }
      return JSON.stringify({
        Row: cs.selection.row,
        Column: cs.selection.from,
        Length: cs.selection.to - cs.selection.from + 1
      }, null, 2);
    }

    function stepJSON(type) {
      if (!cs.selection) { return ''; }
      var value = selectedText().trim();
      return JSON.stringify({
        Type: type,
        Coordinates: {
          Row: cs.selection.row,
          Column: cs.selection.from,
          Length: cs.selection.to - cs.selection.from + 1
        },
        Text: type === 'FillString' ? value : value
      }, null, 2);
    }

    /* ---------- export ---------- */

    function captureHeading(capture, index) {
      var bits = ['#' + index];
      var clock = capture.at > 0 ? new Date(capture.at).toISOString() : '';
      if (clock) { bits.push(clock); }
      if (capture.port) { bits.push('worker ' + capture.port); }
      if (capture.host) { bits.push(capture.host + (capture.hostPort ? ':' + capture.hostPort : '')); }
      if (capture.step) { bits.push('step ' + capture.step + (capture.steps ? '/' + capture.steps : '') + (capture.type ? ' ' + capture.type : '')); }
      return bits.join(' · ');
    }

    function exportVisible() {
      if (!cs.view.length) {
        Toast.push('warn', 'Nothing to export', 'No screens match the current filter.');
        return;
      }
      var parts = cs.view.map(function (entry, i) {
        var heading = captureHeading(entry.capture, i + 1) +
          (entry.repeats > 1 ? ' · ×' + entry.repeats + ' identical' : '');
        return heading + '\n' + new Array(heading.length + 1).join('-') + '\n' + entry.capture.text;
      });
      download('3270connect-screens-' + cs.pid + '-' + Date.now() + '.txt', parts.join('\n\n'));
      Toast.push('ok', 'Downloaded', fmtInt(cs.view.length) + ' screen' + (cs.view.length === 1 ? '' : 's') + ' saved.');
    }

    /* ---------- open / close ---------- */

    function open(pid, options) {
      var opts = options || {};
      var meta = metaByPid[pid] || {};
      var samePid = cs.pid === pid;

      cs.pid = pid;
      cs.path = meta.outputPath || '';
      cs.isOpen = true;
      if (!samePid) { clearCaptures(); cs.offset = 0; }

      cs.worker = opts.port || '';
      cs.step = opts.step ? String(opts.step) : '';
      cs.query = '';
      /* Opened from a worker row, following means following that worker —
         which is exactly what someone watching a stalled one wants. */
      setFollow(opts.follow !== false);

      var pidValue = $('#capturesPidValue');
      if (pidValue) { pidValue.textContent = pid; }
      var path = $('#capturesPath');
      if (path) { path.textContent = cs.path || 'Path unavailable'; }
      var search = $('#capturesSearch');
      if (search) { search.value = ''; }
      var searchClear = $('#capturesSearchClear');
      if (searchClear) { searchClear.hidden = true; }

      Dialog.open('capturesModal');

      if (!cs.path) {
        setStatus('No output file is configured for PID ' + pid + '.', true);
        clearCaptures();
        rebuild();
        return;
      }

      setStatus('Loading…');
      rebuild();
      load(!samePid);
      schedule();
    }

    function close() {
      cs.isOpen = false;
      if (cs.timer) { clearInterval(cs.timer); cs.timer = null; }
      cs.dragging = false;
    }

    /* ---------- wiring ---------- */

    function bind() {
      var modal = $('#capturesModal');
      if (!modal) { return; }

      modal.addEventListener('dialog:hidden', close);

      var list = $('#capturesList');
      if (list) {
        list.addEventListener('click', function (event) {
          var item = event.target.closest ? event.target.closest('.strip-item') : null;
          if (!item) { return; }
          select(num(item.getAttribute('data-index'), 0));
        });
      }

      var search = $('#capturesSearch');
      if (search) {
        search.addEventListener('input', function () {
          cs.query = search.value.trim().toLowerCase();
          var clear = $('#capturesSearchClear');
          if (clear) { clear.hidden = !search.value; }
          rebuild();
        });
      }
      var searchClear = $('#capturesSearchClear');
      if (searchClear) {
        searchClear.addEventListener('click', function () {
          search.value = '';
          cs.query = '';
          searchClear.hidden = true;
          rebuild();
          search.focus();
        });
      }

      var worker = $('#capturesWorker');
      if (worker) {
        worker.addEventListener('change', function () { cs.worker = worker.value; rebuild(); });
      }
      var stepFilter = $('#capturesStepFilter');
      if (stepFilter) {
        stepFilter.addEventListener('change', function () { cs.step = stepFilter.value; rebuild(); });
      }

      var follow = $('#capturesFollow');
      if (follow) {
        follow.addEventListener('click', function () {
          setFollow(!cs.follow);
          if (cs.follow) { select(cs.view.length - 1, true); }
        });
      }

      var interval = $('#capturesInterval');
      if (interval) {
        interval.addEventListener('change', function () {
          cs.interval = num(interval.value, 4000);
          schedule();
        });
      }

      var reload = $('#capturesReload');
      if (reload) { reload.addEventListener('click', function () { load(false); }); }

      var exportBtn = $('#capturesExport');
      if (exportBtn) { exportBtn.addEventListener('click', exportVisible); }

      var prev = $('#capturesPrev');
      if (prev) { prev.addEventListener('click', function () { step(-1); }); }
      var next = $('#capturesNext');
      if (next) { next.addEventListener('click', function () { step(1); }); }

      var diff = $('#capturesDiff');
      if (diff) {
        diff.checked = cs.diff;
        diff.addEventListener('change', function () {
          cs.diff = diff.checked;
          renderScreen();
        });
      }

      var copy = $('#capturesCopy');
      if (copy) {
        copy.addEventListener('click', function () {
          var capture = currentCapture();
          if (capture) { copyText(capture.text, 'Screen'); }
        });
      }
      var save = $('#capturesSave');
      if (save) {
        save.addEventListener('click', function () {
          var capture = currentCapture();
          if (!capture) { return; }
          download('3270connect-screen-' + (capture.seq || cs.index + 1) + '.txt',
            captureHeading(capture, cs.index + 1) + '\n\n' + capture.text);
        });
      }

      var copyCoords = $('#capturesCopyCoords');
      if (copyCoords) { copyCoords.addEventListener('click', function () { copyText(coordinatesJSON(), 'Coordinates'); }); }
      var copyFill = $('#capturesCopyFill');
      if (copyFill) { copyFill.addEventListener('click', function () { copyText(stepJSON('FillString'), 'FillString step'); }); }
      var copyCheck = $('#capturesCopyCheck');
      if (copyCheck) { copyCheck.addEventListener('click', function () { copyText(stepJSON('CheckValue'), 'CheckValue step'); }); }

      bindScreenPointer();
      bindKeys(modal);
    }

    /* Drag across a field to read its coordinates. A 3270 field lives on one
       row, so a drag stays on the row it started on however far up or down
       the pointer wanders. */
    function bindScreenPointer() {
      var grid = $('#capturesScreen');
      if (!grid) { return; }

      grid.addEventListener('mousemove', function (event) {
        var position = positionFrom(event);
        if (!position) { return; }
        if (cs.dragging && cs.selection) {
          cs.selection.from = Math.min(cs.selection.anchor, position.col);
          cs.selection.to = Math.max(cs.selection.anchor, position.col);
          renderScreen();
          paintSelectionReadout();
          return;
        }
        if (!cs.selection) { paintSelectionReadout(position); }
      });

      grid.addEventListener('mouseleave', function () {
        if (!cs.dragging && !cs.selection) { paintSelectionReadout(); }
      });

      grid.addEventListener('mousedown', function (event) {
        var position = positionFrom(event);
        if (!position) { return; }
        event.preventDefault();
        cs.dragging = true;
        cs.selection = { row: position.row, from: position.col, to: position.col, anchor: position.col };
        renderScreen();
        paintSelectionReadout();
      });

      document.addEventListener('mouseup', function () {
        if (!cs.dragging) { return; }
        cs.dragging = false;
        paintSelectionReadout();
      });

      /* Escape drops the selection without closing the dialog under it. */
      grid.addEventListener('dblclick', function () {
        cs.selection = null;
        renderScreen();
        paintSelectionReadout();
      });
    }

    function bindKeys(modal) {
      modal.addEventListener('keydown', function (event) {
        var tag = (event.target.tagName || '').toLowerCase();
        if (tag === 'input' || tag === 'select' || tag === 'textarea') { return; }
        if (event.ctrlKey || event.metaKey || event.altKey) { return; }

        switch (event.key) {
          case 'ArrowLeft': step(-1); event.preventDefault(); break;
          case 'ArrowRight': step(1); event.preventDefault(); break;
          case 'ArrowUp': step(-1); event.preventDefault(); break;
          case 'ArrowDown': step(1); event.preventDefault(); break;
          case 'Home': select(0); event.preventDefault(); break;
          case 'End': select(cs.view.length - 1); event.preventDefault(); break;
          case 'f':
          case 'F':
            setFollow(!cs.follow);
            if (cs.follow) { select(cs.view.length - 1, true); }
            event.preventDefault();
            break;
          case 'Escape':
            if (cs.selection) {
              cs.selection = null;
              renderScreen();
              paintSelectionReadout();
              event.stopPropagation();
              event.preventDefault();
            }
            break;
          default: break;
        }
      });
    }

    return { open: open, bind: bind };
  })();

  /* ======================================================================
     12. Command palette & keyboard shortcuts
     ====================================================================== */

  var Palette = (function () {
    var overlay = null;
    var input = null;
    var results = null;
    var items = [];
    var cursor = 0;

    function commands() {
      var base = [
        { group: 'Actions', title: 'Start a 3270Connect process', sub: 'Upload a workflow and launch a run', icon: 'rocket', run: function () { Modals.show('startProcessModal'); } },
        { group: 'Actions', title: 'Start a sample 3270 app', sub: 'Launch a bundled demo host', icon: 'server', run: function () { Modals.show('startAppModal'); } },
        { group: 'Actions', title: 'Open console logs', sub: 'Stream, filter and export logs', icon: 'terminal', run: function () { Modals.show('consoleModal'); } },
        { group: 'Actions', title: 'Browse screen captures', sub: 'Every screen the run captured, one at a time', icon: 'film', run: openScreenCaptures },
        { group: 'Actions', title: 'Refresh now', sub: 'Pull a fresh metrics snapshot', icon: 'rotate', run: function () { Refresh.now(); } },
        { group: 'Actions', title: 'Export process table (CSV)', sub: 'Download the current view', icon: 'file-csv', run: function () { $('#exportProcesses').click(); } },
        { group: 'Actions', title: 'Export duration chart (PNG)', sub: 'Save the chart as an image', icon: 'image', run: function () { Charts.exportPNG('duration'); } },

        { group: 'View', title: 'Theme · Phosphor green', sub: 'Classic terminal palette', icon: 'circle-half-stroke', run: function () { setTheme('phosphor'); } },
        { group: 'View', title: 'Theme · Amber CRT', sub: 'Warm vintage palette', icon: 'circle-half-stroke', run: function () { setTheme('amber'); } },
        { group: 'View', title: 'Theme · Ice blue', sub: 'Cool high-contrast palette', icon: 'circle-half-stroke', run: function () { setTheme('ice'); } },
        { group: 'View', title: 'Theme · Daylight', sub: 'Light mode for bright rooms', icon: 'sun', run: function () { setTheme('daylight'); } },
        { group: 'View', title: 'Toggle density', sub: 'Comfortable ↔ compact rows', icon: 'compress', run: toggleDensity },
        { group: 'View', title: 'Toggle CRT effects', sub: 'Scanlines and grid drift', icon: 'wand-magic-sparkles', run: toggleFx },
        { group: 'View', title: 'Toggle table / card view', sub: 'Switch the process layout', icon: 'table-cells-large', run: toggleView },
        { group: 'View', title: 'Toggle auto-refresh', sub: 'Pause or resume live polling', icon: 'play', run: toggleAutoRefresh },
        { group: 'View', title: 'Jump to live screen flow', sub: 'What each virtual user is doing right now', icon: 'satellite-dish', run: focusFlow },
        { group: 'View', title: 'Toggle stalled workers only', sub: 'Filter the flow to workers stuck on a step', icon: 'filter', run: toggleFlowStalledOnly },
        { group: 'Help', title: 'Keyboard shortcuts', sub: 'Show the shortcut sheet', icon: 'keyboard', run: function () { Modals.show('shortcutModal'); } }
      ];

      state.metrics.forEach(function (m) {
        base.push({
          group: 'Processes',
          title: 'PID ' + m.pid + ' · logs',
          sub: (m.status || '') + ' · ' + (m.params || '-dashboard'),
          icon: 'file-lines',
          run: function () { Modals.logsFor(m.pid); }
        });
        if (m.outputFilePath && String(m.outputFilePath).trim()) {
          base.push({
            group: 'Processes',
            title: 'PID ' + m.pid + ' · screens',
            sub: 'Browse the screens this run captured',
            icon: 'film',
            run: function () { Captures.open(m.pid); }
          });
        }
        base.push({
          group: 'Processes',
          title: 'PID ' + m.pid + ' · terminate',
          sub: 'Send a kill signal to this process',
          icon: 'skull-crossbones',
          run: function () { Modals.confirmKill(m.pid); }
        });
      });

      return base;
    }

    function score(command, query) {
      if (!query) { return 1; }
      var haystack = (command.title + ' ' + command.sub + ' ' + command.group).toLowerCase();
      var needle = query.toLowerCase();
      if (haystack.indexOf(needle) !== -1) { return 100 - haystack.indexOf(needle); }

      // Simple subsequence match so "stp" finds "Start a 3270Connect process".
      var hi = 0;
      for (var i = 0; i < needle.length; i++) {
        hi = haystack.indexOf(needle[i], hi);
        if (hi === -1) { return 0; }
        hi += 1;
      }
      return 1;
    }

    function render() {
      var query = input.value.trim();
      var scored = commands()
        .map(function (command) { return { command: command, score: score(command, query) }; })
        .filter(function (entry) { return entry.score > 0; })
        .sort(function (a, b) { return b.score - a.score; })
        .slice(0, 40);

      // Keep each group contiguous — ordered by its best-scoring member — so the
      // headings read as sections rather than repeating down the list.
      var order = [];
      var byGroup = {};
      scored.forEach(function (entry) {
        var group = entry.command.group;
        if (!byGroup[group]) { byGroup[group] = []; order.push(group); }
        byGroup[group].push(entry.command);
      });
      items = order.reduce(function (acc, group) { return acc.concat(byGroup[group]); }, []);

      cursor = 0;
      results.innerHTML = '';

      if (!items.length) {
        results.innerHTML = '<div class="palette-empty">No commands match “' + esc(query) + '”.</div>';
        return;
      }

      var lastGroup = null;
      items.forEach(function (command, index) {
        if (command.group !== lastGroup) {
          lastGroup = command.group;
          results.appendChild(el('div', 'palette-group', command.group));
        }
        var button = el('button', 'palette-item');
        button.type = 'button';
        button.setAttribute('data-index', String(index));
        button.setAttribute('aria-selected', String(index === 0));

        var iconBox = el('div', 'pi-icon');
        iconBox.innerHTML = icon(command.icon);
        var text = el('div', 'pi-text');
        text.appendChild(el('div', 'pi-title', command.title));
        if (command.sub) { text.appendChild(el('div', 'pi-sub', command.sub)); }

        button.appendChild(iconBox);
        button.appendChild(text);
        button.addEventListener('click', function () { execute(index); });
        button.addEventListener('mousemove', function () { select(index); });
        results.appendChild(button);
      });
    }

    function select(index) {
      cursor = clamp(index, 0, items.length - 1);
      $$('.palette-item', results).forEach(function (node) {
        var isCurrent = Number(node.getAttribute('data-index')) === cursor;
        node.setAttribute('aria-selected', String(isCurrent));
        if (isCurrent) { node.scrollIntoView({ block: 'nearest' }); }
      });
    }

    function execute(index) {
      var command = items[index];
      close();
      if (command && typeof command.run === 'function') {
        setTimeout(command.run, 60);
      }
    }

    function open() {
      overlay.classList.add('open');
      input.value = '';
      render();
      input.focus();
    }

    function close() {
      overlay.classList.remove('open');
    }

    function isOpen() { return overlay && overlay.classList.contains('open'); }

    function bind() {
      overlay = $('#paletteOverlay');
      input = $('#paletteInput');
      results = $('#paletteResults');
      if (!overlay) { return; }

      input.addEventListener('input', render);
      overlay.addEventListener('click', function (event) { if (event.target === overlay) { close(); } });

      input.addEventListener('keydown', function (event) {
        if (event.key === 'ArrowDown') { event.preventDefault(); select(cursor + 1); }
        else if (event.key === 'ArrowUp') { event.preventDefault(); select(cursor - 1); }
        else if (event.key === 'Enter') { event.preventDefault(); execute(cursor); }
        else if (event.key === 'Escape') { event.preventDefault(); close(); }
      });

      var trigger = $('#paletteTrigger');
      if (trigger) { trigger.addEventListener('click', open); }
    }

    return { bind: bind, open: open, close: close, isOpen: isOpen };
  })();

  /* --- view preference helpers (shared by palette and toolbar) --- */

  function setTheme(theme) {
    Prefs.set('theme', theme);
    Prefs.apply();
    $$('#themePicker button').forEach(function (button) {
      button.setAttribute('aria-pressed', String(button.getAttribute('data-theme') === theme));
    });
    // Charts read their colours from CSS variables, so re-skin after the swap.
    requestAnimationFrame(function () {
      Charts.retheme();
      renderKPIs();
      renderLatency();
    });
  }

  function toggleDensity() {
    var next = Prefs.get('density') === 'compact' ? 'comfortable' : 'compact';
    Prefs.set('density', next);
    Prefs.apply();
    var button = $('#densityToggle');
    if (button) { button.setAttribute('aria-pressed', String(next === 'compact')); }
    Toast.push('info', 'Density', next === 'compact' ? 'Compact layout enabled.' : 'Comfortable layout enabled.');
  }

  function toggleFx() {
    var next = Prefs.get('fx') === 'on' ? 'off' : 'on';
    Prefs.set('fx', next);
    Prefs.apply();
    var button = $('#fxToggle');
    if (button) { button.setAttribute('aria-pressed', String(next === 'on')); }
  }

  function toggleView() {
    var next = Prefs.get('view') === 'cards' ? 'table' : 'cards';
    Prefs.set('view', next);
    Prefs.apply();
    $$('#viewToggle button').forEach(function (button) {
      button.setAttribute('aria-pressed', String(button.getAttribute('data-view') === next));
    });
  }

  function focusFlow() {
    var panel = $('#flowPanel');
    if (!panel || panel.hidden) {
      Toast.push('info', 'Nothing in flight', 'The live screen flow appears once a run has workers executing steps.');
      return;
    }
    panel.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  /* The keyboard and the palette do not have a process row to click, so they
     pick one: the running run that is writing screens, newest first, and a
     finished one only if nothing is running. */
  function openScreenCaptures() {
    var candidates = state.metrics.filter(function (m) {
      return m.outputFilePath && String(m.outputFilePath).trim();
    });
    if (!candidates.length) {
      Toast.push('info', 'No captured screens',
        'A run writes screens once its workflow has an OutputFilePath and an AsciiScreenGrab step.');
      return;
    }
    candidates.sort(function (a, b) {
      if (!!a.isRunning !== !!b.isRunning) { return a.isRunning ? -1 : 1; }
      return num(b.pid) - num(a.pid);
    });
    Captures.open(candidates[0].pid);
  }

  function toggleFlowStalledOnly() {
    var toggle = $('#flowStalledOnly');
    state.flowStalledOnly = !state.flowStalledOnly;
    Prefs.set('flowStalledOnly', state.flowStalledOnly);
    if (toggle) { toggle.checked = state.flowStalledOnly; }
    renderFlow();
    focusFlow();
  }

  function toggleAutoRefresh() {
    var next = !Prefs.get('autoRefresh');
    Prefs.set('autoRefresh', next);
    var toggle = $('#autoRefreshToggle');
    if (toggle) { toggle.checked = next; }
    Refresh.start();
    Refresh.setStatus(next ? 'Live · every ' + Prefs.get('refreshPeriod') + 's' : 'Auto-refresh off');
    if (next) { Refresh.now(); }
  }

  var Keys = (function () {
    function isTyping(event) {
      var node = event.target;
      return node && (node.tagName === 'INPUT' || node.tagName === 'TEXTAREA' || node.tagName === 'SELECT' || node.isContentEditable);
    }

    function bind() {
      document.addEventListener('keydown', function (event) {
        var meta = event.metaKey || event.ctrlKey;

        if (meta && event.key.toLowerCase() === 'k') {
          event.preventDefault();
          Palette.isOpen() ? Palette.close() : Palette.open();
          return;
        }

        if (event.key === 'Escape' && Palette.isOpen()) { Palette.close(); return; }
        // Single-key shortcuts stay out of the way of dialogs and text entry.
        if (isTyping(event) || meta || event.altKey || Palette.isOpen() || Dialog.top()) { return; }

        switch (event.key) {
          case '/':
            event.preventDefault();
            var search = $('#procSearch');
            if (search) { search.focus(); search.select(); }
            break;
          case '?':
            event.preventDefault();
            Modals.show('shortcutModal');
            break;
          case 'r': Refresh.now(); break;
          case 'c': Modals.show('consoleModal'); break;
          case 'v': openScreenCaptures(); break;
          case 's': Modals.show('startProcessModal'); break;
          case 'a': Modals.show('startAppModal'); break;
          case 'p': toggleAutoRefresh(); break;
          case 'd': toggleDensity(); break;
          case 't':
            var order = ['phosphor', 'amber', 'ice', 'daylight'];
            var index = order.indexOf(Prefs.get('theme'));
            setTheme(order[(index + 1) % order.length]);
            break;
          default: break;
        }
      });
    }

    return { bind: bind };
  })();

  /* ======================================================================
     13. Boot
     ====================================================================== */

  function bindToolbar() {
    var toggle = $('#autoRefreshToggle');
    if (toggle) {
      toggle.checked = !!Prefs.get('autoRefresh');
      toggle.addEventListener('change', function () {
        Prefs.set('autoRefresh', toggle.checked);
        Refresh.start();
        Refresh.setStatus(toggle.checked ? 'Live · every ' + Prefs.get('refreshPeriod') + 's' : 'Auto-refresh off');
        if (toggle.checked) { Refresh.now(); }
      });
    }

    $$('#intervalPicker button').forEach(function (button) {
      var value = button.getAttribute('data-interval');
      button.setAttribute('aria-pressed', String(value === String(Prefs.get('refreshPeriod'))));
      button.addEventListener('click', function () {
        Prefs.set('refreshPeriod', value);
        $$('#intervalPicker button').forEach(function (other) {
          other.setAttribute('aria-pressed', String(other === button));
        });
        Refresh.start();
        Refresh.setStatus(Prefs.get('autoRefresh') ? 'Live · every ' + value + 's' : 'Auto-refresh off');
      });
    });

    $$('#themePicker button').forEach(function (button) {
      var theme = button.getAttribute('data-theme');
      button.setAttribute('aria-pressed', String(theme === Prefs.get('theme')));
      button.addEventListener('click', function () { setTheme(theme); });
    });

    $$('#viewToggle button').forEach(function (button) {
      var view = button.getAttribute('data-view');
      button.setAttribute('aria-pressed', String(view === Prefs.get('view')));
      button.addEventListener('click', function () {
        Prefs.set('view', view);
        Prefs.apply();
        $$('#viewToggle button').forEach(function (other) {
          other.setAttribute('aria-pressed', String(other === button));
        });
      });
    });

    $$('#windowPicker button').forEach(function (button) {
      var value = button.getAttribute('data-window');
      button.setAttribute('aria-pressed', String(value === String(Prefs.get('chartWindow'))));
      button.addEventListener('click', function () {
        Prefs.set('chartWindow', value);
        $$('#windowPicker button').forEach(function (other) {
          other.setAttribute('aria-pressed', String(other === button));
        });
        Charts.update();
      });
    });

    var density = $('#densityToggle');
    if (density) {
      density.setAttribute('aria-pressed', String(Prefs.get('density') === 'compact'));
      density.addEventListener('click', toggleDensity);
    }

    var fx = $('#fxToggle');
    if (fx) {
      fx.setAttribute('aria-pressed', String(Prefs.get('fx') === 'on'));
      fx.addEventListener('click', toggleFx);
    }

    var wrapButton = $('#logWrap');
    if (wrapButton) { wrapButton.setAttribute('aria-pressed', String(Prefs.get('wrap') === 'on')); }

    [['#refreshNow', function () { Refresh.now(); }],
     ['#openStartProcess', function () { Modals.show('startProcessModal'); }],
     ['#openStartApp', function () { Modals.show('startAppModal'); }],
     ['#openConsole', function () { Modals.show('consoleModal'); }],
     ['#openShortcuts', function () { Modals.show('shortcutModal'); }],
     ['#resetDurationZoom', function () { Charts.resetZoom('duration'); }],
     ['#resetResourceZoom', function () { Charts.resetZoom('resource'); }],
     ['#exportDurationPNG', function () { Charts.exportPNG('duration'); }],
     ['#exportDurationCSV', function () { Charts.exportCSV('duration'); }],
     ['#exportResourcePNG', function () { Charts.exportPNG('resource'); }],
     ['#exportResourceCSV', function () { Charts.exportCSV('resource'); }]
    ].forEach(function (pair) {
      var node = $(pair[0]);
      if (node) { node.addEventListener('click', pair[1]); }
    });

    // Initial status filter state from prefs
    $$('#statusFilter button').forEach(function (button) {
      button.setAttribute('aria-pressed', String(button.getAttribute('data-filter') === state.statusFilter));
    });

    // Clock in the topbar
    var clock = $('#uptimeClock');
    if (clock) {
      var boot = Date.now();
      setInterval(function () {
        clock.textContent = fmtSeconds((Date.now() - boot) / 1000);
      }, 1000);
    }
  }

  function boot() {
    Prefs.apply();
    Tip.bind();
    Dialog.bind();
    bindToolbar();
    bindTable();
    bindFlow();
    Modals.bind();
    Captures.bind();
    Palette.bind();
    Keys.bind();

    Charts.init();
    Refresh.applySnapshot();
    Refresh.now();
    Refresh.start();
    Refresh.setStatus(Prefs.get('autoRefresh') ? 'Live · every ' + Prefs.get('refreshPeriod') + 's' : 'Auto-refresh off');

    var stamp = $('#refreshStamp');
    if (stamp) { stamp.textContent = 'Updated ' + fmtClock(new Date()); }

    if (!Charts.available()) {
      Toast.push('warn', 'Offline mode', 'Chart.js could not be loaded — charts are disabled but every metric remains available.');
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})();
