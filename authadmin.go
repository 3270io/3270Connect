package main

// The administration area.
//
// Six pages, each answering one question: is everything all right (Overview),
// who has an account (Accounts), which teams are there (Groups), what
// automated clients can reach this console (API tokens), what is running right
// now and whose it is (Load runs), and what has happened (Audit trail).
//
// Pages are server-rendered shells; the tables inside them are painted from
// JSON under /admin/api. Splitting it that way means the tables can refresh
// themselves — a load-run list that is stale is worse than no list — without
// the page having to be re-rendered around them.
//
// Everything here is behind requireAdminPath, which the gate applies to the
// whole /admin prefix. No handler in this file re-checks it, and none should:
// a check that some handlers do is a check the next handler forgets.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/3270io/3270Connect/internal/apitoken"
	"github.com/3270io/3270Connect/internal/audit"
	"github.com/3270io/3270Connect/internal/authz"
	"github.com/3270io/3270Connect/internal/users"
)

// registerAdminHandlers puts the administration area on the console's mux.
func (a *authState) registerAdminHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/admin", a.adminOverviewPage)
	mux.HandleFunc("/admin/users", a.adminUsersPage)
	mux.HandleFunc("/admin/groups", a.adminGroupsPage)
	mux.HandleFunc("/admin/tokens", a.adminTokensPage)
	mux.HandleFunc("/admin/runs", a.adminRunsPage)
	mux.HandleFunc("/admin/audit", a.adminAuditPage)

	mux.HandleFunc("/admin/api/overview", a.adminOverviewData)
	mux.HandleFunc("/admin/api/users", a.adminUsersAPI)
	mux.HandleFunc("/admin/api/users/", a.adminUserAPI)
	mux.HandleFunc("/admin/api/groups", a.adminGroupsAPI)
	mux.HandleFunc("/admin/api/groups/", a.adminGroupAPI)
	mux.HandleFunc("/admin/api/group-roles", a.adminGroupRoleAPI)
	mux.HandleFunc("/admin/api/tokens", a.adminTokensAPI)
	mux.HandleFunc("/admin/api/tokens/", a.adminTokenAPI)
	mux.HandleFunc("/admin/api/runs", a.adminRunsAPI)
	mux.HandleFunc("/admin/api/audit", a.adminAuditAPI)
	mux.HandleFunc("/admin/api/audit.jsonl", a.adminAuditDownload)
}

/* ---------------------------------------------------------------------
   Pages
   --------------------------------------------------------------------- */

func (a *authState) adminPage(w http.ResponseWriter, r *http.Request, page, title, active string) {
	renderAuthPage(w, r, http.StatusOK, page, authPageData{
		Title:               title,
		Active:              active,
		MinLength:           users.MinPasswordLength,
		MaxGroupName:        users.MaxGroupNameLength,
		MaxGroupDescription: users.MaxGroupDescriptionLength,
		Limit:               auditPageLimit,
	})
}

func (a *authState) adminOverviewPage(w http.ResponseWriter, r *http.Request) {
	a.adminPage(w, r, "admin-overview.gohtml", "Administration", "overview")
}

func (a *authState) adminUsersPage(w http.ResponseWriter, r *http.Request) {
	a.adminPage(w, r, "admin-users.gohtml", "Accounts", "users")
}

func (a *authState) adminGroupsPage(w http.ResponseWriter, r *http.Request) {
	a.adminPage(w, r, "admin-groups.gohtml", "Groups", "groups")
}

func (a *authState) adminTokensPage(w http.ResponseWriter, r *http.Request) {
	a.adminPage(w, r, "admin-tokens.gohtml", "API tokens", "tokens")
}

func (a *authState) adminRunsPage(w http.ResponseWriter, r *http.Request) {
	a.adminPage(w, r, "admin-runs.gohtml", "Load runs", "runs")
}

func (a *authState) adminAuditPage(w http.ResponseWriter, r *http.Request) {
	a.adminPage(w, r, "admin-audit.gohtml", "Audit trail", "audit")
}

/* ---------------------------------------------------------------------
   Shared plumbing for the JSON endpoints
   --------------------------------------------------------------------- */

// decodeJSON reads a request body, answering the caller itself when it cannot.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	// A bounded read: this is an authenticated endpoint, but "authenticated"
	// is not "may decide how much memory this process allocates".
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(into); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

// requireMethod answers the caller when the method is not one this endpoint
// serves, and reports whether the handler should continue.
func requireMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

// requireAccounts refuses account management on an instance that has no
// accounts, which is clearer than succeeding against a store nothing reads.
func (a *authState) requireAccounts(w http.ResponseWriter) bool {
	if a.separatesUsers() {
		return true
	}
	writeJSONError(w, http.StatusConflict,
		"Account management needs AUTH_MODE=local or AUTH_MODE=oidc. Restart 3270Connect with it set.")
	return false
}

