---
seo_title: "3270Connect accounts, sign-in, API tokens and OIDC SSO"
description: >-
  The console has no sign-in by default. AUTH_MODE=local adds accounts,
  groups, passwords and per-account API tokens; AUTH_MODE=oidc adds single
  sign-on. Turn it on as soon as the port is shared.
---

# Accounts and Sign-In

By default the operations console has no sign-in. It assumes one operator on a
machine they control, which is right for a laptop, for `go run`, and for the
double-clicked executable — and it is why every install method publishes on
`127.0.0.1`.

Setting `AUTH_MODE=local` turns on accounts: a sign-in page, per-user
passwords, sessions that expire, and an administration area. Turn it on
whenever more than one person can reach the port.

!!! warning "A load run is not a read"
    The console's start dialog points a chosen number of virtual users at a
    chosen host. That is traffic somebody else's mainframe has to absorb, and
    `/kill` stops a run a colleague may be depending on. Those are the two
    things an account boundary is protecting — not a page of graphs.

!!! info "The same settings as 3270Web"
    The variable names, the roles, the group model and the token format are
    deliberately the same as [3270Web](https://3270Web.3270.io)'s. The two are
    frequently run side by side, and nobody should have to learn this twice.

---

## Turning it on

```bash
AUTH_MODE=local
```

Or in `docker-compose.yml`:

```yaml
services:
  3270connect:
    image: ghcr.io/3270io/3270connect:latest
    ports:
      - "127.0.0.1:9200:9200"
    environment:
      - AUTH_MODE=local
    volumes:
      - ./data:/data
```

Three values are accepted: `none` (the default), `local`, and `oidc` for
[single sign-on](#single-sign-on-oidc). Any other value stops startup with an
error rather than quietly running without authentication — a setting that looks
like protection but is not would be worse than none at all.

On a desktop where there is no shell to export a variable from, put it in
`3270Connect.env` beside the executable:

```
AUTH_MODE=local
```

Real environment variables win over that file, so a container can still
override it without editing anything.

## First start

Start with `AUTH_MODE=local` and no accounts, and 3270Connect waits in setup
mode: every page redirects to a one-time setup screen where you create the
first administrator.

To stop the first person who reaches the port from claiming the console, the
form asks for a **setup code** printed in the server log:

```
auth: no accounts yet — open the console to create the first administrator
auth: setup code: EJWQ-RUYN-7XL3-PT3O
auth: the code is required once, and stops working as soon as the account exists
```

The code goes to the log file and to standard error, so under Docker
`docker compose logs 3270connect` shows it. Case, spaces and dashes are all
ignored, so it can be typed however it was copied.

!!! warning "Put the data directory on a volume first"
    A container keeps its accounts in the image layer unless told otherwise, so
    the next deploy would delete the administrator you are about to create and
    reopen first-run setup. See [Where the state lives](#where-the-state-lives).

Open the console in a browser, enter the code, and choose your own username and
password. You are signed in immediately, and setup closes for good.

!!! note "Nothing is created behind your back"
    3270Connect never invents an account or a default password. Until you
    complete setup there are no credentials to leak, and the administrator's
    password is one you chose rather than one printed in a log.

If you would rather not use the web form, create the account with the console
command before starting the server. Setup does not arm when an account already
exists:

```bash
3270Connect user add root --admin
```

## Roles

| Role | May |
|---|---|
| `user` | Sign in, start load runs and workflows, watch every run, and stop **their own** |
| `admin` | The same, plus the [administration area](administration.md) and stopping **anybody's** run |

New accounts are `user` unless `--admin` is given.

Two rules are worth stating plainly, because they are what the roles actually
buy:

- **A run belongs to whoever started it from the console.** Its owner may stop
  it; a colleague may not. An administrator may stop anything, which is how
  capacity is reclaimed from a run somebody left pointed at a production
  region.
- **A run started from a command line has no owner recorded**, because nothing
  asked the console for it. It is therefore an administrator's to stop — the
  fail-closed reading, since "unowned" must not mean "belongs to whoever
  asked".

Every start and every stop is written to the [audit
trail](administration.md#audit-trail), with whose run it was.

## Passwords

At least 12 characters. There are no composition rules — required digits and
symbols shrink the search space more than they enlarge it, while a length floor
does not. Passwords are stored as Argon2id hashes; the plaintext is never
written anywhere.

Changing a password signs out that account's other sessions, which is usually
the point of changing one.

Failed sign-ins are throttled per username **and** per address: five free
attempts, then a lockout that doubles up to fifteen minutes. Username alone
would let an attacker lock out an account they know the name of; address alone
would let a distributed attacker spread guesses across one account. Requiring
both to be clear covers each gap with the other.

## From the command line

Account management is also a console command. It edits the same file the server
reads, so it works whether or not the server is running — a new account can
sign in immediately, without a restart.

```bash
3270Connect user add alice              # create a regular account
3270Connect user add root --admin       # create an administrator
3270Connect user list
3270Connect user passwd alice
3270Connect user disable alice
3270Connect user enable alice
```

Passwords are prompted for on a terminal, or read from stdin when piped:

```bash
printf '%s\n' "$NEW_PASSWORD" | 3270Connect user passwd alice
```

They are never taken as a command-line argument, where they would be visible to
every other process on the machine and recorded in shell history.

Inside a container:

```bash
docker compose exec 3270connect /app/3270Connect user add alice
```

One difference from the web interface: a running server does not see the file
change immediately. Disabling an account from the console command stops its API
tokens at once — the owner is looked up on every call — but a browser already
signed in is ended by a periodic sweep instead, so allow up to five minutes.
Disabling from the Accounts page ends those logins on the spot.

## API tokens

An automated client — a CI job, a scheduled soak test, a script — presents a
token instead of signing in.

```bash
3270Connect token add alice "nightly soak"
3270Connect token add ci "pipeline" --read-only --expires 720h
3270Connect token list
3270Connect token revoke <id>
3270Connect token revoke-all alice
```

The token is shown once, when it is issued. It is stored as a SHA-256 hash, so
a lost token is replaced rather than recovered. Present it as an ordinary
bearer credential:

```bash
curl -H "Authorization: Bearer 3270c_…" http://localhost:9200/dashboard/data
```

A token **belongs to an account and reaches exactly what that account
reaches** — their runs, and nothing belonging to anybody else. It carries the
account's effective role, so a token issued to somebody who administers through
a group administers too.

Two scopes, decided by HTTP method rather than by route, so an endpoint added
later does not default to whatever somebody remembered:

| Scope | Covers |
|---|---|
| `read` | `GET`, `HEAD`, `OPTIONS` — run metrics, logs, captured output |
| `write` | Everything else — executing a workflow, starting a run, stopping one |

`--read-only` issues a token with `read` alone.

A token stops working the moment its account is disabled or deleted; the owner
is looked up on every call rather than trusted from the token.

### The single shared token

Where there is one operator there is nothing to tell apart, so per-account
tokens do not exist. Instead:

| `AUTH_MODE` | `API_TOKEN` | The REST API (`-api`) | The console (`-dashboard`) |
|---|---|---|---|
| `none` | unset | Open — the historical default | Open |
| `none` | set | Requires that token | Open |
| `local` / `oidc` | must be unset | Requires a per-account token | Requires a sign-in |

Setting `API_TOKEN` alongside accounts stops startup with an error. One
credential held by everybody would be a hole straight through the separation
the mode was turned on for, and starting anyway would leave an operator
believing users were separated while one variable said otherwise.

Note the second row: with a single operator, `API_TOKEN` closes the REST API
and leaves the console as it was. The console under `AUTH_MODE=none` is
protected by the interface it binds, not by a credential — if you want a
sign-in on it, that is what `AUTH_MODE=local` is for.

!!! note "MCP needs none of this"
    `3270Connect mcp` speaks the Model Context Protocol over stdin and stdout
    to a client that launched the process. There is no listener and no
    credential: whoever can start the process is already the operator. See
    [MCP Server](mcp.md).

## Groups

A group is a team. It can carry a role, so a console is administered by
"whoever is on the ops rota" rather than by a list of names that goes out of
date.

Groups are made and maintained on the [Groups
page](administration.md#groups). Everyone in a group holds the role it grants
*on top of* whatever their account holds in its own right — an account's
effective role is the stronger of the two. Inheritance is additive only, so
adding somebody to a team can never quietly demote them.

A group may be empty. That is deliberate: a console is usually set up
teams-first, and a group that only existed while somebody was in it could not
be prepared in advance.

## Single sign-on (OIDC)

`AUTH_MODE=oidc` signs people in through an OpenID Connect identity provider —
the directory an organisation already runs — instead of asking them to invent
another password. Accounts appear the first time somebody signs in; nobody has
to be added in advance.

**Local accounts keep working.** This is not an either/or, and the reason
matters: a console whose only door depends on a service it does not run can be
locked out of itself by somebody else's outage or a mistyped setting. First-run
setup still asks for a local administrator, and that account is the way back
in.

### Configuring it

```bash
AUTH_MODE=oidc
OIDC_ISSUER=https://login.example.com/realms/staff
OIDC_CLIENT_ID=3270connect
OIDC_CLIENT_SECRET=…
OIDC_REDIRECT_URL=https://console.example.com/auth/sso/callback
```

Register `OIDC_REDIRECT_URL` with the provider as an allowed redirect URI. It
must be the address a browser actually reaches, and it must end in
`/auth/sso/callback`.

Everything else — the authorization and token endpoints, the signing keys — is
read from the provider's discovery document. The issuer must be `https`, the
one exception being a loopback address so a provider on the same machine can be
tried without a certificate.

3270Connect asks for the authorization code flow with PKCE. There is no
implicit or hybrid flow: the browser only ever carries a one-time code, and the
token exchange happens server to server.

### Roles from the directory

| Variable | Meaning |
|---|---|
| `OIDC_GROUPS_CLAIM` | Which claim carries group membership (default `groups`) |
| `OIDC_ADMIN_GROUPS` | Members of these groups get the `admin` role |
| `OIDC_ALLOWED_GROUPS` | If set, only members of these groups may sign in at all |
| `OIDC_USERNAME_CLAIM` | Which claim to take a display name from |
| `OIDC_SCOPES` | Extra scopes to request alongside `openid` |
| `OIDC_END_SESSION` | Also end the provider's session when signing out here |

Both group lists are comma-separated and matched without regard to case. The
claim may be an array of strings or one space-separated string; either is read.

`OIDC_ADMIN_GROUPS` is re-applied on **every** sign-in, in both directions —
somebody removed from the group is an ordinary user the next time they sign in.
That is the point of managing roles centrally.

Leave it unset and the provider says nothing about roles: everybody arrives as
a `user`, and an administrator promotes people on the Accounts page. Those
promotions survive later sign-ins.

`OIDC_ALLOWED_GROUPS` answers a different question — not what somebody may do
here, but whether they belong here at all. One directory usually serves many
services, and everybody in it being able to start a load test is rarely what
was meant. Somebody outside every listed group is refused, and no account is
created for them.

### What an account looks like

An account is found by the provider's **issuer and subject**, never by name. A
directory renames people, and the subject is the one claim a provider promises
not to recycle. So a rename follows through to the username here and changes
nothing else — the same account, the same tokens, the same audit history.

The username is a display name derived from a claim. Characters the account
store does not accept become dashes, so `alice@corp.example` appears as
`alice-corp.example`.

Two things an administrator can still do, and one they cannot:

- **Disabling works**, and outranks the provider. An account disabled here
  stays out however happily the directory goes on authenticating it.
- **Roles work**, unless `OIDC_ADMIN_GROUPS` is set, in which case the
  directory is the authority.
- **Passwords do not.** An account that signs in through the provider has no
  local password and cannot be given one. A second door the provider knows
  nothing about is one it cannot close when it closes the person's access.

A name a local account already holds is refused rather than linked. Otherwise
anybody who can be named in the directory could take over the break-glass
administrator by being called `root`.

### If the provider is unreachable

The sign-in page says so and offers the password form. Discovery is deferred
until somebody presses the button, so a provider that is down does not stop
3270Connect from starting — which is exactly when the local administrator is
needed.

## Session lifetime

| Variable | Default | Meaning |
|---|---|---|
| `AUTH_SESSION_IDLE` | `30m` | Sign out after this long with no activity |
| `AUTH_SESSION_MAX` | `12h` | Sign out this long after signing in, however active |
| `AUTH_BIND_SESSION_IP` | `auto` | Pin a session to the address that created it |

Sessions live in memory, so restarting the console signs everyone out. That is
deliberate: persisting them would mean writing bearer-equivalent credentials to
disk for a benefit — not having to sign in again after an upgrade — that does
not justify it. **The load runs themselves are unaffected**; they are separate
processes and go on running.

`AUTH_BIND_SESSION_IP` accepts `auto`, `true` or `false`. `auto` enables
pinning on plain HTTP and disables it behind TLS: without TLS the cookie
travels in the clear and can be copied off the wire, so requiring the replay to
come from the same address is most of what stands between a passive
eavesdropper and a usable session. Set it to `false` if people reach the
console through a NAT or VPN whose address changes mid-session, and `true` to
enforce it regardless.

## Behind a reverse proxy

| Variable | Meaning |
|---|---|
| `TRUST_PROXY_HEADERS` | Believe `X-Forwarded-For` and `X-Forwarded-Proto` |
| `TLS_TERMINATED_UPSTREAM` | Assert that something in front terminates TLS, whatever the headers say |

Both are opt-in and must stay that way: the headers are set by whoever sends
the request, so trusting them unconditionally would let any client assert that
its own connection is secure — which is exactly the property the `Secure`
cookie flag is supposed to guarantee.

Without one of them behind a TLS-terminating proxy, the sign-in cookie loses
its `Secure` flag and every rate-limit bucket collapses onto the proxy's
address. The sign-in page notices the common case and says which variable to
set rather than only warning that the connection is not encrypted.

## Where the state lives

Three files, under the same directory the metrics files use — which under
Docker is `/data`, because the image sets `XDG_CONFIG_HOME`:

| File | Holds | Override |
|---|---|---|
| `users.json` | Accounts, groups and group roles | `USERS_PATH` |
| `api-tokens.json` | Issued tokens, as hashes | `API_TOKENS_PATH` |
| `audit.log` | The audit trail, one JSON object per line | `AUDIT_LOG_PATH` |

All three are written `0600` in a `0700` directory, and `users.json` is
replaced atomically — a torn write there would lock every account out at once.

!!! danger "Put them on a volume"
    A container without a volume keeps these in the image layer, which the next
    deploy discards. The symptom is an instance that reopens first-run setup
    after an upgrade, with every account gone.

## What a sign-in does not do

An account boundary decides *who* may use the console. It is one of several
things a shared instance needs, and it is worth being explicit about what it
leaves alone:

- **It does not encrypt anything.** Put TLS in front of the console before
  putting it on a network you do not control.
- **It does not restrict which hosts a workflow may reach.** Anybody who may
  start a run may aim it anywhere the machine can route to.
- **It does not bind the listener for you.** `DASHBOARD_BIND` and `API_BIND`
  still decide what can reach the port, and `localhost` is still the default.

---

## Next

**→ [The administration area](administration.md)** — accounts, groups, tokens,
runs and the audit trail, from the browser.
