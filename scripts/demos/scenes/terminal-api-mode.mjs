/* How-to: drive 3270Connect over HTTP.
 *
 * The point of the video is the last beat — the response carries the screen the
 * workflow actually saw, so a caller gets the mainframe's answer back rather
 * than a job id to go and poll.
 */
export const meta = {
  id: 'terminal-api-mode',
  kicker: 'How-to · Terminal',
  title: 'Call it over HTTP',
  subtitle: 'One POST runs a workflow and hands back the screen',
  endNote: 'API reference at <code>/advanced-features</code>',
  poster: 30,
};

export async function run(ctx) {
  await ctx.titleCard(3.2);
  await ctx.stage.chapter('1 · start the server');

  await ctx.playCast('api-mode', {
    title: 'bash — 3270Connect',
    cues: [
      {
        at: 'serve.prompt',
        until: 'serve.start+0.6',
        chapter: '1 · start the server',
        text: 'API mode is the same binary with `-api`',
        sub: 'It still wants a `-config`: the file is the fallback for requests that omit one',
      },
      {
        at: 'serve.start+1.0',
        until: 'post.prompt-0.3',
        text: 'It says what it will accept before it accepts anything',
        sub: 'No credential unless you set `API_TOKEN`, or `AUTH_MODE=local` for per-account tokens',
      },
      {
        at: 'post.prompt+0.2',
        until: 'post.start+1.0',
        chapter: '2 · post a workflow',
        text: 'POST the workflow JSON — the same file the CLI takes',
        sub: 'The request blocks for as long as the session does; this one runs six steps',
      },
      {
        at: 'post.done-0.3',
        until: 'screen.prompt-0.3',
        text: 'A workflow that ran cleanly answers 200',
        sub: 'A failed step answers with the step that failed and why',
      },
      {
        at: 'screen.prompt+0.2',
        until: 'screen.start+0.8',
        chapter: '3 · read the screen',
        text: 'The response carries the screen itself',
      },
      {
        at: 'screen.start+1.0',
        until: 'end',
        text: 'That is the 3270 screen the host drew, returned to the caller',
        sub: 'Every `AsciiScreenGrab` step in the workflow lands in `output`',
      },
    ],
  });

  await ctx.endCard(3.0);
}