// adminActor names the administrator for a log line.
func adminActor(r *http.Request) string {
	if name := usernameOf(r); name != "" {
		return name
	}
	return principalOf(r).UserID
}

// pathTail returns the last segment of a path, which is how these endpoints
// carry an identifier. net/http's own routing patterns would do it, but the
// console is registered on one mux shared with handlers written long before
// they existed, and mixing the two styles reads worse than one helper.
func pathTail(path, prefix string) string {
	return strings.Trim(strings.TrimPrefix(path, prefix), "/")
}

/* ---------------------------------------------------------------------
   Overview
   --------------------------------------------------------------------- */

// overviewRecentLimit is how many audit entries the overview carries. Enough
// to say what has been happening; the audit page is one click away for more.
const overviewRecentLimit = 12

// adminOverviewData aggregates the instance's state for the landing page:
// account counts, live logins, running load tests and the recent trail. One
// request rather than five, because the page paints as a unit.
func (a *authState) adminOverviewData(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	now := time.Now()

	accounts := map[string]int{"total": 0, "admins": 0, "disabled": 0, "mustChange": 0, "external": 0}
	if a.separatesUsers() {
		list, err := a.userStore().List()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "could not read the account list")
			return
		}
		roles := a.groupRoles()
		for _, u := range list {
			// Effective administrators: one whose role comes from a group
			// belongs in this count exactly as much as one appointed directly.
			if users.EffectiveRole(u, roles) == authz.RoleAdmin {
				accounts["admins"]++
			}
			if u.Disabled {
				accounts["disabled"]++
			}
			if u.MustChangePassword {
				accounts["mustChange"]++
			}
			if u.External() {
				accounts["external"]++
			}
		}
		accounts["total"] = len(list)
	}

	// Live logins (browsers holding a cookie), distinct from load runs: one
	// signed-in person may have several runs going, or none.
	logins, loginUsers := 0, 0
	if a.sessions != nil {
		logins = a.sessions.Count()
		loginUsers = len(a.sessions.UserIDs())
	}

	runs := a.runSnapshot()
	running := 0
	for _, run := range runs {
		if run.Running {
			running++
		}
	}

	// The audit read serves two displays at once: the recent-activity list and
	// the last-24-hours counters above it. 500 newest entries bound the scan
	// the same way the audit page bounds its own view.
	entries, err := a.recorder().Read(500)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the audit log")
		return
	}
	dayAgo := now.Add(-24 * time.Hour)
	var total24h, refused24h int
	for _, e := range entries {
		if e.Time.Before(dayAgo) {
			continue
		}
		total24h++
		if e.Outcome == audit.Denied || e.Outcome == audit.Failure {
			refused24h++
		}
	}
	recent := entries
	if len(recent) > overviewRecentLimit {
		recent = recent[:overviewRecentLimit]
	}
	if recent == nil {
		recent = []audit.Entry{}
	}

	tokensIssued, tokensActive := a.tokenCounts(now)

	writeJSON(w, http.StatusOK, map[string]any{
		"authEnabled": a.separatesUsers(),
		"authMode":    string(a.mode),
		"version":     version,
		"startedAt":   a.startedAt.UTC().Format(time.RFC3339),
		"accounts":    accounts,
		"signedIn":    map[string]int{"logins": logins, "users": loginUsers},
		"runs":        map[string]int{"running": running, "known": len(runs)},
		"tokens":      map[string]int{"issued": tokensIssued, "active": tokensActive},
		"audit": map[string]any{
			"recent":     recent,
			"total24h":   total24h,
			"refused24h": refused24h,
		},
	})
}

func (a *authState) tokenCounts(now time.Time) (issued, active int) {
	if !a.separatesUsers() {
		return 0, 0
	}
	list, err := a.tokenStore().List()
	if err != nil {
		return 0, 0
	}
	for _, tok := range list {
		issued++
		if tok.Active(now) == nil {
			active++
		}
	}
	return issued, active
}

/* ---------------------------------------------------------------------
   Accounts
   --------------------------------------------------------------------- */

// adminUserView is the wire shape for one account. It never carries a hash;
// the store strips it, and this type has nowhere to put one.
type adminUserView struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	Disabled           bool   `json:"disabled"`
	MustChangePassword bool   `json:"mustChangePassword"`
	CreatedAt          string `json:"createdAt"`
	PasswordChangedAt  string `json:"passwordChangedAt"`
	// Self marks the caller's own account, so the page can grey out the
	// actions that would lock them out of the instance they are using.
	Self bool `json:"self"`
	// External marks an account that signs in through the identity provider.
	// It has no password to reset, so the page says where it comes from rather
	// than offering a control that cannot work.
	External bool     `json:"external"`
	Issuer   string   `json:"issuer,omitempty"`
	Groups   []string `json:"groups"`
	// EffectiveRole is what the account actually holds once group assignments
	// are counted; RoleGroups names the groups responsible when it is more
	// than Role says. The page shows both, because "why is this person an
	// administrator" has to be answerable by reading the row.
	EffectiveRole string   `json:"effectiveRole"`
	RoleGroups    []string `json:"roleGroups,omitempty"`
}

