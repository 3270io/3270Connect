package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/3270io/3270Connect/internal/apitoken"
	"github.com/3270io/3270Connect/internal/authsession"
	"github.com/3270io/3270Connect/internal/authz"
	"github.com/3270io/3270Connect/internal/users"
)

// newTestAuth builds an authState with its stores in a temporary directory, so
// a test never touches the state a real console keeps.
func newTestAuth(t *testing.T, mode authz.Mode) *authState {
	t.Helper()
	dir := t.TempDir()
	a := newAuthState()
	a.mode = mode
	a.usersStore = users.NewStore(filepath.Join(dir, "users.json"))
	a.tokensStore = apitoken.NewStore(filepath.Join(dir, "api-tokens.json"))
	a.sessions = authsession.NewStore(time.Hour, 12*time.Hour)
	// The recorder writes to the temporary directory too; a nil one would be
	// usable but would stop the tests from noticing a panic inside Log.
	a.trail = nil
	t.Setenv("AUDIT_LOG_PATH", filepath.Join(dir, "audit.log"))
	if mode != authz.ModeNone {
		a.setup.complete()
	}
	return a
}

// addUser creates an account and returns it.
func addUser(t *testing.T, a *authState, name string, role authz.Role) users.User {
	t.Helper()
	u, err := a.userStore().Add(name, "correct-horse-battery", role, false)
	if err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	return u
}

// signIn creates a login and returns the cookie value for it.
func signIn(t *testing.T, a *authState, u users.User) string {
	t.Helper()
	sess, err := a.sessions.Create(u.ID, u.Username, a.effectiveRoleFor(u), "203.0.113.9", u.MustChangePassword)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess.ID
}

// get builds a request through the gate. cookie may be empty.
func gateRequest(a *authState, method, path, cookie string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "203.0.113.9:5555"
	r.Host = "console.example:9200"
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	}
	if method != http.MethodGet {
		// Every non-browser-navigation in these tests is same-origin; the CSRF
		// check is exercised on its own below.
		r.Header.Set("Origin", "http://console.example:9200")
	}
	w := httptest.NewRecorder()
	a.Gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // reached the handler
	})).ServeHTTP(w, r)
	return w
}

/* ---------------------------------------------------------------------
   The default deployment must not change
   --------------------------------------------------------------------- */

func TestGateAdmitsEverythingWithoutAuth(t *testing.T) {
	a := newTestAuth(t, authz.ModeNone)

	for _, path := range []string{"/dashboard", "/dashboard/data", "/start-process", "/kill", "/admin"} {
		method := http.MethodGet
		if path == "/start-process" || path == "/kill" {
			method = http.MethodPost
		}
		w := gateRequest(a, method, path, "")
		if w.Code != http.StatusTeapot {
			t.Errorf("%s %s: got %d, want the handler to be reached", method, path, w.Code)
		}
	}
}

func TestLocalOperatorIsAnAdministrator(t *testing.T) {
	a := newTestAuth(t, authz.ModeNone)
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	identity, err := a.resolve(r)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !identity.Principal.IsAdmin() {
		t.Fatal("the single local operator must be able to administer their own console")
	}
	if !a.consoleAuthView(withIdentity(r, identity)).CanAdminister {
		t.Fatal("the administration controls must be offered to the single operator")
	}
}

/* ---------------------------------------------------------------------
   With accounts
   --------------------------------------------------------------------- */

func TestGateRedirectsAnonymousBrowserToSignIn(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)

	w := gateRequest(a, http.MethodGet, "/dashboard", "")
	if w.Code != http.StatusFound {
		t.Fatalf("got %d, want a redirect to the sign-in page", w.Code)
	}
	if location := w.Header().Get("Location"); !strings.HasPrefix(location, loginPath) {
		t.Fatalf("redirected to %q, want the sign-in page", location)
	}
}

func TestGateAnswersScriptsWithJSON(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)

	r := httptest.NewRequest(http.MethodGet, "/dashboard/data", nil)
	r.Header.Set("X-Requested-With", "XMLHttpRequest")
	w := httptest.NewRecorder()
	a.Gate(http.NotFoundHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for a script", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content type %q, want JSON", ct)
	}
}

