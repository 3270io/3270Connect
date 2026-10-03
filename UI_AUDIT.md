# 3270Connect UI, user-flow and CLI audit

Date: 3 October 2026. Source: supplied `repomix-output-3270io-3270Connect.xml` snapshot. No fixes implemented.

## Scope and evidence

Reviewed the documentation landing page and installation paths, dashboard template/CSS/JavaScript, authentication templates and routing, administration templates/JavaScript, launch handlers, CLI flag registration/main dispatch, workflow completion and terminal rendering, installer, and CLI documentation. Paths below are original repository paths.

This is an open-source, self-hosted automation tool, not a SaaS trial funnel. No registration, billing, pricing, paywall, upgrade modal or upsell implementation was found in the reviewed surfaces. The conversion goal is **install → first successful replay → repeat use**, with account provisioning for shared instances. Adding trial gates would damage that funnel.

Execution limits: `go` is absent. Playwright is installed, but Chromium's executable is absent; a headless launch failed. Consequently no live app, screenshots, real mainframe session, or rendered 1440px/375px walkthrough is claimed. Viewport findings are source-derived: deterministic CSS/DOM defects are identified as such; layout risks require browser confirmation. Terminal findings are source-derived, at an 80-column baseline and narrow/mobile SSH widths.

Source walkthrough:

| Path | First-time experience | Value and choice handling |
| --- | --- | --- |
| Landing → Install → binary/Docker | A strong category-specific headline leads into deployment choices, then commands referring to a workflow the user must supply. | No guided replay in the ordinary install path. |
| Landing → Lab | Lab supplies a sample host and workflow; this is the closest path to immediate value. | It is buried behind installation choices rather than being the obvious evaluation route. |
| Console, authentication off | No account needed; dashboard opens with metrics and an upload-based launch dialog. | Existing workflow required; sample app launch does not complete the replay journey. |
| Console, local authentication | Setup code from server logs → administrator creation → sign-in; other accounts are provisioned by an administrator. | This is deployment bootstrap, not goal-based product onboarding. No unused goal questionnaire found. |
| Console, OIDC | Organisation sign-in or local password → console, subject to access configuration. | Login offers the branch; no separate first-run assistance follows. |
| Temporary password | Forced password replacement before console access. | Valid security gate, but copy adds speculation and unnecessary explanation. |
| CLI | Install → run command → configuration load → replay → summary. | Help discovery, naming, exit status and process lifetime undermine the automation promise. |
| Upgrade/payment | No such path found. | No early paywall, simultaneous upsell or free-tier nagware finding. |

26 findings: **7 Critical, 14 High impact, 5 Nice to have**. Severity reflects activation, task completion and trust, not how visually dramatic a defect looks.

## 1. Critical — blocks conversion or breaks the experience

### C1. Mobile CSS erases primary action labels
- **Pass:** Designer / First-time user.
- **Where:** `templates/static/css/dashboard.css`, `@media (max-width: 780px)`; `templates/dashboard.gohtml`, topbar and modal footers; `/dashboard`, 375px.
- **Problem:** `.btn-x .txt { display: none; }` applies across the page. Start Process, Start App, Launch run, Test connection and Terminate lose their labels. Primary actions become ambiguous icon-only controls, and those lacking explicit `aria-label` lose their accessible text. Cancel remains labelled, making the footer hierarchy inconsistent. A rocket is not an instruction to a new user. This is a deterministic source defect, not a screenshot inference.
- **Fix:** Restrict icon-only treatment to explicitly labelled secondary toolbar buttons. Keep primary and destructive actions textual at all widths; add `aria-label` where a control intentionally becomes icon-only. Use wrapping or an overflow menu for the topbar, with at least 44px touch targets. Verify every dialog footer at 375px.

