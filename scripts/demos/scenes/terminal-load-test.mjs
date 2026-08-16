/* How-to: turn one workflow into a load test.
 *
 * A single command, and the captions do the explaining: what the two flags
 * mean, what the row printed during the run is telling you, and what happens at
 * the deadline. The run is real — thirty seconds of eight workers against the
 * bundled sample host.
 */
export const meta = {
  id: 'terminal-load-test',
  kicker: 'How-to · Terminal',
  title: 'Run it at scale',
  subtitle: 'The same workflow, eight workers deep, with a deadline',
  endNote: 'Flags and tuning at <code>/basic-usage</code>',
  poster: 18,
};

export async function run(ctx) {
  await ctx.titleCard(3.2);
  await ctx.stage.chapter('the two flags that matter');

  await ctx.playCast('load-test', {
    title: 'bash — 3270Connect',
    cues: [
      {
        at: 'run.prompt',
        until: 'run.start+0.5',
        chapter: 'the two flags that matter',
        text: 'Two flags turn a workflow into a load test',
        sub: '`-concurrent` is how many workers, `-runtime` is how long to keep going',
      },
      {
        at: 'run.start+1.2',
        until: 'run.start+7',
        chapter: 'ramping up',
        text: 'Workers start in batches, not all at once',
        sub: 'Ramp-up batch size and delay come from the workflow file',
      },
      {
        at: 'run.start+7.4',
        until: 'run.done-2.2',
        chapter: 'watching it',
        text: 'One line per interval: in flight, launched, finished, failed',
        sub: 'The pair on the left is active workers against the concurrency you asked for',
      },
      {
        at: 'run.done-2.0',
        until: 'run.done+0.4',
        chapter: 'the deadline',
        text: 'At the deadline it stops scheduling and lets the stragglers finish',
        sub: 'The grace period is how long they get — 30 seconds by default',
      },
      {
        at: 'run.done+0.8',
        until: 'end',
        chapter: 'the report',
        text: '32 workflows, none failed, six seconds each',
        sub: 'Same report as a single run, plus the vUser count you sustained',
      },
    ],
  });

  await ctx.endCard(3.0);
}