func TestPublicPathsNeedNoLogin(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	for _, path := range []string{"/login", "/healthz", "/static/css/auth.css"} {
		w := gateRequest(a, http.MethodGet, path, "")
		if w.Code != http.StatusTeapot {
			t.Errorf("%s: got %d, want the handler to be reached without a login", path, w.Code)
		}
	}
}

func TestOrdinaryAccountIsRefusedTheAdminArea(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	user := addUser(t, a, "alice", authz.RoleUser)
	cookie := signIn(t, a, user)

	w := gateRequest(a, http.MethodGet, "/admin/users", cookie)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for a user reaching the administration area", w.Code)
	}

	admin := addUser(t, a, "root", authz.RoleAdmin)
	w = gateRequest(a, http.MethodGet, "/admin/users", signIn(t, a, admin))
	if w.Code != http.StatusTeapot {
		t.Fatalf("got %d, want an administrator to reach the administration area", w.Code)
	}
}

func TestAdminAreaIsGatedByPrefixNotByRoute(t *testing.T) {
	// The point of gating the prefix is that a route added later is covered
	// without anybody remembering to cover it. This asserts the property
	// rather than the routes that happen to exist today.
	a := newTestAuth(t, authz.ModeLocal)
	cookie := signIn(t, a, addUser(t, a, "alice", authz.RoleUser))

	for _, path := range []string{"/admin", "/admin/", "/admin/something-invented-later", "/admin/api/overview"} {
		w := gateRequest(a, http.MethodGet, path, cookie)
		if w.Code == http.StatusTeapot {
			t.Errorf("%s reached the handler; everything under /admin needs the admin role", path)
		}
	}
}

func TestAdminPrefixDoesNotCatchLookalikePaths(t *testing.T) {
	// "/administration" is not under "/admin", and a prefix check that says
	// otherwise would gate a route nobody meant to gate.
	a := newTestAuth(t, authz.ModeLocal)
	cookie := signIn(t, a, addUser(t, a, "alice", authz.RoleUser))

	w := gateRequest(a, http.MethodGet, "/administration", cookie)
	if w.Code != http.StatusTeapot {
		t.Fatalf("got %d, want /administration to be an ordinary page", w.Code)
	}
}

func TestForcedPasswordChangePinsTheLogin(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	if _, err := a.userStore().Add("alice", "correct-horse-battery", authz.RoleUser, true); err != nil {
		t.Fatalf("add: %v", err)
	}
	user, _, _ := a.userByName("alice")
	cookie := signIn(t, a, user)

	w := gateRequest(a, http.MethodGet, "/dashboard", cookie)
	if w.Code != http.StatusFound || w.Header().Get("Location") != changePasswordPath {
		t.Fatalf("got %d to %q, want a redirect to the password change", w.Code, w.Header().Get("Location"))
	}

	// The two things it may still do: change the password, and sign out.
	for _, path := range []string{changePasswordPath, logoutPath} {
		if w := gateRequest(a, http.MethodGet, path, cookie); w.Code != http.StatusTeapot {
			t.Errorf("%s: got %d, want it to be reachable while the change is pending", path, w.Code)
		}
	}
}

/* ---------------------------------------------------------------------
   Cross-site request forgery
   --------------------------------------------------------------------- */