### C2. The first workflow example is invalid JSON
- **Pass:** First-time user.
- **Where:** `docs/basic-usage.md`, Single Workflow JSON block; `templates/static/js/dashboard.js`, `handleConfigFile()`; `go3270Connect.go`, `startProcessHandler()`; CLI `-config`, `/dashboard`; all viewports/terminal widths.
- **Problem:** The copyable example includes `// optional` comments. Browser `JSON.parse` and Go `json.Unmarshal` reject them. It also points to a private example address rather than a bundled sample target. Following the manual literally produces failure before value.
- **Fix:** Make all copyable workflow blocks strict JSON and put commentary outside the block. Provide a downloadable, validated sample workflow aimed at the documented sample app, with a distinct lab variant using its service name. Put the exact host/start/run instructions before the flag reference. Validate documentation examples against the workflow validator in CI.

### C3. The ordinary console has no complete first-replay path
- **Pass:** First-time user.
- **Where:** `templates/dashboard.gohtml`, `procEmpty`, Start Process and Start App dialogs; `templates/static/js/dashboard.js`, `renderTable()`, `startApp()`; `docs/installation.md`, `docs/install.sh`; `/dashboard`, 1440px and 375px; fresh CLI install.
- **Problem:** The empty state tells users to start a run or app, but launch requires an existing JSON file. Starting a sample host only reports a port; it does not supply the matching workflow, prefill the run or take the user to a replay. Ordinary installer success commands reference `workflow.json` without creating it. The lab has a sample file, but the ordinary browser path does not use it. Installation is being mistaken for activation.
- **Fix:** Add a prominent “Run a sample workflow” empty-state action: select a bundled app, start/verify that host, load its matching validated JSON, use one worker, launch, then focus its result and capture. Offer “Use my workflow” as the secondary path. In lab mode reuse the existing sample host and server-side sample file instead of requiring an upload. Give CLI users an explicit sample-generation or demo command and print the exact next command after installation.

### C4. Launch success is reported before the process starts
- **Pass:** First-time user.
- **Where:** `go3270Connect.go`, both branches of `startProcessHandler()`; `templates/static/js/dashboard.js`, `startProcess()` and `startApp()`; POST `/start-process`, `/dashboard`, both viewports.
- **Problem:** The handler launches a goroutine and returns HTTP 200 with “started successfully” before `cmd.Start()`/`cmd.Run()` succeeds. The UI closes the dialog and celebrates. A missing executable or occupied sample port can therefore look successful while nothing useful appears. There is no returned run identifier for following the launch.
- **Fix:** Start the child synchronously and return its PID/run ID, or return 202 with a tracked launch ID and explicit pending state. Report start failures inline. For sample hosts confirm readiness before saying “Listening”. Focus the new run and distinguish starting, running, completed and failed states; reserve replay success for a completed workflow.

### C5. A finished CLI load run can keep CI hanging forever
- **Pass:** First-time user.
- **Where:** `go3270Connect.go`, `main()`, automatic `runDashboard()` and final `if concurrent > 1 && dashboardStarted { select {} }`; `docs/basic-usage.md`, `docs/terminal-ui.md`; CLI concurrent runs, any terminal including non-TTY CI.
- **Problem:** Concurrent runs start the dashboard automatically and, after completion, can block indefinitely to keep it open. `-headless` controls the emulator, not process lifetime. A user adopting the advertised CI use case can finish the workload yet never finish the job.
- **Fix:** End workflow commands after cleanup and summary by default. Make persistent console lifetime an explicit `-keepDashboard` option; never enable it implicitly for non-interactive execution. Keep `-dashboard` as the intentional server mode. Verify a finite concurrent run exits without input under a CI runner.

### C6. Workflow failures do not produce a reliable failing CLI exit status
- **Pass:** First-time user.
- **Where:** `go3270Connect.go`, `main()`, `runWorkflow()`, `printSingleWorkflowSummary()`, `runConcurrentWorkflows()`; CLI replay/CI, all terminal widths.
- **Problem:** Configuration failures explicitly exit nonzero, but the reviewed replay completion paths print a summary and return without deriving an exit status from failed workflows or connection failures. Single-run execution can report failure and still return success to its caller; concurrent execution additionally has C5. This undermines the core claim that a replay is useful in CI.
- **Fix:** Return a structured run result from execution and map it to documented exit codes after cleanup: success, configuration error, connection failure, workflow failure and interrupted run. Include connection failure in the default failed outcome; allow an explicit best-effort policy for exploratory load runs. Test exit codes with refused connections and failed assertions, not just printed text.

