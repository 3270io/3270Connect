/* How-to: run 3270Connect as an MCP server.
 *
 * The last beat is a real MCP session — initialize, then a tools/call — rather
 * than a screenshot of a client's settings page. It is the part somebody
 * debugging a client configuration actually needs to see, because it separates
 * "the server works" from "my client is wired up wrong".
 */
export const meta = {
  id: 'terminal-mcp-server',
  kicker: 'How-to · Terminal',
  title: 'Drive it from an AI client',
  subtitle: 'The same binary, speaking Model Context Protocol',
  endNote: 'Client setup at <code>/mcp</code>',
  poster: 10,
};

export async function run(ctx) {
  await ctx.titleCard(3.2);
  await ctx.stage.chapter('1 · the catalogue');

  await ctx.playCast('mcp-server', {
    title: 'bash — 3270Connect',
    cues: [
      {
        at: 'list.prompt',
        until: 'list.start+0.6',
        chapter: '1 · the catalogue',
        text: '`3270Connect mcp` is the whole install',
        sub: '`--list-tools` prints the catalogue and exits — no host, no workflow, no run',
      },
      {
        at: 'list.start+1.0',
        until: 'schema.prompt-0.4',
        text: 'Fifteen tools, every one of them read-only by default',
        sub: 'Composing and validating workflows, then reading back runs, logs and latencies',
      },
      {
        at: 'schema.prompt+0.2',
        until: 'schema.start+0.8',
        chapter: '2 · what a client sees',
        text: 'Each tool carries the description the model reads',
      },
      {
        at: 'schema.start+1.0',
        until: 'call.prompt-0.4',
        text: 'The descriptions say when to reach for a tool, not just what it does',
        sub: 'This is what an AI client uses to choose — worth reading before you widen `MCP_TOOLS`',
      },
      {
        at: 'call.prompt+0.2',
        until: 'call.start+1.2',
        chapter: '3 · a real call',
        text: 'And it is a plain stdio server, so you can call it by hand',
        sub: 'An `initialize`, then a `tools/call` — the exchange a client would make',
      },
      {
        at: 'call.done-0.4',
        until: 'end',
        text: 'The host answered, and that came back through the protocol',
        sub: 'If this works and your client does not, the problem is the client configuration',
      },
    ],
  });

  await ctx.endCard(3.0);
}
