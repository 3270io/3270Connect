/* How-to: run your first workflow from the command line.
 *
 * The three beats are the three things somebody has to understand before they
 * can use the tool at all — what a workflow file is, what running it prints,
 * and what it leaves behind. Cue times are anchored to the cast's marks, so
 * re-recording the cast on a slower machine keeps the captions in place.
 */
export const meta = {
  id: 'terminal-first-workflow',
  kicker: 'How-to · Terminal',
  title: 'Your first workflow',
  subtitle: 'Describe a 3270 session in JSON, then replay it from the shell',
  endNote: 'Full step reference at <code>/workflow</code>',
  poster: 12,
};

export async function run(ctx) {
  await ctx.titleCard(3.2);
  await ctx.stage.chapter('1 · the workflow file');

  await ctx.playCast('first-workflow', {
    title: 'bash — 3270Connect',
    cues: [
      {
        at: 'show.prompt',
        until: 'show.start',
        chapter: '1 · the workflow file',
        text: 'A workflow is a JSON file, not a script',
        sub: 'Where to connect, and the steps to take once you are there',
      },
      {
        at: 'show.start+0.6',
        until: 'run.prompt-0.4',
        text: 'Connect, check the screen, fill two fields, press enter, capture',
        sub: 'Coordinates are 1-based — `Row 5, Column 21` is where the first field starts',
      },
      {
        at: 'run.prompt+0.2',
        until: 'run.start+0.4',
        chapter: '2 · running it',
        text: 'Point the binary at the file and run it',
        sub: '`-headless` drives the session without opening an emulator window',
      },
      {
        at: 'run.done-0.2',
        until: 'artifacts.prompt-0.4',
        text: 'Every run ends with a report',
        sub: 'Workflows started, completed and failed, plus the time each one took',
      },
      {
        at: 'artifacts.prompt+0.2',
        until: 'artifacts.start+0.6',
        chapter: '3 · what it leaves behind',
        text: 'The run writes its evidence to disk',
      },
      {
        at: 'artifacts.start+0.8',
        until: 'end',
        text: 'A summary you can read later, and every screen it captured',
        sub: '`logs/summary_<pid>.txt` · `screens.html` from the `AsciiScreenGrab` steps',
      },
    ],
  });

  await ctx.endCard(3.0);
}