func TestCSRF(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	cookie := signIn(t, a, addUser(t, a, "alice", authz.RoleUser))

	request := func(method string, headers map[string]string) int {
		r := httptest.NewRequest(method, "/start-process", nil)
		r.Host = "console.example:9200"
		r.RemoteAddr = "203.0.113.9:5555"
		r.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		a.Gate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})).ServeHTTP(w, r)
		return w.Code
	}

	cases := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"same-origin POST", http.MethodPost, map[string]string{"Origin": "http://console.example:9200"}, http.StatusTeapot},
		{"cross-origin POST", http.MethodPost, map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		{"same-origin referer", http.MethodPost, map[string]string{"Referer": "http://console.example:9200/dashboard"}, http.StatusTeapot},
		{"cross-origin referer", http.MethodPost, map[string]string{"Referer": "http://evil.example/page"}, http.StatusForbidden},
		{"a GET is never checked", http.MethodGet, nil, http.StatusTeapot},

		// Sec-Fetch-Site comes from the browser rather than from the page, so
		// it outranks a forgeable Origin when both are present.
		{"browser, own page", http.MethodPost,
			map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://console.example:9200"}, http.StatusTeapot},
		{"browser, typed or bookmarked", http.MethodPost,
			map[string]string{"Sec-Fetch-Site": "none"}, http.StatusTeapot},
		{"browser, another site", http.MethodPost,
			map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example"}, http.StatusForbidden},
		{"browser, sibling subdomain", http.MethodPost,
			map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"a lying page cannot claim same-origin", http.MethodPost,
			map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://console.example:9200"}, http.StatusForbidden},

		// curl, CI and the installer's own checks send none of the three. They
		// are not browsers, so they are not the threat, and refusing them
		// would break every scripted client of endpoints that predate this.
		{"a script sends no browser signal", http.MethodPost, nil, http.StatusTeapot},
	}
	for _, tc := range cases {
		if got := request(tc.method, tc.headers); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestBearerCallerIsExemptFromCSRF(t *testing.T) {
	// A token carries no cookie, so there is nothing for another site to ride
	// on — and curl sends no Origin, so requiring one would refuse every
	// legitimate automated client.
	a := newTestAuth(t, authz.ModeLocal)
	owner := addUser(t, a, "ci", authz.RoleUser)
	_, secret, err := a.tokenStore().Issue(owner.ID, "pipeline", []string{apitoken.ScopeRead, apitoken.ScopeWrite}, nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/start-process", nil)
	r.Host = "console.example:9200"
	r.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	a.Gate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(w, r)

	if w.Code != http.StatusTeapot {
		t.Fatalf("got %d, want a bearer caller to be admitted without an Origin", w.Code)
	}
}

/* ---------------------------------------------------------------------
   Tokens
   --------------------------------------------------------------------- */

func TestTokenScopeFollowsTheMethod(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	owner := addUser(t, a, "ci", authz.RoleUser)
	_, secret, err := a.tokenStore().Issue(owner.ID, "watcher", []string{apitoken.ScopeRead}, nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	call := func(method string) int {
		r := httptest.NewRequest(method, "/dashboard/data", nil)
		r.Host = "console.example:9200"
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		a.Gate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})).ServeHTTP(w, r)
		return w.Code
	}

	if got := call(http.MethodGet); got != http.StatusTeapot {
		t.Errorf("GET with a read token: got %d, want it admitted", got)
	}
	if got := call(http.MethodPost); got != http.StatusForbidden {
		t.Errorf("POST with a read-only token: got %d, want 403", got)
	}
}

func TestTokenStopsWorkingWhenItsAccountIsDisabled(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	owner := addUser(t, a, "ci", authz.RoleUser)
	// A second administrator, or SetDisabled refuses to disable the only one.
	addUser(t, a, "root", authz.RoleAdmin)
	_, secret, err := a.tokenStore().Issue(owner.ID, "pipeline", []string{apitoken.ScopeRead}, nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := a.authenticateToken(secret); err != nil {
		t.Fatalf("a live account's token must work: %v", err)
	}
	if err := a.userStore().SetDisabled("ci", true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := a.authenticateToken(secret); err == nil {
		t.Fatal("a disabled account's token must stop working")
	}
}

func TestSharedTokenOnlyExistsForASingleOperator(t *testing.T) {
	a := newTestAuth(t, authz.ModeNone)

	// No API_TOKEN: the historical default, where the REST API is open.
	if a.apiRequiresToken() {
		t.Fatal("an instance with one operator and no API_TOKEN must not require a credential")
	}
	principal, err := a.authenticateToken("")
	if err != nil || !principal.IsAdmin() {
		t.Fatalf("got %v, %v; want the local operator", principal, err)
	}

	a.sharedToken = "s3cret"
	if !a.apiRequiresToken() {
		t.Fatal("API_TOKEN must make the REST API require a credential")
	}
	if _, err := a.authenticateToken("wrong"); err == nil {
		t.Fatal("a wrong shared token must be refused")
	}
	if _, err := a.authenticateToken("s3cret"); err != nil {
		t.Fatalf("the configured shared token must be accepted: %v", err)
	}
}

func TestAPIListenerRefusesARequestThatSendsNoCredential(t *testing.T) {
	// The console and the API listener disagree about what a missing header
	// means, and should: with one operator the console must open, while an
	// API_TOKEN that is set is somebody asking for the API to be closed. A
	// request with no Authorization header at all must not be answered as that
	// operator.
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name  string
		setup func(*authState)
		want  int
	}{
		{"one operator, no API_TOKEN", func(*authState) {}, http.StatusTeapot},
		{"one operator, API_TOKEN set", func(a *authState) { a.sharedToken = "s3cret" }, http.StatusUnauthorized},
		{"accounts", func(a *authState) { a.mode = authz.ModeLocal }, http.StatusUnauthorized},
	} {
		a := newTestAuth(t, authz.ModeNone)
		tc.setup(a)

		router := gin.New()
		router.Use(a.GinAuth())
		router.POST("/api/execute", func(c *gin.Context) { c.Status(http.StatusTeapot) })

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/execute", nil))
		if w.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}

func TestAPIListenerAcceptsTheSharedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a := newTestAuth(t, authz.ModeNone)
	a.sharedToken = "s3cret"

	router := gin.New()
	router.Use(a.GinAuth())
	router.POST("/api/execute", func(c *gin.Context) { c.Status(http.StatusTeapot) })

	call := func(header string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/execute", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}

	if got := call("Bearer s3cret"); got != http.StatusTeapot {
		t.Errorf("the configured token: got %d, want it admitted", got)
	}
	if got := call("Bearer wrong"); got != http.StatusUnauthorized {
		t.Errorf("a wrong token: got %d, want 401", got)
	}
}

func TestSharedTokenIsRefusedAlongsideAccounts(t *testing.T) {
	// One credential held by everybody would be a hole straight through the
	// separation the mode was turned on for, so it stops startup.
	t.Setenv(authz.ModeEnv, "local")
	t.Setenv("API_TOKEN", "shared")
	t.Setenv("USERS_PATH", filepath.Join(t.TempDir(), "users.json"))

	a := newAuthState()
	err := a.configure()
	if err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
		t.Fatalf("got %v, want a refusal naming API_TOKEN", err)
	}
}

/* ---------------------------------------------------------------------
   Configuration
   --------------------------------------------------------------------- */

func TestUnknownAuthModeStopsStartup(t *testing.T) {
	t.Setenv(authz.ModeEnv, "sortof")
	a := newAuthState()
	if err := a.configure(); err == nil {
		t.Fatal("an unsupported AUTH_MODE must stop startup rather than run without authentication")
	}
}

func TestUnknownBindSessionIPStopsStartup(t *testing.T) {
	t.Setenv(authz.ModeEnv, "none")
	t.Setenv("AUTH_BIND_SESSION_IP", "sometimes")
	a := newAuthState()
	if err := a.configure(); err == nil {
		t.Fatal("an unsupported AUTH_BIND_SESSION_IP must stop startup")
	}
}

func TestFirstRunSetupArmsAndFunnels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(authz.ModeEnv, "local")
	t.Setenv("USERS_PATH", filepath.Join(dir, "users.json"))
	t.Setenv("AUDIT_LOG_PATH", filepath.Join(dir, "audit.log"))
	t.Setenv("API_TOKEN", "")

	a := newAuthState()
	if err := a.configure(); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if !a.setup.pending() {
		t.Fatal("a mode that needs accounts and has none must arm first-run setup")
	}

	w := gateRequest(a, http.MethodGet, "/dashboard", "")
	if w.Code != http.StatusFound || w.Header().Get("Location") != setupPath {
		t.Fatalf("got %d to %q, want a redirect to setup", w.Code, w.Header().Get("Location"))
	}

	// An account created by the console command closes setup without a
	// restart, which is the documented way to avoid the web form.
	if _, err := a.userStore().Add("root", "correct-horse-battery", authz.RoleAdmin, false); err != nil {
		t.Fatalf("add: %v", err)
	}
	if a.setupStillPending() {
		t.Fatal("setup must close as soon as an account exists")
	}
	w = gateRequest(a, http.MethodGet, setupPath, "")
	if w.Code != http.StatusFound || w.Header().Get("Location") != loginPath {
		t.Fatal("the setup page must close for good once an account exists")
	}
}

func TestSetupCodeIsForgivingButNotGuessable(t *testing.T) {
	var s setupState
	s.begin("ABCD2345EFGH")

	for _, typed := range []string{"ABCD2345EFGH", "abcd-2345-efgh", " ABCD 2345 EFGH "} {
		if !s.matches(typed) {
			t.Errorf("%q should be accepted: case, spaces and dashes are transcription noise", typed)
		}
	}
	for _, typed := range []string{"", "ABCD2345EFG", "ZZZZZZZZZZZZ"} {
		if s.matches(typed) {
			t.Errorf("%q should be refused", typed)
		}
	}

	s.complete()
	if s.matches("ABCD2345EFGH") {
		t.Fatal("the code must stop working once setup is complete")
	}
}

/* ---------------------------------------------------------------------
   Ownership of a load run
   --------------------------------------------------------------------- */

func TestMayStopRun(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	alice := addUser(t, a, "alice", authz.RoleUser)
	bob := addUser(t, a, "bob", authz.RoleUser)
	root := addUser(t, a, "root", authz.RoleAdmin)

	a.runs.claim(4242, alice.ID, alice.Username)

	request := func(u users.User) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/kill?pid=4242", nil)
		return withIdentity(r, requestIdentity{
			Principal: authz.Principal{UserID: u.ID, Role: a.effectiveRoleFor(u), Kind: authz.KindWeb},
			Username:  u.Username,
		})
	}

	if ok, why := a.mayStopRun(request(alice), 4242); !ok {
		t.Errorf("the owner must be able to stop their own run: %s", why)
	}
	if ok, _ := a.mayStopRun(request(bob), 4242); ok {
		t.Error("a colleague must not be able to stop somebody else's run")
	}
	if ok, why := a.mayStopRun(request(root), 4242); !ok {
		t.Errorf("an administrator must be able to stop any run: %s", why)
	}

	// A run this console did not start has no owner, and unowned must not read
	// as "belongs to whoever asked".
	if ok, _ := a.mayStopRun(request(alice), 9999); ok {
		t.Error("a run with no recorded owner must need an administrator")
	}
	if ok, _ := a.mayStopRun(request(root), 9999); !ok {
		t.Error("an administrator must be able to stop a run started outside the console")
	}
}

func TestRunOwnersPruneKeepsRecentAndDropsDead(t *testing.T) {
	owners := newRunOwners()
	owners.claim(1, "u1", "alice")
	owners.claim(2, "u2", "bob")

	// Nothing is alive, but only entries older than the window are dropped: a
	// finished run stays on the console for a while and its row should still
	// say whose it was.
	dead := func(int) bool { return false }
	owners.prune(dead, time.Hour, time.Now())
	if _, ok := owners.owner(1); !ok {
		t.Fatal("a recently finished run must keep its owner label")
	}

	owners.prune(dead, time.Hour, time.Now().Add(2*time.Hour))
	if _, ok := owners.owner(1); ok {
		t.Fatal("a long-dead run must be forgotten")
	}
}

func TestOwnerNamesAreOmittedForASingleOperator(t *testing.T) {
	a := newTestAuth(t, authz.ModeNone)
	a.runs.claim(7, authz.LocalUserID, "local operator")
	if got := a.ownerNamesFor([]ExtendedMetrics{{Metrics: Metrics{PID: 7}}}); got != nil {
		t.Fatalf("got %v, want no owner column where there is nobody to tell apart", got)
	}
}

/* ---------------------------------------------------------------------
   Roles that come from a group
   --------------------------------------------------------------------- */

func TestGroupRoleReachesAnOpenSession(t *testing.T) {
	// A login carries the role it was created with, so a role granted to a
	// group has to be pushed into sessions that already exist — otherwise a
	// demotion waits for the next sign-in, and the demoted administrator can
	// undo it from the page they are standing on.
	a := newTestAuth(t, authz.ModeLocal)
	addUser(t, a, "root", authz.RoleAdmin)
	alice := addUser(t, a, "alice", authz.RoleUser)
	if err := a.userStore().SetGroups("alice", []string{"ops"}); err != nil {
		t.Fatalf("set groups: %v", err)
	}
	cookie := signIn(t, a, alice)

	if w := gateRequest(a, http.MethodGet, "/admin", cookie); w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want the administration area refused before the grant", w.Code)
	}

	if err := a.userStore().SetGroupRole("ops", authz.RoleAdmin); err != nil {
		t.Fatalf("grant: %v", err)
	}
	a.pushEffectiveRoles()

	if w := gateRequest(a, http.MethodGet, "/admin", cookie); w.Code != http.StatusTeapot {
		t.Fatalf("got %d, want the grant to reach the session already open", w.Code)
	}

	if err := a.userStore().SetGroupRole("ops", authz.RoleUser); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	a.pushEffectiveRoles()

	if w := gateRequest(a, http.MethodGet, "/admin", cookie); w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want the revocation to reach the session already open", w.Code)
	}
}

