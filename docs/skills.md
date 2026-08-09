---
description: >-
  The procedures the AI assistant follows are files, not prose compiled into
  the binary — teach it about your own hosts without waiting for a release.
---

# Skills and Extensions

The procedures the AI assistant follows are files, not prose compiled into the
binary. That means two things: the always-on prompt stays small, and you can
teach it about your own hosts without waiting for a release.

## How it fits together

A **skill** is a playbook — how to ramp a load test, how to read a set of
percentiles. An **instruction** is a policy fragment several skills share.
An **extension** is a folder bundling both, plus saved workflows.

The prompt carries an *index*: each skill's name and one line. The assistant
calls `load_skill` when it decides it needs one, and `load_instruction` for
the fragments that skill cites. Loading a playbook for work nobody asked for
would cost the same as loading one that was needed.

## What ships

| Skill | For |
|---|---|
| `ramp-up-load-test` | Running a concurrent load test, from first smoke run to a quotable result |
| `find-concurrency-knee` | Finding where a host stops scaling, by stepping up and comparing |
| `soak-test` | Holding a steady load for hours to surface leaks and gradual slowdown |
| `interpret-results` | Turning counters and percentiles into something actionable |
| `author-workflow` | Writing workflow JSON against the schema rather than from memory |
| `before-after-comparison` | Measuring whether a change moved throughput or latency, holding everything else still |

| Instruction | Covers |
|---|---|
| `load-test-safety` | Confirming the target, the caps, and knowing what you started |
| `reading-latency` | Rolling windows, percentiles over means, what the numbers cannot say |
| `workflow-authoring` | Step keys, checks, placeholders, pacing, disconnecting |

`list_skills` and `list_instructions` show what an installation actually has,
including anything you added.

## Adding a skill

Drop a folder beside the binary:

```
3270Connect
skills/
  our-billing-load/
    SKILL.md
```

```markdown
---
name: our-billing-load
description: Run the overnight billing load profile against the test region, with the concurrency and pacing we use for release sign-off.
invocation: [billing-load, release-signoff]
tools: [validate_workflow, start_load_test, get_load_test_metrics]
instructions: [load-test-safety]
---

# Billing load profile

## When to use

Signing off a release against the billing region.

## Steps

1. Use `workflows/billing.json`, which has the transaction mix Ops agreed.
2. 40 concurrent users, 20 minutes. Below 15 minutes the batch window has
   not opened and the numbers describe a quiet system.
3. Report p95 against the 2.5s figure in the SLA.

## Anti-patterns

- Running before 02:00. The overnight batch is still going and the numbers
  will be worse than anything a user will see.
```

Only `name` and `description` are required, and `name` must match the folder.
Everything else is optional. Restart 3270Connect and it appears in
`list_skills` with its source shown as `local`.

Instructions go in `instructions/` beside `skills/`, named
`<something>.instructions.md`. A skill cites one by mentioning it in
backticks or listing it in frontmatter.

## Replacing a built-in

A skill with the same name as a built-in does **not** replace it by default —
it is refused and reported, and `list_extensions` says why. To replace one
deliberately, name it:

```yaml
---
name: ramp-up-load-test
description: Our ramp profile, which starts at 20 because below that the region is idle.
overrides: ramp-up-load-test
---
```

Tuning the ramp playbook for your site should be possible; doing it by
accident should not, because the built-ins carry the safety rules.
`list_skills` reports the replacement.

## Extensions

An extension bundles skills, instructions and workflows so a team can share
one folder:

```
extensions/
  acme-billing/
    3270-extension.json
    skills/
      acme-billing-load/SKILL.md
    instructions/
      acme-conventions.instructions.md
    workflows/
      billing.json
```

```json
{
  "schemaVersion": 1,
  "name": "acme-billing",
  "version": "1.0.0",
  "displayName": "ACME billing load pack",
  "description": "Load profiles and conventions for the ACME billing region.",
  "requires": { "product": "3270Connect", "minVersion": "1.9.0" },
  "contributes": {
    "skills":       [{ "dir": "skills/acme-billing-load", "name": "acme-billing-load" }],
    "instructions": [{ "file": "instructions/acme-conventions.instructions.md" }],
    "workflows":    [{ "file": "workflows/billing.json" }]
  }
}
```

Install by unzipping into `extensions/` and restarting. Disable one without
deleting it by listing its name in `extensions/.disabled`, one per line.

`list_extensions` shows every pack found, whether it is enabled, and **why
one failed to load** — which is the answer when a skill someone expects is
missing from `list_skills`.

## What an extension may contribute

Content only: skills, instructions and workflow documents.

A contributed workflow appears in `list_workflows` alongside the files in the
working directory, with its source named, and goes through the same
`validate_workflow` gate as one you wrote yourself — a pack cannot ship a
workflow that skips the check.

There is deliberately no way for an extension to register a command, a script,
or an executable tool. 3270Connect generates load against production systems,
and "drop a folder in and it runs" is not a security model an operator can
reason about. Everything an extension needs is already a built-in tool it can
direct the assistant to use.

Treat an extension as you would any other trusted local content: there is no
signing, so install ones you or your team wrote.

## Rules the loader applies

- Every contributed path must resolve **inside** the extension folder.
- A skill's frontmatter `name` must match what the manifest declares.
- `schemaVersion` must be `1`; `requires.minVersion` must not be newer than
  the running version.
- A pack that fails any of these is **skipped and reported**, never
  half-loaded. One malformed manifest does not stop 3270Connect starting.
