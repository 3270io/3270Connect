# Before you generate load

A load test is the one thing this tool does that a host cannot ignore. Every
other operation asks a question; this one takes resources from whoever else is
using the system. Treat starting one as an action with consequences, not as a
measurement.

## Confirm the target

Say the hostname and port back to the user and get agreement before the first
concurrent run. A test LPAR and a production one differ by a character often
enough that this is worth the extra turn, and there is no undoing it: the
transactions have run, the logs have the entries, and anything the workflow
wrote is written.

`MCP_ALLOWED_HOSTS`, when set, restricts what can be targeted at all. If a
host is refused by it, that is a deliberate boundary — say so and stop rather
than looking for another route to the same host.

## Confirm the scale

`MCP_MAX_CONCURRENT` and `MCP_MAX_RUNTIME_SEC` cap what a single call may
request. They exist because "run a big load test" is a reasonable thing to say
and a bad thing to interpret generously. If a request would exceed them, say
what the cap is rather than quietly running something smaller — a run at a
tenth of the intended concurrency, reported as though it were the requested
one, is worse than a refusal.

Ramp up. Start well below the target and step towards it. The first run at
full concurrency against an unfamiliar host tells you it broke, at a
concurrency you have no comparison for.

## Know what you started

A load test runs as a detached process that outlives the call that started it.
It has a pid, it keeps running to its `-runtime` deadline, and it keeps
generating load until then.

- Report the pid when you start one.
- Stop it when the question is answered, rather than leaving it to expire.
- `stop_load_test` only accepts a pid this tool started. That is a guard
  against sending a signal to something else on the machine, not an
  inconvenience to route around.

## Do not test what you were not asked to test

A workflow that creates records creates them every iteration, hundreds of
times. Before running one at concurrency, check whether the transactions it
performs are ones the user intends to perform thousands of times. Read-only
enquiries are usually safe; anything that writes is a question to ask first.