func toAdminUserView(u users.User, selfID string, groupRoles map[string]authz.Role) adminUserView {
	groups := u.Groups
	if groups == nil {
		groups = []string{}
	}
	return adminUserView{
		ID:                 u.ID,
		Username:           u.Username,
		Role:               string(u.Role),
		Disabled:           u.Disabled,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt.UTC().Format("2006-01-02 15:04"),
		PasswordChangedAt:  u.PasswordChangedAt.UTC().Format("2006-01-02 15:04"),
		Self:               u.ID == selfID,
		External:           u.External(),
		Issuer:             u.Issuer,
		Groups:             groups,
		EffectiveRole:      string(users.EffectiveRole(u, groupRoles)),
		RoleGroups:         users.RoleGrantingGroups(u, groupRoles),
	}
}

func (a *authState) adminUsersAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.adminListUsers(w, r)
	case http.MethodPost:
		a.adminCreateUser(w, r)
	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost)
	}
}

func (a *authState) adminListUsers(w http.ResponseWriter, r *http.Request) {
	if !a.separatesUsers() {
		writeJSON(w, http.StatusOK, map[string]any{"authEnabled": false, "users": []adminUserView{}})
		return
	}
	list, err := a.userStore().List()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the account list")
		return
	}
	roles := a.groupRoles()
	selfID := principalOf(r).UserID
	out := make([]adminUserView, 0, len(list))
	for _, u := range list {
		out = append(out, toAdminUserView(u, selfID, roles))
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Username) < strings.ToLower(out[j].Username)
	})

	known, _ := a.userStore().Groups()
	if known == nil {
		known = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authEnabled": true,
		"users":       out,
		// Every group in use, so the page can offer them rather than relying
		// on somebody spelling one the same way twice.
		"groups":     known,
		"groupRoles": roles,
		// Where groups come from, so the page can say why it will not let them
		// be edited on a directory-owned account.
		"groupsFromProvider": a.ssoEnabled() && a.sso.GroupsClaim != "",
	})
}

type adminCreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (a *authState) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAccounts(w) {
		return
	}
	var req adminCreateUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	role := authz.RoleUser
	if strings.EqualFold(req.Role, string(authz.RoleAdmin)) {
		role = authz.RoleAdmin
	}
	username := strings.TrimSpace(req.Username)
	if err := users.ValidateUsername(username); err != nil {
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return
	}
	if err := users.ValidatePassword(req.Password); err != nil {
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return
	}

	// New accounts must change the password the administrator chose: it was
	// typed by somebody else and probably sent over chat.
	user, err := a.userStore().Add(username, req.Password, role, true)
	if err != nil {
		if errors.Is(err, users.ErrUserExists) {
			writeJSONError(w, http.StatusConflict, "an account with that name already exists")
			return
		}
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return
	}

	log.Printf("auth: %s created account %q (%s)", adminActor(r), user.Username, user.Role)
	a.auditRequest(r, audit.EventAccountCreated, audit.Success, user.Username,
		map[string]string{"role": string(user.Role)})
	writeJSON(w, http.StatusCreated, map[string]any{
		"user": toAdminUserView(user, principalOf(r).UserID, a.groupRoles()),
	})
}

type adminUpdateUserRequest struct {
	// Pointers so "not mentioned" and "set to false" stay distinguishable.
	Role     *string `json:"role"`
	Disabled *bool   `json:"disabled"`
	Password *string `json:"password"`
	// Groups replaces the whole membership list. A pointer for the same
	// reason: "not mentioned" must not clear it, and an empty list must.
	Groups *[]string `json:"groups"`
}

func (a *authState) adminUserAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch, http.MethodPost:
		a.adminUpdateUser(w, r)
	case http.MethodDelete:
		a.adminDeleteUser(w, r)
	default:
		requireMethod(w, r, http.MethodPatch, http.MethodPost, http.MethodDelete)
	}
}

// lookupTarget resolves the account named in the path, answering the request
// itself when it cannot.
func (a *authState) lookupTarget(w http.ResponseWriter, r *http.Request) (users.User, bool) {
	id := pathTail(r.URL.Path, "/admin/api/users")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing account id")
		return users.User{}, false
	}
	user, found, err := a.userStore().ByID(id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the account")
		return users.User{}, false
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "no such account")
		return users.User{}, false
	}
	return user, true
}

