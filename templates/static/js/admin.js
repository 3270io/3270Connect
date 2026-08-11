/* 3270Connect — the administration area.
 *
 * One script for six pages. It picks its behaviour from location.pathname
 * rather than from an inline hint on the page, because these pages are served
 * under a content-security policy that forbids inline script — and a policy
 * with an exception carved into it for a variable assignment is not much of a
 * policy.
 *
 * Everything here talks to /admin/api/*. Those endpoints are behind the
 * administrator gate on the server; nothing in this file is a security
 * boundary, and a control it hides is a courtesy rather than a defence.
 */
(function () {
  "use strict";

  /* ---------------------------------------------------------------------
     Small shared helpers
     --------------------------------------------------------------------- */

  const $ = (sel, root) => (root || document).querySelector(sel);
  const $$ = (sel, root) => Array.from((root || document).querySelectorAll(sel));

  /** el builds an element. Text is set with textContent throughout this file,
   *  never innerHTML: usernames, group names, audit targets and workflow
   *  parameters all arrive from somewhere else, and a table is not the place
   *  to find out that one of them contained markup. */
  function el(tag, attrs, ...children) {
    const node = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v === false || v === null || v === undefined) continue;
      if (k === "class") node.className = v;
      else if (k === "text") node.textContent = v;
      else if (k.startsWith("on")) node.addEventListener(k.slice(2), v);
      else node.setAttribute(k, v === true ? "" : v);
    }
    for (const child of children) {
      if (child === null || child === undefined || child === false) continue;
      node.append(child);
    }
    return node;
  }

  let toastTimer = null;
  function toast(message, kind) {
    const box = $("#toast");
    if (!box) return;
    box.textContent = message;
    box.className = "toast" + (kind ? " is-" + kind : "");
    box.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { box.hidden = true; }, kind === "error" ? 8000 : 4000);
  }

  /** api performs a request and unwraps the server's error shape.
   *
   *  A 401 means the login expired while the tab was open. Reloading lands on
   *  the sign-in page, which is more use than a toast saying the same thing
   *  next to a table that will never refresh again. */
  async function api(path, options) {
    const response = await fetch(path, Object.assign({
      headers: { "Content-Type": "application/json", "X-Requested-With": "XMLHttpRequest" },
      credentials: "same-origin",
    }, options || {}));
    if (response.status === 401) {
      window.location.href = "/login?next=" + encodeURIComponent(window.location.pathname);
      throw new Error("signed out");
    }
    let body = null;
    try { body = await response.json(); } catch (_) { /* an empty body is fine */ }
    if (!response.ok) {
      throw new Error((body && body.error) || ("request failed with " + response.status));
    }
    return body || {};
  }

  function row(cells, className) {
    const tr = el("tr", className ? { class: className } : {});
    for (const cell of cells) tr.append(cell);
    return tr;
  }

  function cell(text, className) {
    return el("td", className ? { class: className, text: text } : { text: text });
  }

  function emptyRow(tbody, span, message) {
    tbody.replaceChildren(row([el("td", { colspan: span, class: "empty", text: message })]));
  }

  function chip(text, kind) {
    return el("span", { class: "chip" + (kind ? " chip-" + kind : ""), text: text });
  }

  /** confirmed asks before something irreversible. window.confirm is plain and
   *  unmissable, which is what is wanted for "delete this account". */
  function confirmed(question) {
    return window.confirm(question);
  }

  /** generatePassword invents a temporary password from the browser's
   *  cryptographic random source.
   *
   *  The alphabet leaves out O/0 and I/l/1: this password exists to be read
   *  out or typed by somebody else, and those are the pairs that get
   *  transcribed wrongly. */
  function generatePassword(length) {
    const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_";
    const bytes = new Uint32Array(length);
    crypto.getRandomValues(bytes);
    let out = "";
    for (let i = 0; i < length; i++) out += alphabet[bytes[i] % alphabet.length];
    return out;
  }

  async function copyToClipboard(value) {
    try {
      await navigator.clipboard.writeText(value);
      toast("Copied.", "ok");
    } catch (_) {
      // Clipboard access needs a secure context, which a console on plain HTTP
      // is not. Saying so beats a control that silently does nothing.
      toast("The browser would not allow a copy here — select the text instead.", "error");
    }
  }

  function openDialog(id) { const d = $(id); if (d) d.hidden = false; }
  function closeDialog(id) { const d = $(id); if (d) d.hidden = true; }

  function showError(id, message) {
    const box = $(id);
    if (!box) return;
    box.textContent = message || "";
    box.hidden = !message;
  }

  /* ---------------------------------------------------------------------
     Overview
     --------------------------------------------------------------------- */

  function initOverview() {
    const tile = (name, value, note) => {
      const v = $(`[data-tile="${name}"]`);
      const n = $(`[data-tile="${name}Note"]`);
      if (v) v.textContent = value;
      if (n) n.textContent = note || "";
    };

    async function refresh() {
      let data;
      try {
        data = await api("/admin/api/overview");
      } catch (err) {
        toast(err.message, "error");
        return;
      }

      const accounts = data.accounts || {};
      if (data.authEnabled) {
        tile("accounts", accounts.total,
          `${accounts.admins} administrator${accounts.admins === 1 ? "" : "s"}` +
          (accounts.disabled ? `, ${accounts.disabled} disabled` : "") +
          (accounts.external ? `, ${accounts.external} single sign-on` : ""));
        tile("logins", (data.signedIn || {}).logins,
          `${(data.signedIn || {}).users} account${(data.signedIn || {}).users === 1 ? "" : "s"}`);
        tile("tokens", (data.tokens || {}).active, `${(data.tokens || {}).issued} issued`);
      } else {
        tile("accounts", "—", "no sign-in configured");
        tile("logins", "—", "no sign-in configured");
        tile("tokens", "—", "no sign-in configured");
      }

      const runs = data.runs || {};
      tile("runs", runs.running, `${runs.known} known to this machine`);

      const audit = data.audit || {};
      tile("refused", audit.refused24h, `${audit.total24h} recorded in 24 hours`);
      const card = $('[data-tile-card="refused"]');
      if (card) card.classList.toggle("is-alert", (audit.refused24h || 0) > 0);

      const tbody = $("#recent");
      const entries = audit.recent || [];
      if (!entries.length) {
        emptyRow(tbody, 5, "Nothing recorded yet.");
      } else {
        tbody.replaceChildren(...entries.map(auditRow));
      }

      const uptime = $("#uptime");
      if (uptime && data.startedAt) {
        uptime.textContent = `3270Connect ${data.version} — running since ${data.startedAt}.`;
      }
    }

    refresh();
    // Half a minute: often enough that the page is worth leaving open, rarely
    // enough that it is not a load of its own on a machine under test.
    setInterval(refresh, 30000);
  }

  function auditRow(entry) {
    const actor = entry.actor || {};
    const who = actor.username || actor.userId || "—";
    return row([
      cell(formatTime(entry.time), "nowrap"),
      cell(entry.event, "mono"),
      cell(actor.kind === "api-token" ? `${who} (token)` : who),
      cell(entry.target || "—"),
      cell(entry.outcome, "outcome-" + entry.outcome),
    ]);
  }

  function formatTime(value) {
    if (!value) return "—";
    const when = new Date(value);
    if (isNaN(when.getTime())) return value;
    return when.toISOString().replace("T", " ").slice(0, 19);
  }

  /* ---------------------------------------------------------------------
     Accounts
     --------------------------------------------------------------------- */

  function initUsers() {
    let state = { users: [], groups: [], groupRoles: {}, fromProvider: false };
    let editing = null; // null while adding, otherwise the account being reset

    async function load() {
      try {
        const data = await api("/admin/api/users");
        state = {
          users: data.users || [],
          groups: data.groups || [],
          groupRoles: data.groupRoles || {},
          fromProvider: !!data.groupsFromProvider,
          enabled: !!data.authEnabled,
        };
      } catch (err) {
        toast(err.message, "error");
        return;
      }
      render();
      renderGroupRoles();
    }

    function visible() {
      const needle = ($("#filter").value || "").trim().toLowerCase();
      const scope = $("#scope").value;
      return state.users.filter((u) => {
        if (needle) {
          const haystack = (u.username + " " + (u.groups || []).join(" ")).toLowerCase();
          if (!haystack.includes(needle)) return false;
        }
        switch (scope) {
          case "admin": return u.effectiveRole === "admin";
          case "disabled": return u.disabled;
          case "mustchange": return u.mustChangePassword;
          case "external": return u.external;
          default: return true;
        }
      });
    }

    function render() {
      const tbody = $("#rows");
      if (!state.enabled) {
        emptyRow(tbody, 6, "This console has no accounts: AUTH_MODE is none.");
        $("#add").disabled = true;
        return;
      }
      const list = visible();
      if (!list.length) {
        emptyRow(tbody, 6, "No account matches that.");
        return;
      }
      tbody.replaceChildren(...list.map(userRow));
    }

    function userRow(u) {
      const name = el("td", {},
        el("div", { text: u.username }),
        u.self ? el("div", { class: "small muted", text: "this is you" }) : null,
        u.external ? el("div", { class: "small muted", text: "single sign-on · " + (u.issuer || "") }) : null);

      const roleCell = el("td", {},
        chip(u.effectiveRole === "admin" ? "Administrator" : "User",
          u.effectiveRole === "admin" ? "admin" : null),
        (u.roleGroups && u.roleGroups.length && u.role !== "admin")
          ? el("div", { class: "small muted", text: "via " + u.roleGroups.join(", ") })
          : null);

      const groups = el("td", { text: (u.groups && u.groups.length) ? u.groups.join(", ") : "—" });

      const status = el("td", {},
        u.disabled ? chip("Disabled", "danger")
          : u.mustChangePassword ? chip("Owes a password change", "warn")
            : chip("Enabled"));

      const actions = el("td", { class: "actions" },
        el("button", {
          class: "btn btn-sm", type: "button",
          disabled: u.self || u.external,
          title: u.external ? "This account has no local password" : "",
          onclick: () => openReset(u),
        }, "Reset password"),
        el("button", {
          class: "btn btn-sm", type: "button", disabled: u.self,
          onclick: () => setRole(u, u.role === "admin" ? "user" : "admin"),
        }, u.role === "admin" ? "Make user" : "Make admin"),
        el("button", {
          class: "btn btn-sm", type: "button", disabled: u.self,
          onclick: () => setDisabled(u, !u.disabled),
        }, u.disabled ? "Enable" : "Disable"),
        el("button", {
          class: "btn btn-sm btn-danger", type: "button", disabled: u.self,
          onclick: () => remove(u),
        }, "Delete"));

      return row([name, roleCell, groups, status, cell(u.createdAt, "nowrap"), actions]);
    }

    function renderGroupRoles() {
      const tbody = $("#grouproles");
      if (!state.enabled) {
        emptyRow(tbody, 3, "This console has no accounts: AUTH_MODE is none.");
        return;
      }
      if (!state.groups.length) {
        emptyRow(tbody, 3, "No groups yet. Create one on the Groups page.");
        return;
      }
      tbody.replaceChildren(...state.groups.map((name) => {
        const grants = state.groupRoles[name] === "admin";
        return row([
          cell(name),
          el("td", {}, grants ? chip("Administrator", "admin") : el("span", { class: "muted", text: "nothing" })),
          el("td", { class: "actions" },
            el("button", {
              class: "btn btn-sm", type: "button",
              onclick: () => setGroupRole(name, grants ? "user" : "admin"),
            }, grants ? "Revoke" : "Grant administrator")),
        ]);
      }));
    }

    async function patch(u, body, done) {
      try {
        await api("/admin/api/users/" + encodeURIComponent(u.id), {
          method: "PATCH", body: JSON.stringify(body),
        });
        toast(done, "ok");
        load();
      } catch (err) {
        toast(err.message, "error");
      }
    }

    const setRole = (u, role) => patch(u, { role: role }, `${u.username} is now ${role === "admin" ? "an administrator" : "a user"}.`);

    function setDisabled(u, disabled) {
      if (disabled && !confirmed(`Disable ${u.username}? They are signed out everywhere immediately and their API tokens stop working.`)) return;
      patch(u, { disabled: disabled }, `${u.username} is ${disabled ? "disabled" : "enabled"}.`);
    }

    async function remove(u) {
      if (!confirmed(`Delete ${u.username}? This cannot be undone, and every token they hold is revoked.`)) return;
      try {
        await api("/admin/api/users/" + encodeURIComponent(u.id), { method: "DELETE" });
        toast(`${u.username} deleted.`, "ok");
        load();
      } catch (err) {
        toast(err.message, "error");
      }
    }

    async function setGroupRole(group, role) {
      try {
        await api("/admin/api/group-roles", {
          method: "POST", body: JSON.stringify({ group: group, role: role }),
        });
        toast(role === "admin"
          ? `Everyone in ${group} now administers this console.`
          : `${group} no longer grants a role.`, "ok");
        load();
      } catch (err) {
        toast(err.message, "error");
      }
    }

    /* The one dialog, in its two shapes. */

    function openAdd() {
      editing = null;
      $("#dialog-title").textContent = "Add account";
      $("#dialog-sub").textContent = "The password below is temporary: its owner chooses their own on first sign-in.";
      $("#field-username").hidden = false;
      $("#field-role").hidden = false;
      $("#d-username").value = "";
      $("#d-password").value = "";
      $("#d-role").value = "user";
      showError("#d-error", "");
      openDialog("#dialog");
      $("#d-username").focus();
    }

    function openReset(u) {
      editing = u;
      $("#dialog-title").textContent = "Reset the password for " + u.username;
      $("#dialog-sub").textContent = "They are signed out everywhere, and asked to choose their own the next time they sign in.";
      $("#field-username").hidden = true;
      $("#field-role").hidden = true;
      $("#d-password").value = "";
      showError("#d-error", "");
      openDialog("#dialog");
      $("#d-password").focus();
    }

    async function save() {
      const password = $("#d-password").value;
      try {
        if (editing) {
          await api("/admin/api/users/" + encodeURIComponent(editing.id), {
            method: "PATCH", body: JSON.stringify({ password: password }),
          });
          toast(`Password reset for ${editing.username}.`, "ok");
        } else {
          await api("/admin/api/users", {
            method: "POST",
            body: JSON.stringify({
              username: $("#d-username").value.trim(),
              password: password,
              role: $("#d-role").value,
            }),
          });
          toast("Account created.", "ok");
        }
        closeDialog("#dialog");
        load();
      } catch (err) {
        showError("#d-error", err.message);
      }
    }

    $("#add").addEventListener("click", openAdd);
    $("#d-cancel").addEventListener("click", () => closeDialog("#dialog"));
    $("#d-save").addEventListener("click", save);
    $("#d-generate").addEventListener("click", () => { $("#d-password").value = generatePassword(20); });
    $("#d-copy").addEventListener("click", () => copyToClipboard($("#d-password").value));
    $("#filter").addEventListener("input", render);
    $("#scope").addEventListener("change", render);

    load();
  }

  /* ---------------------------------------------------------------------
     Groups
     --------------------------------------------------------------------- */

  function initGroups() {
    let state = { groups: [], accounts: [], fromProvider: false, enabled: false };
    let editing = null; // null while creating, otherwise the group being changed

    async function load() {
      try {
        const data = await api("/admin/api/groups");
        state = {
          groups: data.groups || [],
          accounts: data.accounts || [],
          fromProvider: !!data.groupsFromProvider,
          enabled: !!data.authEnabled,
        };
      } catch (err) {
        toast(err.message, "error");
        return;
      }
      const note = $("#provider-note");
      if (note) note.hidden = !state.fromProvider;
      render();
    }

    function render() {
      const tbody = $("#rows");
      if (!state.enabled) {
        emptyRow(tbody, 5, "This console has no accounts: AUTH_MODE is none.");
        $("#add").disabled = true;
        return;
      }
      const needle = ($("#filter").value || "").trim().toLowerCase();
      const list = state.groups.filter((g) => {
        if (!needle) return true;
        return (g.name + " " + (g.members || []).join(" ")).toLowerCase().includes(needle);
      });
      if (!list.length) {
        emptyRow(tbody, 5, "No group matches that.");
        return;
      }
      tbody.replaceChildren(...list.map(groupRow));
    }

    function groupRow(g) {
      const name = el("td", {},
        el("div", { text: g.name }),
        g.description ? el("div", { class: "small muted", text: g.description }) : null);

      const members = el("td", {},
        el("div", { text: (g.members && g.members.length) ? String(g.members.length) : "empty" }),
        (g.members && g.members.length)
          ? el("div", { class: "small muted", text: g.members.join(", ") })
          : null);

      const grants = el("td", {}, g.role === "admin"
        ? chip("Administrator", "admin")
        : el("span", { class: "muted", text: "nothing" }));

      const source = el("td", {}, g.declared
        ? chip("Declared")
        : chip("In use", "warn"));

      const actions = el("td", { class: "actions" },
        el("button", { class: "btn btn-sm", type: "button", onclick: () => openEdit(g) }, "Edit"),
        el("button", { class: "btn btn-sm btn-danger", type: "button", onclick: () => remove(g) }, "Delete"));

      return row([name, members, grants, source, actions]);
    }

    function memberList(selected, externalMembers) {
      const box = $("#g-members");
      const chosen = new Set((selected || []).map((s) => s.toLowerCase()));
      const locked = new Set((externalMembers || []).map((s) => s.toLowerCase()));
      box.replaceChildren(...state.accounts.map((account) => {
        // A directory-owned account is shown but not tickable where the
        // deployment maps a groups claim: the next sign-in would overwrite
        // whatever was set here, so offering the control would be a lie.
        const isLocked = state.fromProvider && account.external;
        const input = el("input", {
          type: "checkbox",
          value: account.username,
          disabled: isLocked,
        });
        input.checked = chosen.has(account.username.toLowerCase()) || locked.has(account.username.toLowerCase());
        return el("label", { class: isLocked ? "is-locked" : "" }, input,
          el("span", { text: account.username + (isLocked ? " · single sign-on" : "") }));
      }));
    }

    function openCreate() {
      editing = null;
      $("#dialog-title").textContent = "Create group";
      $("#g-name").value = "";
      $("#g-description").value = "";
      $("#g-role").value = "user";
      memberList([], []);
      showError("#g-error", "");
      openDialog("#dialog");
      $("#g-name").focus();
    }

    function openEdit(g) {
      editing = g;
      $("#dialog-title").textContent = "Edit " + g.name;
      $("#g-name").value = g.name;
      $("#g-description").value = g.description || "";
      $("#g-role").value = g.role === "admin" ? "admin" : "user";
      memberList(g.members, g.externalMembers);
      showError("#g-error", "");
      openDialog("#dialog");
      $("#g-name").focus();
    }

    function chosenMembers() {
      return $$("#g-members input[type=checkbox]").filter((i) => i.checked && !i.disabled).map((i) => i.value);
    }

    async function save() {
      const name = $("#g-name").value.trim();
      const body = {
        description: $("#g-description").value.trim(),
        members: chosenMembers(),
        role: $("#g-role").value,
      };
      try {
        if (editing) {
          body.newName = name;
          await api("/admin/api/groups/" + encodeURIComponent(editing.name), {
            method: "PATCH", body: JSON.stringify(body),
          });
          toast(`${name} saved.`, "ok");
        } else {
          body.name = name;
          await api("/admin/api/groups", { method: "POST", body: JSON.stringify(body) });
          toast(`${name} created.`, "ok");
        }
        closeDialog("#dialog");
        load();
      } catch (err) {
        showError("#g-error", err.message);
      }
    }

    async function remove(g) {
      if (!confirmed(`Delete ${g.name}? It is removed from every account in it, and the role it granted goes with it. The accounts themselves are untouched.`)) return;
      try {
        await api("/admin/api/groups/" + encodeURIComponent(g.name), { method: "DELETE" });
        toast(`${g.name} deleted.`, "ok");
        load();
      } catch (err) {
        toast(err.message, "error");
      }
    }

    $("#add").addEventListener("click", openCreate);
    $("#g-cancel").addEventListener("click", () => closeDialog("#dialog"));
    $("#g-save").addEventListener("click", save);
    $("#filter").addEventListener("input", render);

    load();
  }

  /* ---------------------------------------------------------------------
     API tokens
     --------------------------------------------------------------------- */

  function initTokens() {
    let state = { tokens: [], accounts: [], enabled: false, sharedTokenSet: false };

    async function load() {
      try {
        const data = await api("/admin/api/tokens");
        state = {
          tokens: data.tokens || [],
          accounts: data.accounts || [],
          enabled: !!data.authEnabled,
          sharedTokenSet: !!data.sharedTokenSet,
        };
      } catch (err) {
        toast(err.message, "error");
        return;
      }
      const select = $("#t-account");
      select.replaceChildren(...state.accounts.map((name) => el("option", { value: name, text: name })));
      render();
    }

    function render() {
      const tbody = $("#rows");
      if (!state.enabled) {
        emptyRow(tbody, 7, state.sharedTokenSet
          ? "This console has one operator and one shared credential, set in API_TOKEN. Per-account tokens need AUTH_MODE=local."
          : "This console has one operator and no credential configured. Set API_TOKEN, or AUTH_MODE=local for per-account tokens.");
        $("#add").disabled = true;
        return;
      }
      const needle = ($("#filter").value || "").trim().toLowerCase();
      const list = state.tokens.filter((t) =>
        !needle || (t.owner + " " + t.name + " " + t.id).toLowerCase().includes(needle));
      if (!list.length) {
        emptyRow(tbody, 7, "No token matches that.");
        return;
      }
      tbody.replaceChildren(...list.map(tokenRow));
    }

    function tokenRow(t) {
      const status = t.status === "active" ? chip("Active")
        : t.status === "revoked" ? chip("Revoked", "danger")
          : t.status === "expired" ? chip("Expired", "warn")
            : chip(t.status, "warn");
      return row([
        cell(t.id, "mono"),
        cell(t.owner),
        cell(t.name),
        cell(t.scopes, "mono"),
        el("td", {}, status),
        cell(t.lastUsedAt || "never", "nowrap"),
        el("td", { class: "actions" },
          el("button", {
            class: "btn btn-sm btn-danger", type: "button", disabled: t.revoked,
            onclick: () => revoke(t),
          }, "Revoke")),
      ]);
    }

    async function revoke(t) {
      if (!confirmed(`Revoke ${t.id}? Whatever is using it stops working immediately.`)) return;
      try {
        await api("/admin/api/tokens/" + encodeURIComponent(t.id), { method: "DELETE" });
        toast("Revoked.", "ok");
        load();
      } catch (err) {
        toast(err.message, "error");
      }
    }

    async function issue() {
      try {
        const result = await api("/admin/api/tokens", {
          method: "POST",
          body: JSON.stringify({
            username: $("#t-account").value,
            name: $("#t-name").value.trim(),
            readOnly: $("#t-readonly").checked,
            expiresIn: $("#t-expires").value,
          }),
        });
        closeDialog("#dialog");
        $("#s-value").value = result.secret;
        openDialog("#secret");
        load();
      } catch (err) {
        showError("#t-error", err.message);
      }
    }

    $("#add").addEventListener("click", () => {
      $("#t-name").value = "";
      $("#t-readonly").checked = false;
      $("#t-expires").value = "";
      showError("#t-error", "");
      openDialog("#dialog");
      $("#t-name").focus();
    });
    $("#t-cancel").addEventListener("click", () => closeDialog("#dialog"));
    $("#t-save").addEventListener("click", issue);
    $("#s-copy").addEventListener("click", () => copyToClipboard($("#s-value").value));
    $("#s-done").addEventListener("click", () => {
      // Cleared on the way out rather than left in the DOM of a page somebody
      // may leave open on a shared screen.
      $("#s-value").value = "";
      closeDialog("#secret");
    });
    $("#filter").addEventListener("input", render);

    load();
  }

  /* ---------------------------------------------------------------------
     Load runs
     --------------------------------------------------------------------- */

  function initRuns() {
    let showOwners = false;

    async function load() {
      let data;
      try {
        data = await api("/admin/api/runs");
      } catch (err) {
        toast(err.message, "error");
        return;
      }
      showOwners = !!data.authEnabled;
      const tbody = $("#rows");
      const runs = data.runs || [];
      if (!runs.length) {
        emptyRow(tbody, 9, "Nothing has run on this machine yet.");
        return;
      }
      tbody.replaceChildren(...runs.map(runRow));
    }

    function runRow(run) {
      const owner = run.self ? el("span", { class: "muted", text: "this console" })
        : !showOwners ? el("span", { class: "muted", text: "—" })
          : run.owner ? el("span", { text: run.owner })
            : el("span", { class: "muted", text: "not started here" });

      return row([
        cell(String(run.pid), "mono"),
        el("td", {}, owner),
        el("td", {}, run.self ? chip("Serving this page")
          : run.running ? chip("Running") : chip(run.status || "finished", "warn")),
        cell(String(run.active)),
        cell(String(run.completed)),
        cell(String(run.failed)),
        cell(run.startedAt || "—", "nowrap"),
        cell(run.params || "—", "mono small"),
        el("td", { class: "actions" },
          el("button", {
            class: "btn btn-sm btn-danger", type: "button",
            disabled: !run.running || run.self,
            title: run.self ? "Stopping this would stop the console you are reading" : "",
            onclick: () => stop(run),
          }, "Stop")),
      ]);
    }

    async function stop(run) {
      const whose = run.owner ? `${run.owner}'s run` : "this run";
      if (!confirmed(`Stop ${whose} (pid ${run.pid})? Whatever it is measuring ends now.`)) return;
      try {
        // /kill predates the administration area and answers in plain text, so
        // it is called directly rather than through api().
        const response = await fetch("/kill?pid=" + encodeURIComponent(run.pid), {
          method: "POST",
          credentials: "same-origin",
          headers: { "X-Requested-With": "XMLHttpRequest" },
        });
        if (!response.ok) throw new Error((await response.text()) || ("stop failed with " + response.status));
        toast("Stopped.", "ok");
        load();
      } catch (err) {
        toast(err.message, "error");
      }
    }

    load();
    setInterval(load, 5000);
  }

  /* ---------------------------------------------------------------------
     Audit trail
     --------------------------------------------------------------------- */

  function initAudit() {
    let entries = [];

    async function load() {
      try {
        const data = await api("/admin/api/audit?limit=500");
        entries = data.entries || [];
        const path = $("#path");
        if (path && data.path) path.textContent = "Written to " + data.path;
      } catch (err) {
        toast(err.message, "error");
        return;
      }
      render();
    }

    function render() {
      const needle = ($("#filter").value || "").trim().toLowerCase();
      const outcome = $("#outcome").value;
      const list = entries.filter((e) => {
        if (outcome === "refused" && e.outcome === "success") return false;
        if (outcome === "success" && e.outcome !== "success") return false;
        if (!needle) return true;
        const actor = e.actor || {};
        const detail = Object.entries(e.detail || {}).map(([k, v]) => k + " " + v).join(" ");
        return (e.event + " " + (actor.username || "") + " " + (actor.userId || "") + " " +
          (e.clientIp || "") + " " + (e.target || "") + " " + detail).toLowerCase().includes(needle);
      });

      const tbody = $("#rows");
      if (!list.length) {
        emptyRow(tbody, 7, entries.length ? "No entry matches that." : "Nothing recorded yet.");
        return;
      }
      tbody.replaceChildren(...list.map((e) => {
        const actor = e.actor || {};
        const who = actor.username || actor.userId || "—";
        const detail = Object.entries(e.detail || {}).map(([k, v]) => `${k}=${v}`).join(" ");
        return row([
          cell(formatTime(e.time), "nowrap"),
          cell(e.event, "mono"),
          cell(actor.kind === "api-token" ? `${who} (token)` : who),
          cell(e.clientIp || "—", "mono"),
          cell(e.target || "—"),
          cell(e.outcome, "outcome-" + e.outcome),
          cell(detail || "—", "small"),
        ]);
      }));
    }

    $("#filter").addEventListener("input", render);
    $("#outcome").addEventListener("change", render);
    load();
  }

  /* ---------------------------------------------------------------------
     Dispatch
     --------------------------------------------------------------------- */

  const pages = {
    "/admin": initOverview,
    "/admin/users": initUsers,
    "/admin/groups": initGroups,
    "/admin/tokens": initTokens,
    "/admin/runs": initRuns,
    "/admin/audit": initAudit,
  };

  const start = pages[window.location.pathname.replace(/\/+$/, "") || "/admin"];
  if (start) start();
})();