func TestSelfAdminDependsOnGroup(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	addUser(t, a, "root", authz.RoleAdmin)
	alice := addUser(t, a, "alice", authz.RoleUser)
	if err := a.userStore().SetGroups("alice", []string{"ops"}); err != nil {
		t.Fatalf("set groups: %v", err)
	}
	if err := a.userStore().SetGroupRole("ops", authz.RoleAdmin); err != nil {
		t.Fatalf("grant: %v", err)
	}

	req := func(u users.User) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/admin/api/group-roles", nil)
		return withIdentity(r, requestIdentity{
			Principal: authz.Principal{UserID: u.ID, Role: authz.RoleAdmin, Kind: authz.KindWeb},
			Username:  u.Username,
		})
	}

	if !a.selfAdminDependsOnGroup(req(alice), "ops") {
		t.Error("alice administers only through ops, so clearing it is self-demotion")
	}
	if a.selfAdminDependsOnGroup(req(alice), "someone-else") {
		t.Error("a group alice is not in cannot be what makes her an administrator")
	}
	root, _, _ := a.userByName("root")
	if a.selfAdminDependsOnGroup(req(root), "ops") {
		t.Error("root's own role stands on its own")
	}
}

/* ---------------------------------------------------------------------
   The sweep
   --------------------------------------------------------------------- */

