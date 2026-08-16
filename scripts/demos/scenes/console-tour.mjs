/* Showcase: the operations console, start to finish.
 *
 * Unlike the terminal scenes this one is live — it starts a real console and a
 * real sample host, launches a real load run through the browser, and waits for
 * actual numbers to arrive. Nothing on screen is staged, which is the point: it
 * is the only honest way to show a console whose whole job is to move.
 */
const CONSOLE_PORT = 8145;
// The sample host has to answer on the port workflows/load-test.json names —
// the console pre-fills its host and port overrides from the uploaded file, so
// a mismatch here does not fail loudly. It just leaves every worker sitting on
// Connect, and the charts stay empty for the whole video.
const HOST_PORT = 3270;

export const meta = {
  id: 'console-tour',
  kicker: 'Showcase · Console',
  title: 'The operations console',
  subtitle: 'Launch a load run from the browser and watch it land',
  endNote: 'Console reference at <code>/dashboard</code>',
  poster: 26,
};

export async function run(ctx) {
  const state = await ctx.freshState();
  await ctx.services.start({
    argv: ['./3270Connect', '-runApp', '1', '-runApp-port', String(HOST_PORT)],
    env: { XDG_CONFIG_HOME: state },
    wait: 2500,
  });
  await ctx.services.start({
    argv: ['./3270Connect', '-dashboard', '-dashboardPort', String(CONSOLE_PORT)],
    env: { XDG_CONFIG_HOME: state, DASHBOARD_BIND: '127.0.0.1' },
    port: CONSOLE_PORT,
  });

  // The console loads behind the title card so the stage is never empty.
  await ctx.titleCard(3.6, () => ctx.openConsole(
    `http://127.0.0.1:${CONSOLE_PORT}/dashboard`, {
      width: 1460, height: 790, show: false,
      title: `127.0.0.1:${CONSOLE_PORT}/dashboard`,
    }));

  await ctx.stage.chapter('1 · the console');
  await ctx.stage.showWindow(true);
  await ctx.sleep(1100);
  const f = ctx.frame();

  await ctx.say(
    'Any 3270Connect can serve its own console',
    '`3270Connect -dashboard` — no separate service, no database',
    4.4);

  await ctx.spotlight(f.locator('.kpi-strip'));
  await ctx.say(
    'Six readings, refreshed while you watch',
    'Active workers, workflows started, completed and failed, success rate, throughput',
    5.0);
  await ctx.spotlight(null);

  /* ------------------------------------------------------- launching a run */

  await ctx.stage.chapter('2 · launching a run');
  await ctx.clickThrough(f.locator('#openStartProcess'));
  await ctx.sleep(900);
  await ctx.say('Runs start from the browser as well as the shell', null, 3.4);

  await f.locator('#configFile').setInputFiles(`${ctx.build}/load-test.json`);
  await ctx.sleep(1400);
  await ctx.say(
    'The workflow is parsed in the browser before it is uploaded',
    'Host, port, output file and step count, read back from the file you chose',
    5.0);

  await ctx.clickThrough(f.locator('#concurrent'));
  await f.locator('#concurrent').fill('8');
  await ctx.sleep(500);
  await f.locator('#runtime').fill('70');
  await ctx.say(
    'Then the load profile — how many workers, and for how long',
    'Eight concurrent workflows against the bundled sample host',
    4.6);

  await ctx.clickThrough(f.locator('#startProcessSubmit'));
  await ctx.stage.clearCaption();
  await ctx.sleep(2500);

  /* ---------------------------------------------------------- watching it */

  await ctx.stage.chapter('3 · watching it run');
  await ctx.say(
    'The run is a child process, and the console owns it',
    'It appears in the table with its own PID, runtime and progress',
    2.0);
  await ctx.sleep(6000);

  await ctx.scrollTo('#procTable', { offset: -120 });
  await ctx.say(
    'Every process on the machine, live',
    'Filter by PID or status, or stop a run from its row',
    5.0);

  await ctx.scrollTo('#flowPanel', { offset: -90 });
  await ctx.say(
    'Live screen flow shows where each worker has got to',
    'Workers grouped by the step they are on, so a stall is obvious',
    5.4);

  await ctx.scrollTo('.chart-grid', { offset: -80 });
  await ctx.say(
    'The charts fill in as results land',
    'Workflow duration over time, outcomes, and what the run costs the box',
    5.4);

  /* --------------------------------------------------------- logs + theme */

  await ctx.stage.chapter('4 · logs and appearance');
  await ctx.scrollTo('body', { offset: 0, settle: 800 });
  await ctx.clickThrough(f.locator('#openConsole'));
  await ctx.sleep(1200);
  await ctx.say(
    'Console logs stream from every process at once',
    'Filter by PID or severity, or search the text',
    5.0);
  await ctx.clickThrough(f.locator('#consoleModal .btn-close[data-close]'));
  await ctx.sleep(900);

  await ctx.say('Four palettes, and the docs share them', null, 1.6);
  for (const [theme, label] of [['amber', 'AMB'], ['ice', 'ICE'], ['daylight', 'DAY'],
    ['phosphor', 'GRN']]) {
    // Caption first: written the other way round, each line names the palette
    // that was on screen a moment ago rather than the one about to appear.
    await ctx.stage.caption('Four palettes, and the docs share them',
      `Now showing ${label} — the choice is remembered per browser`);
    await ctx.clickThrough(f.locator(`#themePicker button[data-theme="${theme}"]`),
      { settle: 220 });
    await ctx.sleep(1600);
  }
  await ctx.sleep(1200);
  await ctx.stage.pointer(0, 0, { show: false });

  await ctx.endCard(3.0);
}