func (a *authState) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAccounts(w) {
		return
	}
	target, ok := a.lookupTarget(w, r)
	if !ok {
		return
	}
	var req adminUpdateUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	self := target.ID == principalOf(r).UserID

	if req.Role != nil {
		role := authz.RoleUser
		if strings.EqualFold(*req.Role, string(authz.RoleAdmin)) {
			role = authz.RoleAdmin
		}
		// Self-demotion is refused rather than merely warned about: it takes
		// effect immediately, and the administrator would lose the page they
		// are standing on with no way back except the console command.
		if self && role != authz.RoleAdmin {
			writeJSONError(w, http.StatusBadRequest, "you cannot remove your own administrator role")
			return
		}
		if err := a.userStore().SetRole(target.Username, role); err != nil {
			writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
			return
		}
		// A login carries the role it was created with, so the change has to
		// be pushed into the sessions the account is already in. Without this
		// a demotion would not take effect until the person signed in again —
		// and they could undo it from the page they were still standing on.
		//
		// What is pushed is the effective role: demoting somebody's own role
		// while a group still grants them administration demotes nothing, and
		// a session saying otherwise would disagree with every fresh login.
		target.Role = role
		a.sessions.SetRoleFor(target.ID, a.effectiveRoleFor(target))
		log.Printf("auth: %s set %q role to %s", adminActor(r), target.Username, role)
		a.auditRequest(r, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "role", "role": string(role)})
	}

	if req.Disabled != nil {
		if self && *req.Disabled {
			writeJSONError(w, http.StatusBadRequest, "you cannot disable your own account")
			return
		}
		if err := a.userStore().SetDisabled(target.Username, *req.Disabled); err != nil {
			writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
			return
		}
		if *req.Disabled {
			// A disabled account must lose its live sessions, or "disabled"
			// only takes effect the next time they sign in.
			a.sessions.DeleteAllFor(target.ID)
		}
		log.Printf("auth: %s set %q disabled=%v", adminActor(r), target.Username, *req.Disabled)
		a.auditRequest(r, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "disabled", "disabled": strconv.FormatBool(*req.Disabled)})
	}

	if req.Groups != nil {
		// Refused on an account the directory owns, rather than written and
		// then overwritten at the next sign-in — which would look like the
		// change had worked until somebody noticed it had not.
		if target.External() && a.ssoEnabled() && a.sso.GroupsClaim != "" {
			writeJSONError(w, http.StatusConflict,
				"this account's groups come from the identity provider; change them there")
			return
		}
		// Leaving a group can remove a role. Doing that to yourself is
		// self-demotion wearing different clothes, and is refused for the same
		// reason: it takes effect immediately and strands you.
		if self && target.Role != authz.RoleAdmin {
			roles := a.groupRoles()
			after := target
			after.Groups = users.NormaliseGroups(*req.Groups)
			if users.EffectiveRole(target, roles) == authz.RoleAdmin &&
				users.EffectiveRole(after, roles) != authz.RoleAdmin {
				writeJSONError(w, http.StatusBadRequest,
					"your administrator role comes from one of these groups; you cannot leave it yourself")
				return
			}
		}
		if err := a.userStore().SetGroups(target.Username, *req.Groups); err != nil {
			writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
			return
		}
		// Group membership can carry a role, so the sessions this account
		// already holds are given the recomputed one.
		target.Groups = users.NormaliseGroups(*req.Groups)
		a.sessions.SetRoleFor(target.ID, a.effectiveRoleFor(target))
		log.Printf("auth: %s set groups for %q", adminActor(r), target.Username)
		a.auditRequest(r, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "groups", "groups": strings.Join(target.Groups, " ")})
	}

	if req.Password != nil {
		if err := users.ValidatePassword(*req.Password); err != nil {
			writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
			return
		}
		if err := a.userStore().SetPassword(target.Username, *req.Password); err != nil {
			writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
			return
		}
		// The administrator now knows this password, so its owner has to
		// replace it before doing anything else.
		if err := a.userStore().RequirePasswordChange(target.Username); err != nil {
			log.Printf("auth: could not flag %q for a password change: %v", target.Username, err)
		}
		if !self {
			a.sessions.DeleteAllFor(target.ID)
		}
		log.Printf("auth: %s reset the password for %q", adminActor(r), target.Username)
		a.auditRequest(r, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "password reset"})
	}

	updated, found, err := a.userStore().ByID(target.ID)
	if err != nil || !found {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": toAdminUserView(updated, principalOf(r).UserID, a.groupRoles()),
	})
}