func TestSweepEndsLoginsForAccountsDisabledOnDisk(t *testing.T) {
	// `3270Connect user disable` edits the file directly and cannot reach into
	// a running console's memory. The sweep is what closes that gap.
	a := newTestAuth(t, authz.ModeLocal)
	addUser(t, a, "root", authz.RoleAdmin)
	alice := addUser(t, a, "alice", authz.RoleUser)
	cookie := signIn(t, a, alice)

	if w := gateRequest(a, http.MethodGet, "/dashboard", cookie); w.Code != http.StatusTeapot {
		t.Fatalf("got %d, want the login to work before the account is disabled", w.Code)
	}

	if err := users.NewStore(a.userStore().Path()).SetDisabled("alice", true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	a.sweep()

	if w := gateRequest(a, http.MethodGet, "/dashboard", cookie); w.Code != http.StatusFound {
		t.Fatalf("got %d, want the login ended once the account is disabled", w.Code)
	}
}

/* ---------------------------------------------------------------------
   Pages
   --------------------------------------------------------------------- */

func TestEveryPageTemplateParses(t *testing.T) {
	want := []string{
		"login.gohtml", "setup.gohtml", "change-password.gohtml", "denied.gohtml",
		"admin-overview.gohtml", "admin-users.gohtml", "admin-groups.gohtml",
		"admin-tokens.gohtml", "admin-runs.gohtml", "admin-audit.gohtml",
	}
	for _, name := range want {
		if authPages[name] == nil {
			t.Errorf("%s did not parse; the log line from parseAuthPages says why", name)
		}
	}
	if len(authPages) != len(want) {
		t.Errorf("parsed %d pages, expected %d — a new one needs a line above", len(authPages), len(want))
	}
}

func TestPagesRender(t *testing.T) {
	cases := []struct {
		page string
		data authPageData
		want string
	}{
		{"login.gohtml", authPageData{Title: "Sign in", Next: "/dashboard"}, `action="/login"`},
		{"setup.gohtml", authPageData{Title: "First run", MinLength: 12}, "setup code"},
		{"change-password.gohtml", authPageData{Title: "Change your password", MinLength: 12, Forced: true}, "currentPassword"},
		{"denied.gohtml", authPageData{Title: "Not for you", Message: "This page requires an administrator account."}, "administrator account"},
		{"admin-overview.gohtml", authPageData{Title: "Administration", Active: "overview"}, "Recent activity"},
		{"admin-users.gohtml", authPageData{Title: "Accounts", Active: "users", MinLength: 12}, "Add account"},
		{"admin-groups.gohtml", authPageData{Title: "Groups", Active: "groups", MaxGroupName: 64}, "Create group"},
		{"admin-tokens.gohtml", authPageData{Title: "API tokens", Active: "tokens"}, "Issue token"},
		{"admin-runs.gohtml", authPageData{Title: "Load runs", Active: "runs"}, "Load runs"},
		{"admin-audit.gohtml", authPageData{Title: "Audit trail", Active: "audit", Limit: 500}, "Audit trail"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		renderAuthPage(w, r, http.StatusOK, tc.page, tc.data)
		if w.Code != http.StatusOK {
			t.Errorf("%s: got %d", tc.page, w.Code)
			continue
		}
		body := w.Body.String()
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: body does not contain %q", tc.page, tc.want)
		}
		if !strings.Contains(body, "<!doctype html>") {
			t.Errorf("%s: the layout did not wrap the page", tc.page)
		}
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s: these pages must carry a content-security policy", tc.page)
		}
	}
}