### C7. A green connection test proves TCP reachability, not workflow readiness
- **Pass:** First-time user.
- **Where:** `go3270Connect.go`, `testConnectionHandler()`; `templates/static/js/dashboard.js`, `testConnection()`, `paintSteps(3)`; `/test-connection`, launch dialog, both viewports.
- **Problem:** The handler only opens and closes a TCP socket. The UI marks Verify complete, although no TN3270 negotiation, TLS certificate validation, model compatibility, login or workflow assertion has been tested. A listener on the wrong service can earn a reassuring green check.
- **Fix:** Rename the current check “Check TCP reachability” and state its limit. Add a distinct terminal preflight using the effective workflow TLS/model/code-page settings, with a timeout and negotiated result. Keep login/assertion verification as a sample replay, not an implied guarantee from an open socket.

## 2. High impact — significant friction or lost trust

### H1. Landing-page proof looks like invented benchmark data
- **Pass:** Designer.
- **Where:** `docs/index.md`, hero KPI strip; documentation `/`, 1440px and 375px.
- **Problem:** Hardcoded 97.1%, 0.34s, 2,381 and 2,313 are not identified as illustrative or accompanied by reproducible conditions. The completed/finished wording is also confusing. “Dependencies: 0” obscures bundled s3270 and platform prerequisites. Precise-looking numbers without provenance weaken enterprise trust.
- **Fix:** Label them “Example run”, link the exact workload/environment, and use consistent counters. Replace the dependency claim with “Bundled emulator; no separate emulator install”, with a nearby platform support link. Prefer actual sample output over unsupported performance claims.

### H2. The landing page has three exits and hides its best evaluation path
- **Pass:** Designer / First-time user.
- **Where:** `docs/index.md`, hero actions; `docs/installation.md`, Lab section; documentation `/`, both viewports.
- **Problem:** Install it, See it work and Basic usage compete for attention. The no-mainframe lab is the strongest way to experience value but is not the hero's primary path. The opening paragraph leads with implementation detail before a clear evaluation action.
- **Fix:** Make “Try the sample lab” the primary CTA and “Install for my host” secondary; keep video as a modest text link. Add concise qualification: free/open source, local evaluation, Docker required for the lab, no mainframe required. Link straight to the exact lab command.

### H3. The homepage repeats generic brochure prose after making its point
- **Pass:** Designer.
- **Where:** `docs/index.md`, Introduction, Features, Getting Started and Conclusion; documentation `/`, both viewports.
- **Problem:** A specific hero and demo are followed by generic “robust toolkit”, “enhancing productivity” and repeated feature lists. This is the part that reads like AI-generated marketing. It inflates scrolling and makes the next action disappear.
- **Fix:** Remove duplicated introduction/features/conclusion sections. Keep the hero, labelled demo, three concrete use cases, prerequisites and one repeated evaluation CTA. Put long capability inventories in reference documentation.

### H4. Recorded-session positioning lacks an in-product recording handoff
- **Pass:** First-time user.
- **Where:** `docs/index.md`, `docs/basic-usage.md`, `docs/chaos-mode.md`, `templates/dashboard.gohtml`; landing → `/dashboard`, both viewports.
- **Problem:** The hero promises a recorded session turned into a workflow, while this console asks for JSON. Recording/export and AI interaction involve the separate 3270Web product. The boundary is not clear at the moment users need a file.
- **Fix:** Add “Create a workflow in 3270Web” beside upload, explain the separate application, and link to its recording/export instructions. Document record → export → import as one path. Do not suggest a recorder exists in this console until it does.