func (a *authState) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAccounts(w) {
		return
	}
	target, ok := a.lookupTarget(w, r)
	if !ok {
		return
	}
	if target.ID == principalOf(r).UserID {
		writeJSONError(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	if err := a.userStore().Delete(target.Username); err != nil {
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return
	}
	a.sessions.DeleteAllFor(target.ID)
	// Its tokens already stop working — authenticateToken looks the owner up
	// on every call — but marking them revoked keeps the token list honest
	// rather than showing dead credentials as active.
	//
	// Disabling does not do this, deliberately: the same live lookup refuses a
	// disabled account's tokens, and re-enabling should give the person their
	// automated clients back rather than making them reissue everything.
	if n, err := a.tokenStore().RevokeAllFor(target.ID); err != nil {
		log.Printf("auth: could not revoke tokens for %q: %v", target.Username, err)
	} else if n > 0 {
		log.Printf("auth: revoked %d API token(s) belonging to %q", n, target.Username)
		a.auditRequest(r, audit.EventTokenRevoked, audit.Success, target.Username,
			map[string]string{"count": strconv.Itoa(n), "reason": "account deleted"})
	}
	log.Printf("auth: %s deleted account %q", adminActor(r), target.Username)
	a.auditRequest(r, audit.EventAccountDeleted, audit.Success, target.Username, nil)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

/* ---------------------------------------------------------------------
   Groups
   --------------------------------------------------------------------- */

func (a *authState) adminGroupsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.adminListGroups(w, r)
	case http.MethodPost:
		a.adminCreateGroup(w, r)
	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost)
	}
}

func (a *authState) adminListGroups(w http.ResponseWriter, r *http.Request) {
	if !a.separatesUsers() {
		writeJSON(w, http.StatusOK, map[string]any{"authEnabled": false, "groups": []users.GroupInfo{}})
		return
	}
	list, err := a.userStore().ListGroups()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the groups")
		return
	}
	if list == nil {
		list = []users.GroupInfo{}
	}
	accounts, err := a.userStore().List()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the account list")
		return
	}
	type member struct {
		Username string `json:"username"`
		External bool   `json:"external"`
	}
	members := make([]member, 0, len(accounts))
	for _, u := range accounts {
		members = append(members, member{Username: u.Username, External: u.External()})
	}
	sort.Slice(members, func(i, j int) bool {
		return strings.ToLower(members[i].Username) < strings.ToLower(members[j].Username)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"authEnabled": true,
		"groups":      list,
		"accounts":    members,
		// Membership of a directory-owned account is the directory's, because
		// the claim is replayed at every sign-in. The page needs to know so it
		// can say so rather than offering a tick box that will be undone.
		"groupsFromProvider": a.ssoEnabled() && a.sso.GroupsClaim != "",
		"maxNameLength":      users.MaxGroupNameLength,
		"maxDescription":     users.MaxGroupDescriptionLength,
	})
}

type adminGroupRequest struct {
	Name        string    `json:"name"`
	NewName     *string   `json:"newName"`
	Description *string   `json:"description"`
	Members     *[]string `json:"members"`
	Role        *string   `json:"role"`
}

func (a *authState) adminCreateGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAccounts(w) {
		return
	}
	var req adminGroupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	group, err := a.userStore().CreateGroup(req.Name, description)
	if err != nil {
		if errors.Is(err, users.ErrGroupExists) {
			writeJSONError(w, http.StatusConflict, "a group with that name already exists")
			return
		}
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return
	}
	log.Printf("auth: %s created group %q", adminActor(r), group.Name)
	a.auditRequest(r, audit.EventGroupCreated, audit.Success, group.Name, nil)

	// Members and a role in the same request, so the page's one dialog is one
	// act rather than three the operator has to complete in order.
	if req.Members != nil {
		if !a.applyGroupMembers(w, r, group.Name, *req.Members) {
			return
		}
	}
	if req.Role != nil {
		if !a.applyGroupRole(w, r, group.Name, *req.Role) {
			return
		}
	}
	a.adminListGroups(w, r)
}

func (a *authState) adminGroupAPI(w http.ResponseWriter, r *http.Request) {
	if !a.requireAccounts(w) {
		return
	}
	name := pathTail(r.URL.Path, "/admin/api/groups")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing group name")
		return
	}
	// The name arrives percent-encoded because a group may contain spaces;
	// net/http has already decoded the path, so this is the decoded form.
	switch r.Method {
	case http.MethodPatch, http.MethodPost:
		a.adminUpdateGroup(w, r, name)
	case http.MethodDelete:
		a.adminDeleteGroup(w, r, name)
	default:
		requireMethod(w, r, http.MethodPatch, http.MethodPost, http.MethodDelete)
	}
}

