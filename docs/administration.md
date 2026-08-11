---
seo_title: "The 3270Connect administration area — accounts, groups and audit"
description: >-
  Administrators get /admin: an instance overview, account and group
  management, API tokens, every load run on the machine, and the audit trail
  of who did what.
---

# The Administration Area

Administrators get an **Administration** area at `/admin`. Six pages, each
answering one question:

| Page | Answers |
|---|---|
| [Overview](#overview) | Is everything all right? |
| [Accounts](#accounts) | Who has an account? |
| [Groups](#groups) | Which teams are there? |
| [API tokens](#api-tokens) | What automated clients can reach this console? |
| [Load runs](#load-runs) | What is running right now, and whose is it? |
| [Audit trail](#audit-trail) | What has happened? |

Everything under `/admin` requires the `admin` role. The gate is applied to the
whole prefix rather than route by route, so a page added later cannot forget to
opt in.

!!! info "It works without accounts too"
    Under `AUTH_MODE=none` there is one operator who may do everything, so the
    area opens and the Load runs and Audit trail pages are useful immediately.
    The Accounts, Groups and Tokens pages say what to set to make them
    meaningful. See [Accounts and Sign-In](authentication.md).

---

## Overview

The front page is the state of the instance: how many accounts exist, who holds
a live sign-in, how many load runs are going, how many tokens are active, and
how many requests were refused in the last 24 hours — followed by the most
recent audit entries. The numbers refresh themselves every half minute while
the page is open.

The **Refused (24h)** tile is the one to look at. A handful is somebody
mistyping a password; a run of them is the shape of something else.

## Accounts

`/admin/users` is where you add accounts, change roles, reset passwords,
disable and re-enable people, and delete accounts. The list can be filtered by
name or group, or narrowed to administrators, disabled accounts, accounts that
still owe a password change, or accounts that arrive by single sign-on.

A few actions are deliberately unavailable, because each would strand you on a
page you could no longer use, with no way back except the console command:

- Removing your own administrator role
- Disabling or deleting your own account
- Demoting, disabling or deleting the last enabled administrator
- Leaving, or clearing the role of, the group your own administration comes
  from

Disabling an account, resetting its password or deleting it signs that person
out everywhere immediately. **Deleting an account also revokes every token it
holds** — disabling deliberately does not, because the same live lookup already
refuses a disabled account's tokens, and re-enabling should give somebody their
automated clients back rather than making them reissue everything.

A password an administrator sets is temporary: its owner is asked to choose
their own the next time they sign in, and until they do, the only page that
account can reach is the password form.

The **Add account** and **Reset password** dialogs both offer **Generate**,
which invents a 20-character password from the browser's cryptographic random
source and reveals it so you can read it out or hand it over — **Copy** puts it
on the clipboard. It is worth using: the alternative is a password you thought
of, which in practice is the same one for every account you create. The
generated alphabet leaves out `O`/`0` and `I`/`l`/`1`, because a temporary
password exists to be transcribed by somebody else.

Nothing shows the password again after the account is saved. If it is lost
before it reaches its owner, reset it — that is what **Reset password** is for.

**Changing a role takes effect immediately, in whatever browser that person
already has open.** It does not wait for them to sign in again, and it does not
sign them out. Demotion is the direction that matters: a demoted administrator
who kept the role until their session expired could restore it from the page
they were still standing on.

### Roles from groups

Below the account list, **Roles from groups** shows which teams grant
administration and lets you grant or revoke it in one click. The account list
above reports the result honestly: somebody who administers because of a group
wears the same Administrator badge with *via ‹group›* under it, and the
administrator count on the overview counts them.

## Groups

`/admin/groups` is where a group is made and maintained. One row per team, and
one dialog that does the whole job:

| Field | What it does |
|---|---|
| **Name** | What the group is called. No commas — a comma separates one group from the next everywhere a list of them is written |
| **Description** | An optional note, shown beside the name in the table |
| **Role granted** | The role every member holds on top of their own |
| **Members** | The accounts in the group, ticked from the account list |

**A group may be empty.** A console is usually set up teams-first — the rota is
written before the people arrive — and a group that only existed while somebody
was in it could not be prepared in advance.

**Renaming a group carries everything with it**: its members and the role it
grants. Renaming is what to do when the same team ends up spelled two ways; the
old name stops existing rather than lingering as an empty group beside the new
one.

**Deleting a group** removes it from every account in it and drops the role it
granted. The accounts themselves are untouched — only the group is.

Groups that were never declared here are listed alongside declared ones, marked
**in use**: those are names that exist only because an account carries one or a
role is assigned to one, including the groups an identity provider sends. They
can be described, renamed, filled and deleted like any other.

Where [single sign-on](authentication.md#single-sign-on-oidc) maps a groups
claim, membership of a directory-owned account belongs to the directory: those
accounts appear in the member list marked **single sign-on** and cannot be
ticked here, because the next sign-in would overwrite the change. Change them
in the directory.

Creating, changing and deleting a group is written to the audit trail as
`group.created`, `group.updated` and `group.deleted`.

## API tokens

`/admin/tokens` lists every token issued on this console — who owns it, what it
was issued for, its scopes, whether it is still active and when it was last
used — and issues new ones.

The secret is shown once, in a dialog, and never again: the store keeps a hash,
so a lost token is replaced rather than recovered. Closing the dialog clears it
from the page rather than leaving it in the DOM of a tab somebody may leave
open on a shared screen.

Everything on this page is also a console command, which is usually easier
inside a container:

```bash
docker compose exec 3270connect /app/3270Connect token add alice "nightly soak"
```

See [API tokens](authentication.md#api-tokens) for what a token reaches and how
scopes work.

## Load runs

`/admin/runs` lists every load run this machine is publishing metrics for,
whoever started it: the process id, the owner, how many virtual users are
active, how many workflows have finished and failed, when it started and the
parameters it was given. The list refreshes itself every few seconds.

**Stop** ends a run and frees its capacity. This is the page for a run somebody
left pointed at a production region, and for reclaiming a machine that is
saturated.

Two rows behave differently, and the page says so:

- A run started from a command line rather than from the console has **no owner
  recorded**, because nothing asked the console for it.
- The console's own process appears here too — it publishes a metrics file like
  any other — marked *serving this page*, with Stop disabled. Stopping it would
  stop the page you are reading.

Every stop is written to the audit trail with whose run it was, so "who stopped
my soak test" is answerable from both ends.

## Audit trail

`/admin/audit` is the security record, kept apart from the debug log because it
is read for a different reason and for far longer. One JSON object per line,
append-only, holding only what an administrator would be asked about months
later.

| Event | Recorded when |
|---|---|
| `login.succeeded`, `login.failed`, `login.locked_out`, `logout` | Somebody signs in, fails to, is throttled, or signs out |
| `account.created`, `account.updated`, `account.deleted`, `account.password_changed`, `account.first_admin_created` | An account changes |
| `group.created`, `group.updated`, `group.deleted`, `group.role_changed` | A group changes |
| `token.issued`, `token.revoked`, `token.refused` | A token is issued, revoked, or presented and refused |
| `run.started`, `run.killed` | A load run is started or stopped |
| `workflow.executed` | A workflow is run through the REST API |
| `connection.tested` | Somebody dials a host to see whether the port answers |
| `sampleapp.started` | A bundled sample application is started as a TN3270 host |

Each line records who, from where, what was acted on, whether it worked and a
few named details. **Never a password, a token, or the contents of a screen**:
the point of the file is that an administrator can read it, which means
anything in it is disclosed to every administrator for as long as it exists.

The page shows the newest 500 entries and filters them by text or by outcome.
**Download all of it** streams the whole file as newline-delimited JSON, for
keeping or for shipping somewhere that keeps it:

```bash
curl -H "Authorization: Bearer 3270c_…" \
  http://localhost:9200/admin/api/audit.jsonl > audit.jsonl
```

The file rolls over at 8 MiB and one previous generation is kept. Beyond that,
a deployment that needs long retention needs somewhere to ship the lines to,
not a bigger file on the same disk.

!!! note "A refused login says less to the browser than to you"
    The sign-in page answers every failure the same way, because distinguishing
    them would report which usernames exist and which accounts are disabled.
    The trail is allowed the distinction the caller is not: `login.failed`
    carries a `reason` of `bad credentials`, `account disabled` or
    `store error`.

## The JSON behind the pages

The tables are painted from endpoints under `/admin/api`, all of them
administrator-only and all of them usable from a script with a token:

| Endpoint | Method | Does |
|---|---|---|
| `/admin/api/overview` | `GET` | The overview's counters and recent trail |
| `/admin/api/users` | `GET`, `POST` | List accounts; create one |
| `/admin/api/users/<id>` | `PATCH`, `DELETE` | Change role, enabled state, groups or password; delete |
| `/admin/api/groups` | `GET`, `POST` | List groups; create one |
| `/admin/api/groups/<name>` | `PATCH`, `DELETE` | Rename, describe, set members and role; delete |
| `/admin/api/group-roles` | `POST` | Grant or clear the role a group carries |
| `/admin/api/tokens` | `GET`, `POST` | List tokens; issue one |
| `/admin/api/tokens/<id>` | `DELETE` | Revoke a token |
| `/admin/api/runs` | `GET` | Every load run on the machine |
| `/admin/api/audit` | `GET` | The recent trail, `?limit=` up to 2000 |
| `/admin/api/audit.jsonl` | `GET` | The whole trail, oldest first |

## Related pages

- [Accounts and Sign-In](authentication.md) — turning authentication on, roles, tokens and SSO
- [Web Dashboard](dashboard.md) — the console these pages sit beside
- [Installation](installation.md) — where the state directory lives