### H5. First launch defaults to a three-minute, five-worker load test
- **Pass:** First-time user.
- **Where:** `templates/dashboard.gohtml`, Load profile; `/dashboard`, launch dialog, both viewports.
- **Problem:** Defaults are concurrent=5 and runtime=180. A novice trying to verify one workflow is steered into repeated concurrent traffic, extra waiting and noisier failures. The CLI default is one workflow, so the entry points disagree.
- **Fix:** Introduce explicit “Replay once” and “Load test” modes, with one replay selected initially. Map replay to the existing single-workflow runtime semantics server-side; expose concurrency, ramp-up and duration only for load mode. Show the target and expected workload before submission.

### H6. Invalid uploads remain launchable and errors are transient
- **Pass:** First-time user.
- **Where:** `templates/static/js/dashboard.js`, `handleConfigFile()`, `startProcess()`; launch dialog, both viewports.
- **Problem:** A parse failure sets `parsedConfig=null` but leaves the file selected; launch checks file presence and native validity, not parsed/schema validity. Even `null` or a structurally wrong JSON value is not a usable workflow. The warning toast disappears while the user is still deciding what to do.
- **Fix:** Track reading/invalid/valid states, require an object matching the workflow schema, and disable launch until valid. Keep a field-level parse/validation error with location and repair guidance. Reset validation on every file change and ignore late reads from replaced files. Keep server validation authoritative.

### H7. Config, Review, Verify are decorative rather than a trustworthy stepper
- **Pass:** First-time user.
- **Where:** `templates/dashboard.gohtml`, `startSteps`; `templates/static/js/dashboard.js`, `handleConfigFile()`, `testConnection()`, dialog show hook; launch dialog, both viewports.
- **Problem:** Parsing advances two steps; a TCP check advances three. Changing host or port does not invalidate the previous green verification, and all fields remain one large form. The apparent progress is stronger than the evidence behind it.
- **Fix:** Either use real stages with a clear current stage and validation gates, or replace the stepper with truthful status chips. Bind verification to an effective-configuration fingerprint and invalidate it when relevant fields change. Mark optional verification optional.

### H8. The empty dashboard prioritises zero charts over getting started
- **Pass:** Designer / First-time user.
- **Where:** `templates/dashboard.gohtml`, KPI strip, chart grid, process section; `templates/static/css/dashboard.css`, `.kpi-strip`; `/dashboard`, both viewports.
- **Problem:** Six KPI tiles and several charts precede the process empty state. At 375px the 210px minimum grid width yields a single column: six empty tiles consume substantial vertical space before users reach an explanation. At 1440px the duration chart spans the full chart grid. Expertise is assumed before there is any data.
- **Fix:** On an instance with no runs, put the sample-replay welcome panel above analytics and collapse empty charts. After the first run, show an overview with active, completed, failed and duration first; put detailed analysis behind expandable sections. Preserve expert access without making novices scroll through zeros.

### H9. Mobile process cards remove the identity needed to choose a run
- **Pass:** Designer / First-time user.
- **Where:** `templates/static/js/dashboard.js`, `renderCards()`; `templates/static/css/dashboard.css`, mobile `.card-view`; `/dashboard`, 375px.
- **Problem:** Cards contain PID, status and four counters, but omit workflow name, host, parameters and duration. Two runs with similar counts become indistinguishable unless users recognise operating-system PIDs.
- **Fix:** Include uploaded workflow display name, host:port, launch time and duration in each card. Make PID secondary and expose concise labelled actions in a menu. Retain essential identity when switching between table and card views.

### H10. Session expiry is presented as a broken server
- **Pass:** First-time user.
- **Where:** `templates/static/js/dashboard.js`, `Refresh.fetchData()` versus `templates/static/js/admin.js`, `api()`; `/dashboard/data`, `/dashboard`, both viewports.
- **Problem:** Dashboard polling treats HTTP failures or a redirected login HTML response as connection errors and eventually shows an offline banner. Admin requests already have a 401 redirect path. Users can keep waiting for a server that is fine while their session has expired.
- **Fix:** Give dashboard data endpoints a consistent JSON 401 response. Share request handling that distinguishes expired session, denied access and transport failure. Offer sign-in with a preserved return path and restore safe UI choices; do not discard an unsent workflow unnecessarily.