func TestAdminNavMarksTheCurrentPage(t *testing.T) {
	html := string(adminNavHTML("groups"))
	if !strings.Contains(html, `aria-current="page"`) {
		t.Fatal("the current tab must be announced")
	}
	if strings.Count(html, "is-current") != 1 {
		t.Fatalf("exactly one tab should be current, got %d", strings.Count(html, "is-current"))
	}
	for _, item := range adminNavItems {
		if !strings.Contains(html, item.Href) {
			t.Errorf("%s is missing from the navigation", item.Href)
		}
	}
}

/* ---------------------------------------------------------------------
   Small units
   --------------------------------------------------------------------- */

func TestSafeReturnPath(t *testing.T) {
	// An open redirect immediately after somebody types a password is the most
	// convincing place in the application to have one.
	cases := map[string]string{
		"/dashboard":           "/dashboard",
		"/admin/users?x=1":     "/admin/users?x=1",
		"":                     "/dashboard",
		"//evil.example":       "/dashboard",
		"http://evil.example":  "/dashboard",
		"https://evil.example": "/dashboard",
		`/\evil.example`:       "/dashboard",
		"javascript:alert(1)":  "/dashboard",
	}
	for input, want := range cases {
		if got := safeReturnPath(input); got != want {
			t.Errorf("safeReturnPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReturnPathOnlyRemembersAGet(t *testing.T) {
	// Replaying a POST after a sign-in would resubmit whatever it carried,
	// which here means starting a load run somebody did not ask for twice.
	get := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	if got := returnPathFor(get); got != "/admin/audit" {
		t.Errorf("got %q, want the page remembered", got)
	}
	post := httptest.NewRequest(http.MethodPost, "/start-process", nil)
	if got := returnPathFor(post); got != "/dashboard" {
		t.Errorf("got %q, want a POST not to be replayed", got)
	}
}

func TestSanitiseUsername(t *testing.T) {
	cases := map[string]string{
		"alice":              "alice",
		"alice@corp.example": "alice-corp.example",
		"Ada Lovelace":       "Ada-Lovelace",
		"  spaced  ":         "spaced",
		"":                   "",
		"!!!":                "",
	}
	for input, want := range cases {
		if got := sanitiseUsername(input); got != want {
			t.Errorf("sanitiseUsername(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHumanStoreErrorStripsThePackagePrefix(t *testing.T) {
	err := users.ValidatePassword("short")
	got := humanStoreError(err)
	if strings.Contains(got, "users:") {
		t.Fatalf("got %q; a form must not show a package name to somebody choosing a password", got)
	}
	if !strings.HasSuffix(got, ".") || got[0] < 'A' || got[0] > 'Z' {
		t.Fatalf("got %q, want a sentence", got)
	}
}

func TestWantsJSON(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		headers map[string]string
		want    bool
	}{
		{"a browser navigating", "/dashboard", map[string]string{"Accept": "text/html"}, false},
		{"the API surface", "/api/execute", nil, true},
		{"the admin API", "/admin/api/users", nil, true},
		{"fetch()", "/dashboard", map[string]string{"X-Requested-With": "XMLHttpRequest"}, true},
		{"a bearer client", "/dashboard", map[string]string{"Authorization": "Bearer x"}, true},
		{"accepts both", "/dashboard", map[string]string{"Accept": "text/html,application/json"}, false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		for k, v := range tc.headers {
			r.Header.Set(k, v)
		}
		if got := wantsJSON(r); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStatePathsFollowTheStateDirectory(t *testing.T) {
	// All three live together, wherever the state directory turns out to be.
	// Asserted against stateDir() rather than against a path built here,
	// because where that is differs by platform — %AppData% on Windows, the
	// XDG directory elsewhere — and the property that matters is that the
	// three agree, not which convention the host uses.
	t.Setenv("USERS_PATH", "")
	t.Setenv("API_TOKENS_PATH", "")
	t.Setenv("AUDIT_LOG_PATH", "")

	dir := stateDir()
	for name, got := range map[string]string{
		"users":  resolveUsersPath(),
		"tokens": resolveTokensPath(),
		"audit":  resolveAuditPath(),
	} {
		if filepath.Dir(got) != dir {
			t.Errorf("%s path %q is not in the state directory %q", name, got, dir)
		}
	}
}

func TestStateDirFollowsXDGConfigHome(t *testing.T) {
	// The container image points XDG_CONFIG_HOME at the mounted volume, and
	// the accounts file has to land there rather than in the image layer that
	// the next deploy discards. This is the whole of what makes that work.
	//
	// Unix only: os.UserConfigDir reads %AppData% on Windows and ignores the
	// XDG variable entirely, so there would be nothing to assert.
	if runtime.GOOS == "windows" {
		t.Skip("XDG_CONFIG_HOME is not how Windows names its configuration directory")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if got := stateDir(); !strings.HasPrefix(got, dir) {
		t.Fatalf("state directory %q is not under XDG_CONFIG_HOME %q", got, dir)
	}
}

func TestOverridesWinOverTheStateDirectory(t *testing.T) {
	// A deployment that keeps its accounts somewhere else — a secret mount, a
	// path an operator already backs up — says so with these.
	dir := t.TempDir()
	t.Setenv("USERS_PATH", filepath.Join(dir, "u.json"))
	t.Setenv("API_TOKENS_PATH", filepath.Join(dir, "t.json"))
	t.Setenv("AUDIT_LOG_PATH", filepath.Join(dir, "a.log"))

	for name, got := range map[string]string{
		"USERS_PATH":      resolveUsersPath(),
		"API_TOKENS_PATH": resolveTokensPath(),
		"AUDIT_LOG_PATH":  resolveAuditPath(),
	} {
		if filepath.Dir(got) != dir {
			t.Errorf("%s was ignored: got %q", name, got)
		}
	}
}

func TestWhoAmI(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	admin := addUser(t, a, "root", authz.RoleAdmin)
	cookie := signIn(t, a, admin)

	r := httptest.NewRequest(http.MethodGet, whoamiPath, nil)
	r.RemoteAddr = "203.0.113.9:5555"
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	identity, err := a.resolve(r)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	w := httptest.NewRecorder()
	a.whoamiHandler(w, withIdentity(r, identity))

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["authenticated"] != true || body["username"] != "root" || body["isAdmin"] != true {
		t.Fatalf("got %v, want the signed-in administrator", body)
	}
}

func TestAuditTrailRecordsARefusedToken(t *testing.T) {
	a := newTestAuth(t, authz.ModeLocal)
	addUser(t, a, "root", authz.RoleAdmin)

	r := httptest.NewRequest(http.MethodGet, "/dashboard/data", nil)
	r.Header.Set("Authorization", "Bearer nonsense")
	w := httptest.NewRecorder()
	a.Gate(http.NotFoundHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
	entries, err := a.recorder().Read(10)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	if len(entries) == 0 || entries[0].Event != "token.refused" {
		t.Fatalf("got %v, want a refused-token line", entries)
	}
}

func TestStateDirFallsBackWhenTheConfigDirIsUnknown(t *testing.T) {
	// os.UserConfigDir fails when neither XDG_CONFIG_HOME nor HOME is set,
	// which is what a stripped-down container looks like. A panic there would
	// take the console down before it said why.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if got := stateDir(); got == "" {
		t.Fatal("stateDir must always name somewhere")
	}
	if _, err := os.Stat(filepath.Dir(stateDir())); err != nil && !os.IsNotExist(err) {
		t.Fatalf("unexpected error stating the fallback: %v", err)
	}
}
