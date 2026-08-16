/* How-to: profile a host before you trust a workflow against it.
 *
 * The probe is one connect and a handful of read-only Query actions, so the
 * video is short by nature. The captions carry the reason to run it: the
 * document is comparable across tools and across time, which is what makes a
 * host change visible instead of merely surprising.
 */
export const meta = {
  id: 'terminal-host-profiler',
  kicker: 'How-to · Terminal',
  title: 'Profile a host',
  subtitle: 'One connect, one JSON document describing what answered',
  endNote: 'Field reference at <code>/compatibility-profile-schema</code>',
  poster: 18,
};

export async function run(ctx) {
  await ctx.titleCard(3.2);
  await ctx.stage.chapter('1 · the probe');

  await ctx.playCast('host-profiler', {
    title: 'bash — 3270Connect',
    cues: [
      {
        at: 'probe.prompt',
        until: 'probe.start+0.5',
        chapter: '1 · the probe',
        text: '`-profile` connects once, asks the host about itself, and exits',
        sub: 'Read-only — it runs no workflow and types nothing into the session',
      },
      {
        at: 'probe.done-0.3',
        until: 'device.prompt-0.4',
        text: 'A few seconds, then a `CompatibilityProfile` document on disk',
        sub: 'Non-zero exit on failure, so a pipeline can stop before the load test starts',
      },
      {
        at: 'device.prompt+0.2',
        until: 'device.start+0.8',
        chapter: '2 · what answered',
        text: 'What is on the other end, as the negotiation saw it',
      },
      {
        at: 'device.start+1.0',
        until: 'timing.prompt-0.4',
        text: 'Terminal model, screen size, colour, extended attributes',
        sub: 'The same shape 3270Web writes, so two tools against one host diff cleanly',
      },
      {
        at: 'timing.prompt+0.2',
        until: 'timing.start+0.8',
        chapter: '3 · gaps and timing',
        text: 'What it could not determine is recorded, not guessed',
      },
      {
        at: 'timing.start+1.0',
        until: 'end',
        text: 'Unanswered queries are listed, and the connect is timed',
        sub: 'A sample host answers less than a z/OS image would — the gaps are the point',
      },
    ],
  });

  await ctx.endCard(3.0);
}