### H11. Sign-in leaves unprovisioned or locked-out users without a next step
- **Pass:** First-time user.
- **Where:** `templates/auth/pages/login.gohtml`, `templates/auth/layout.gohtml`; `/login`, both viewports.
- **Problem:** There is no account-request or password-recovery guidance, and the source explicitly omits it. This does not need public self-registration, but a new colleague cannot discover that an administrator must create/reset their account. A rejected login becomes a dead end.
- **Fix:** Add a compact “Need access or a password reset? Contact your instance administrator” help control, optionally configured with the instance support URL. Link a deployment-appropriate recovery guide without exposing account existence. Keep public registration off unless deliberately supported.

### H12. Administration dialogs do not match the dashboard's keyboard behaviour
- **Pass:** Designer / First-time user.
- **Where:** `templates/static/js/admin.js`, `openDialog()`/`closeDialog()`; auth admin page modal templates; `/admin/users`, `/admin/groups`, `/admin/tokens`, both viewports and keyboard use.
- **Problem:** Admin dialogs simply toggle `hidden`; they lack the dashboard manager's focus trap, Escape handling, focus restoration and body scroll lock. Focus can leave a supposedly modal account/token task. Confirmation also falls back to native browser dialogs.
- **Fix:** Share a dialog primitive with initial focus, focus containment, inert background, Escape/cancel behaviour and focus restoration. Use an in-app destructive confirmation naming the affected entity. Preserve copyable one-time secrets until explicit dismissal.

### H13. CLI help is a flag dump and can fail before displaying help
- **Pass:** First-time user.
- **Where:** `go3270Connect.go`, `init()` flag registration and `main()`; `authcli.go`, `mcp.go`; CLI `-help`/`-version`, 80 columns and narrow terminals.
- **Problem:** Explicit `-help` and `-version` reach `auth.configure()` before their branches, so invalid authentication configuration can prevent these commands working. Help uses default `flag.Usage()` and does not offer a task-oriented index covering workflow, console, sample host, user, token and MCP paths.
- **Fix:** Handle help/version immediately after parsing, before authentication, listeners, housekeeping and decorative output. Provide grouped task help with three runnable examples and subcommand links. Keep detailed flags available through an extended help mode and send successful help/version to stdout.

### H14. Installer and documentation disagree on executable casing
- **Pass:** First-time user.
- **Where:** `docs/install.sh`, `COMMAND_NAME="3270connect"` and success output; `docs/basic-usage.md`, `docs/index.md`, `authcli.go` usage strings; Linux CLI, all widths.
- **Problem:** The one-command installer publishes lowercase `3270connect`, but prominent usage examples and account command help use `3270Connect`. Linux command names are case-sensitive. A correctly installed user can get “command not found” from copy-pasting the next documentation step.
- **Fix:** Standardise Linux commands on `3270connect` throughout docs and usage output. Keep Windows examples explicitly `3270Connect.exe`. Derive help's command name from the invoked binary where appropriate and verify installation-to-first-command examples together.

## 3. Nice to have — polish

### N1. The console overuses decorative motion and uppercase microtype
- **Pass:** Designer.
- **Where:** `templates/static/css/dashboard.css`, backdrop grid drift, scanlines, mark sheen, typography; `templates/static/js/dashboard.js`, preferences default `fx:'on'`; `/dashboard`, both viewports.
- **Problem:** Mainframe branding is distinctive, but ambient gradients, glass panels, sheen, glow and CRT effects compete with operational data. Tiny tracked uppercase metadata and mobile labels are harder to scan than sentence case. This is a visual-design judgment grounded in CSS, not a claim of measured contrast failure; reduced-motion support already exists.
- **Fix:** Default decorative FX off, retain the theme palette, and use a solid hierarchy of surfaces. Reserve motion for state changes. Use sans-serif sentence case for instructions and labels; keep mono for terminal data. Raise essential metadata to at least 12–14px and verify contrast across all four themes.

