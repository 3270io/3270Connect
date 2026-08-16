/* How-to: put a sign-in on the console, then use the administration pages.
 *
 * The account is created with the CLI before recording rather than through the
 * first-run setup page, because the setup code is printed to the server's log
 * and reading it would mean putting a terminal on screen next to the browser.
 * The pages after sign-in are the point of the video, and they are identical
 * either way.
 */
const CONSOLE_PORT = 8146;
const USERNAME = 'ada';
const PASSWORD = 'Analytical-Engine-1843';

export const meta = {
  id: 'console-sign-in',
  kicker: 'How-to · Console',
  title: 'Sign-in and administration',
  subtitle: 'Turn on accounts, then manage them from the browser',
  endNote: 'Setup and roles at <code>/authentication</code>',
  poster: 34,
};

export async function run(ctx) {
  const state = await ctx.freshState();
  const env = { XDG_CONFIG_HOME: state, AUTH_MODE: 'local' };

  // Seed the first administrator the same way an operator would, so the video
  // opens on the sign-in page rather than the one-time setup page.
  await ctx.run('./3270Connect', ['user', 'add', USERNAME, '--admin'], {
    env, stdin: `${PASSWORD}\n`,
  });

  await ctx.services.start({
    argv: ['./3270Connect', '-dashboard', '-dashboardPort', String(CONSOLE_PORT)],
    env: { ...env, DASHBOARD_BIND: '127.0.0.1' },
    port: CONSOLE_PORT,
  });

  await ctx.titleCard(3.4, () => ctx.openConsole(
    `http://127.0.0.1:${CONSOLE_PORT}/dashboard`, {
      width: 1460, height: 790, show: false,
      title: `127.0.0.1:${CONSOLE_PORT}`,
    }));

  const f = ctx.frame();
  await ctx.stage.chapter('1 · signing in');
  await ctx.stage.showWindow(true);
  await ctx.sleep(1100);

  await ctx.say(
    'With `AUTH_MODE=local`, the console asks who you are',
    'Every route is gated, including the ones a later release adds — the gate wraps the whole server',
    5.4);

  await ctx.clickThrough(f.locator('#username'));
  await f.locator('#username').type(USERNAME, { delay: 90 });
  await ctx.sleep(400);
  await ctx.clickThrough(f.locator('#password'));
  await f.locator('#password').type(PASSWORD, { delay: 55 });
  await ctx.say('Accounts come from `3270Connect user add`, or from your identity provider', null, 3.0);

  await ctx.clickThrough(f.locator('button.auth-submit'));
  await ctx.sleep(2600);
  await ctx.say(
    'The console is the same console — the runs on it now have an owner',
    null, 4.0);

  /* -------------------------------------------------------- administration */

  await ctx.stage.chapter('2 · administration');
  await ctx.openConsole(`http://127.0.0.1:${CONSOLE_PORT}/admin`, {
    width: 1460, height: 790, title: `127.0.0.1:${CONSOLE_PORT}/admin`,
  });
  await ctx.say(
    'Administrators get a second set of pages under `/admin`',
    'Accounts, groups, API tokens, every load run on the machine, and the audit trail',
    5.4);

  for (const [path, text, sub] of [
    ['/admin/users', 'Accounts, their role, and where that role came from',
      'A role can be set here or inherited from a group your provider sends'],
    ['/admin/tokens', 'API tokens are issued per account, not shared',
      'The token is shown once; what is stored is a hash, and revoking it is immediate'],
    ['/admin/audit', 'And everything lands in the audit trail',
      'Sign-ins, runs started and stopped, and every change made on these pages'],
  ]) {
    await ctx.openConsole(`http://127.0.0.1:${CONSOLE_PORT}${path}`, {
      width: 1460, height: 790, title: `127.0.0.1:${CONSOLE_PORT}${path}`,
    });
    await ctx.say(text, sub, 5.2);
  }

  await ctx.endCard(3.0);
}