func (a *authState) adminUpdateGroup(w http.ResponseWriter, r *http.Request, name string) {
	var req adminGroupRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Renaming or describing first, so the membership and role changes below
	// are applied to the name the group now has.
	current := name
	if req.NewName != nil || req.Description != nil {
		group, err := a.userStore().UpdateGroup(name, req.NewName, req.Description, a.providerOwnsGroups())
		if err != nil {
			a.writeGroupError(w, err)
			return
		}
		current = group.Name
		log.Printf("auth: %s updated group %q", adminActor(r), current)
		detail := map[string]string{}
		if req.NewName != nil && !strings.EqualFold(name, current) {
			detail["renamedFrom"] = name
		}
		a.auditRequest(r, audit.EventGroupUpdated, audit.Success, current, detail)
	}

	if req.Members != nil {
		if !a.applyGroupMembers(w, r, current, *req.Members) {
			return
		}
	}
	if req.Role != nil {
		if !a.applyGroupRole(w, r, current, *req.Role) {
			return
		}
	}
	a.adminListGroups(w, r)
}

// applyGroupMembers replaces a group's membership, and reports whether the
// caller has already been answered.
func (a *authState) applyGroupMembers(w http.ResponseWriter, r *http.Request, group string, members []string) bool {
	// Leaving a group your own administrator role depends on is self-demotion
	// wearing different clothes, so it is refused here as it is on the
	// Accounts page.
	if a.selfAdminDependsOnGroup(r, group) && !containsUsername(members, usernameOf(r)) {
		writeJSONError(w, http.StatusBadRequest,
			"your own administrator role comes from this group; another administrator has to remove you from it")
		return false
	}

	skipped, err := a.userStore().SetGroupMembers(group, members, a.providerOwnsGroups())
	if err != nil {
		a.writeGroupError(w, err)
		return false
	}
	// Membership can carry a role, so the people already signed in are given
	// the recomputed one rather than the one they signed in with.
	a.pushEffectiveRoles()
	log.Printf("auth: %s set the membership of group %q", adminActor(r), group)
	detail := map[string]string{"members": strconv.Itoa(len(members))}
	if len(skipped) > 0 {
		// Said out loud rather than silently dropped: an administrator who
		// ticked a directory-owned account is entitled to know it did not take.
		detail["skipped"] = strings.Join(skipped, " ")
	}
	a.auditRequest(r, audit.EventGroupUpdated, audit.Success, group, detail)
	return true
}

// applyGroupRole assigns or clears the role a group grants, and reports
// whether the caller has already been answered.
func (a *authState) applyGroupRole(w http.ResponseWriter, r *http.Request, group, roleName string) bool {
	role := authz.RoleUser
	if strings.EqualFold(roleName, string(authz.RoleAdmin)) {
		role = authz.RoleAdmin
	}
	// Clearing an assignment your own administrator role depends on would lock
	// you out of the page you are standing on, exactly like demoting yourself.
	// The store's own guard only protects the last administrator; this one
	// protects the caller.
	if role != authz.RoleAdmin && a.selfAdminDependsOnGroup(r, group) {
		writeJSONError(w, http.StatusBadRequest,
			"your own administrator role comes from this group; another administrator has to change it")
		return false
	}
	if err := a.userStore().SetGroupRole(group, role); err != nil {
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return false
	}
	log.Printf("auth: %s set group %q role to %s", adminActor(r), group, role)
	a.auditRequest(r, audit.EventGroupRoleSet, audit.Success, group,
		map[string]string{"role": string(role)})
	// The people in the group are already signed in; the change reaches them
	// now rather than at their next sign-in.
	a.pushEffectiveRoles()
	return true
}

func (a *authState) adminDeleteGroup(w http.ResponseWriter, r *http.Request, name string) {
	if a.selfAdminDependsOnGroup(r, name) {
		writeJSONError(w, http.StatusBadRequest,
			"your own administrator role comes from this group; another administrator has to delete it")
		return
	}
	if err := a.userStore().DeleteGroup(name, a.providerOwnsGroups()); err != nil {
		a.writeGroupError(w, err)
		return
	}
	a.pushEffectiveRoles()
	log.Printf("auth: %s deleted group %q", adminActor(r), name)
	a.auditRequest(r, audit.EventGroupDeleted, audit.Success, name, nil)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// providerOwnsGroups reports whether this deployment takes group membership
// from an identity provider, which is what makes a directory-fed group's name
// the directory's rather than an administrator's.
func (a *authState) providerOwnsGroups() bool {
	return a.ssoEnabled() && a.sso.GroupsClaim != ""
}

func (a *authState) writeGroupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, users.ErrGroupNotFound):
		writeJSONError(w, http.StatusNotFound, "no such group")
	case errors.Is(err, users.ErrGroupExists):
		writeJSONError(w, http.StatusConflict, "a group with that name already exists")
	case errors.Is(err, users.ErrProviderManagedGroup):
		writeJSONError(w, http.StatusConflict,
			"this group's membership comes from the identity provider; rename or remove it there")
	default:
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
	}
}