### N2. Tooltip-sized controls are too small for touch tasks
- **Pass:** Designer.
- **Where:** `templates/static/css/dashboard.css`, `.btn-x.sm.icon` and `.btn-close` (30px); `templates/dashboard.gohtml`, chart and dialog controls; `/dashboard`, 375px/coarse pointer.
- **Problem:** Desktop-sized controls persist on touch screens. Hover explanations do not make an unlabeled icon self-explanatory on a phone, and adjacent 30px targets invite wrong taps.
- **Fix:** Add coarse-pointer sizing of at least 44×44px and sufficient gaps. Use accessible names and tap-accessible help for complex controls. Move low-priority chart controls to a labelled overflow menu.

### N3. Forced-password and setup copy lectures rather than helps
- **Pass:** Designer / First-time user.
- **Where:** `templates/auth/pages/setup.gohtml`, `change-password.gohtml`, `admin-users.gohtml`; `/setup`, `/account/password`, `/admin/users`, both viewports.
- **Problem:** Password composition theory, “probably sent it over chat” and long explanations of administrator protection add noise and speculation. These are forms for completing a task, not essays about implementation policy.
- **Fix:** State “At least N characters” beside the password field. Explain forced replacement as “Your temporary password must be replaced before you continue.” Move policy detail to contextual help and keep protections explained beside disabled actions.

### N4. Terminal output needs a compact and non-interactive presentation
- **Pass:** Designer / First-time user.
- **Where:** `go3270Connect.go`, `formatLiveStatsRow()`, `formatPowerupRow()`, `clear()`, `promptToContinueWaiting()`; CLI 80 columns, narrow SSH and non-TTY CI.
- **Problem:** Fixed column widths plus multiple emoji and separators can exceed narrow terminal widths; `clear()` emits ANSI screen-clear sequences and grace prompts redraw with carriage returns regardless of TTY. `-headless` does not select a plain log format. Exact clipping is unrendered, but the unconditional escape output is confirmed.
- **Fix:** Detect terminal width and use stacked compact rows below the full-table threshold. Honour non-TTY and `NO_COLOR`, skip clears/redraws, and print deterministic progress lines. Add explicit output/non-interactive options; when stdin is not a terminal, apply the shutdown policy without asking a question.

### N5. Sorting and transient notifications have uneven accessibility
- **Pass:** Designer.
- **Where:** `templates/dashboard.gohtml`, sortable `<th>` cells; `templates/static/js/dashboard.js`, `bindTable()`; `templates/static/js/admin.js`, `toast()`; admin `.toast` templates; both viewports and keyboard/screen-reader use.
- **Problem:** Process sorting is attached to clicks on non-focusable table headers. Admin notifications change a plain div without a live-region role, unlike auth error alerts. Keyboard and assistive-technology users get a weaker version of the same product.
- **Fix:** Put a real button inside sortable headers and maintain `aria-sort` on the header. Use a persistent `role="status"`/polite live region for successful notifications and assertive error feedback where appropriate. Keep actionable errors inline rather than relying on an expiring toast.

## Recommended implementation order

Fix the documentation sample and executable naming first: small changes immediately repair the first-run path. Fix CLI termination/exit status and mobile primary labels next. Then deliver the guided sample replay and tracked launch result. Improve validation and truthful preflight before polishing the landing page and decorative styling.

Before declaring the UX repaired, run browser checks at 1440px and 375px for fresh/empty, sample, running, completed, invalid-file, expired-session and failed-launch states. Run CLI checks on 80-column and narrow TTYs and redirected stdin/stdout, including finite concurrent completion, failed assertions, refused connections, help with invalid auth config, and exact installer-generated commands. These are future acceptance checks, not tests performed in this audit.