func containsUsername(list []string, name string) bool {
	if name == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), name) {
			return true
		}
	}
	return false
}

// adminGroupRoleAPI assigns a role to a group from the Accounts page, where
// the whole group dialog would be too much furniture for one field.
func (a *authState) adminGroupRoleAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !a.requireAccounts(w) {
		return
	}
	var req struct {
		Group string `json:"group"`
		Role  string `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !a.applyGroupRole(w, r, strings.TrimSpace(req.Group), req.Role) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "groupRoles": a.groupRoles()})
}

/* ---------------------------------------------------------------------
   API tokens
   --------------------------------------------------------------------- */

type adminTokenView struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	Scopes     string `json:"scopes"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	Revoked    bool   `json:"revoked"`
}

func (a *authState) adminTokensAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.adminListTokens(w, r)
	case http.MethodPost:
		a.adminIssueToken(w, r)
	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost)
	}
}

func (a *authState) adminListTokens(w http.ResponseWriter, r *http.Request) {
	if !a.separatesUsers() {
		writeJSON(w, http.StatusOK, map[string]any{
			"authEnabled": false,
			"tokens":      []adminTokenView{},
			// The single-operator shape has one credential and it is not
			// issued here. Saying which one, and whether it is set, is the
			// whole of what this page can usefully tell that deployment.
			"sharedTokenSet": a.sharedToken != "",
		})
		return
	}
	list, err := a.tokenStore().List()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the token list")
		return
	}
	names := map[string]string{}
	if accounts, err := a.userStore().List(); err == nil {
		for _, u := range accounts {
			names[u.ID] = u.Username
		}
	}

	now := time.Now()
	out := make([]adminTokenView, 0, len(list))
	for _, tok := range list {
		owner := names[tok.UserID]
		if owner == "" {
			// The account was deleted; the token no longer authenticates, but
			// saying so is more use than printing a bare identifier.
			owner = "(deleted)"
		}
		view := adminTokenView{
			ID:        tok.ID,
			Owner:     owner,
			Name:      tok.Name,
			Scopes:    strings.Join(tok.Scopes, "+"),
			Status:    tokenStatus(tok, now),
			CreatedAt: tok.CreatedAt.UTC().Format("2006-01-02 15:04"),
			Revoked:   tok.RevokedAt != nil,
		}
		if tok.LastUsedAt != nil {
			view.LastUsedAt = tok.LastUsedAt.UTC().Format("2006-01-02 15:04")
		}
		if tok.ExpiresAt != nil {
			view.ExpiresAt = tok.ExpiresAt.UTC().Format("2006-01-02 15:04")
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })

	accountNames := make([]string, 0, len(names))
	for _, name := range names {
		accountNames = append(accountNames, name)
	}
	sort.Slice(accountNames, func(i, j int) bool {
		return strings.ToLower(accountNames[i]) < strings.ToLower(accountNames[j])
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"authEnabled": true,
		"tokens":      out,
		"accounts":    accountNames,
	})
}

func (a *authState) adminIssueToken(w http.ResponseWriter, r *http.Request) {
	if !a.requireAccounts(w) {
		return
	}
	var req struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		ReadOnly bool   `json:"readOnly"`
		// ExpiresIn is a Go duration, e.g. "720h". Empty means no expiry.
		ExpiresIn string `json:"expiresIn"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	owner, ok, err := a.userByName(strings.TrimSpace(req.Username))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the account list")
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "no such account")
		return
	}

	scopes := []string{apitoken.ScopeRead, apitoken.ScopeWrite}
	if req.ReadOnly {
		scopes = []string{apitoken.ScopeRead}
	}
	var expiresAt *time.Time
	if raw := strings.TrimSpace(req.ExpiresIn); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			writeJSONError(w, http.StatusBadRequest, "cannot read that as a duration; try 24h or 720h")
			return
		}
		when := time.Now().Add(d)
		expiresAt = &when
	}

	record, secret, err := a.tokenStore().Issue(owner.ID, req.Name, scopes, expiresAt)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return
	}
	log.Printf("auth: %s issued token %s for %q", adminActor(r), record.ID, owner.Username)
	a.auditRequest(r, audit.EventTokenIssued, audit.Success, record.ID, map[string]string{
		"account": owner.Username,
		"name":    record.Name,
		"scopes":  strings.Join(record.Scopes, "+"),
	})

	// The only time the secret exists outside the client that will hold it.
	// Not written to the trail, not logged, and not recoverable: the store
	// keeps a hash, so a lost token is replaced rather than found.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     record.ID,
		"secret": secret,
		"owner":  owner.Username,
		"scopes": strings.Join(record.Scopes, "+"),
	})
}

func (a *authState) adminTokenAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodDelete) {
		return
	}
	if !a.requireAccounts(w) {
		return
	}
	id := pathTail(r.URL.Path, "/admin/api/tokens")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing token id")
		return
	}
	if err := a.tokenStore().Revoke(id); err != nil {
		if errors.Is(err, apitoken.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "no such token")
			return
		}
		writeJSONError(w, http.StatusBadRequest, humanStoreError(err))
		return
	}
	log.Printf("auth: %s revoked token %s", adminActor(r), id)
	a.auditRequest(r, audit.EventTokenRevoked, audit.Success, id, nil)
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// userByName finds an account by username.
func (a *authState) userByName(name string) (users.User, bool, error) {
	list, err := a.userStore().List()
	if err != nil {
		return users.User{}, false, err
	}
	for _, u := range list {
		if strings.EqualFold(u.Username, name) {
			return u, true, nil
		}
	}
	return users.User{}, false, nil
}

/* ---------------------------------------------------------------------
   Load runs
   --------------------------------------------------------------------- */

// adminRunView is one load run as the administration page sees it.
type adminRunView struct {
	PID       int    `json:"pid"`
	Owner     string `json:"owner"`
	Status    string `json:"status"`
	Running   bool   `json:"running"`
	Params    string `json:"params"`
	Active    int    `json:"active"`
	Completed int64  `json:"completed"`
	Failed    int64  `json:"failed"`
	StartedAt string `json:"startedAt,omitempty"`
	TimeLeft  int64  `json:"timeLeft"`
	// Self marks the console's own process, which publishes a metrics file
	// like any other and so appears in this list. It cannot be stopped from
	// here — killProcessHandler refuses it — so the page says why rather than
	// offering a button that can only fail.
	Self bool `json:"self"`
}

// runSnapshot is every run this machine knows about, whoever started it.
//
// Read from the metrics files rather than from the owner map, because the
// files are the only channel out of a detached child and because a run started
// from a command line has no owner entry — and is exactly the run an
// administrator is looking for when they open this page.
func (a *authState) runSnapshot() []adminRunView {
	_, extended := readDashboardMetrics(dashboardMetricsDir())
	out := make([]adminRunView, 0, len(extended))
	for _, m := range extended {
		view := adminRunView{
			PID:       m.PID,
			Status:    m.Status,
			Running:   m.IsRunning,
			Params:    m.Params,
			Active:    m.ActiveWorkflows,
			Completed: m.TotalWorkflowsCompleted,
			Failed:    m.TotalWorkflowsFailed,
			TimeLeft:  m.TimeLeft,
			Self:      m.PID == os.Getpid(),
		}
		if m.StartTimestamp > 0 {
			view.StartedAt = time.Unix(m.StartTimestamp, 0).UTC().Format("2006-01-02 15:04:05")
		}
		if owner, ok := a.runs.owner(m.PID); ok {
			view.Owner = owner.Username
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Running != out[j].Running {
			return out[i].Running
		}
		return out[i].PID < out[j].PID
	})
	return out
}

func (a *authState) adminRunsAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	runs := a.runSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"authEnabled": a.separatesUsers(),
		"runs":        runs,
	})
}

/* ---------------------------------------------------------------------
   The audit trail
   --------------------------------------------------------------------- */

// auditPageLimit is how many entries the page asks for and shows. Enough to
// answer "what just happened" without turning a browser tab into a log viewer;
// the download is there for anything longer.
const auditPageLimit = 500

// adminAuditAPI serves the recent trail as JSON.
//
// Administrator-only, because this file names accounts, addresses and the
// hosts they aimed load at — which is exactly the material somebody would want
// in order to work out what to impersonate.
func (a *authState) adminAuditAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = min(n, 2000)
		}
	}
	entries, err := a.recorder().Read(limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not read the audit log")
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"path":    a.recorder().Path(),
	})
}

// adminAuditDownload streams the whole trail, oldest first, for keeping or for
// feeding to something that keeps it.
func (a *authState) adminAuditDownload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="3270Connect-audit.jsonl"`)
	if err := a.recorder().Dump(w); err != nil {
		// Too late for a status code — the header is already out — so this
		// ends the response and leaves the note in the log.
		log.Printf("auth: could not stream the audit log: %v", err)
	}
}

// tokenStatus renders a token's state for a list.
func tokenStatus(tok apitoken.Token, now time.Time) string {
	switch err := tok.Active(now); {
	case errors.Is(err, apitoken.ErrRevoked):
		return "revoked"
	case errors.Is(err, apitoken.ErrExpired):
		return "expired"
	case tok.ExpiresAt != nil:
		return "expires " + tok.ExpiresAt.Format("2006-01-02")
	default:
		return "active"
	}
}
