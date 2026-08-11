package main

// Who is at the console, and what they are allowed to do with it.
//
// 3270Connect began as a program somebody ran on their own laptop, and the
// console inherited that: no sign-in, and /start-process launching a load run
// for anybody who could reach the port. That is still the default and still
// the right default — a single operator on a machine they control does not
// need an account to be themselves.
//
// It stops being right the moment the port is shared. A load run is not a
// read-only thing: it points a chosen number of virtual users at a chosen
// host, which is traffic somebody else's mainframe has to absorb, and /kill
// stops a run somebody else may be depending on. AUTH_MODE=local puts accounts
// in front of that, AUTH_MODE=oidc takes them from the directory the
// organisation already runs, and both leave the default deployment untouched.
//
// The shape is deliberately the same as 3270Web's, down to the environment
// variable names, because the two are run side by side and an operator should
// not have to learn the second one twice. The packages under internal/ are the
// same packages; this file is where they meet net/http.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/3270io/3270Connect/internal/apitoken"
	"github.com/3270io/3270Connect/internal/audit"
	"github.com/3270io/3270Connect/internal/authsession"
	"github.com/3270io/3270Connect/internal/authz"
	"github.com/3270io/3270Connect/internal/oidc"
	"github.com/3270io/3270Connect/internal/reqsec"
	"github.com/3270io/3270Connect/internal/users"
)

// authCookieName holds the login session identifier.
const authCookieName = "3270Connect_auth"

// Paths the console serves for authentication itself.
const (
	loginPath          = "/login"
	logoutPath         = "/logout"
	changePasswordPath = "/account/password"
	whoamiPath         = "/whoami"
	setupPath          = "/setup"
	ssoStartPath       = "/auth/sso"
	ssoCallbackPath    = "/auth/sso/callback"
)

// publicPaths are served without a login even when the mode requires one.
//
// An explicit set rather than a prefix rule, so adding a route never silently
// makes it public: anything not named here needs a login.
var publicPaths = map[string]bool{
	loginPath:       true,
	logoutPath:      true,
	setupPath:       true,
	ssoStartPath:    true,
	ssoCallbackPath: true,
	"/healthz":      true,
}

// publicPrefixes cover page furniture, which has to load before any gate or
// the sign-in page arrives unstyled.
var publicPrefixes = []string{"/static/", "/favicon"}

// adminPathPrefix is the administration area. Everything under it needs the
// admin role; see requireAdmin.
const adminPathPrefix = "/admin"

// authState is the console's authentication and authorization state.
//
// One value rather than a scatter of globals, so a test can stand up an
// instance with its own stores and its own clock without touching the process
// the rest of the program is using.
type authState struct {
	// mode is validated at startup, so handlers can treat it as a known value.
	mode authz.Mode

	usersStore  *users.Store
	tokensStore *apitoken.Store
	trail       *audit.Recorder
	sessions    *authsession.Store
	limiter     *loginLimiter

	// setup tracks whether the instance still needs its first administrator.
	setup setupState

	// provider and sso are the identity-provider client and its settings,
	// present only under AUTH_MODE=oidc; ssoPending holds sign-ins between the
	// redirect out and the callback back.
	provider   *oidc.Provider
	sso        ssoSettings
	ssoPending *pendingLoginStore

	idleTimeout     time.Duration
	absoluteTimeout time.Duration
	// bindIP is "auto", "true" or "false"; see bindSessionIP.
	bindIP string

	// sharedToken is the API_TOKEN a single-operator deployment may set to put
	// a credential in front of the REST API. Empty means the API is open,
	// which is what it has always been.
	sharedToken string

	// runs records who started which load run, so /kill can tell somebody
	// stopping their own run from somebody stopping a colleague's.
	runs *runOwners

	startedAt time.Time
}

// auth is the console's state. A package-level value because everything else
// in this program is, and because the dashboard's handlers are registered on
// the default mux with no receiver to hang it from.
var auth = newAuthState()

func newAuthState() *authState {
	return &authState{
		mode:            authz.ModeNone,
		limiter:         newLoginLimiter(),
		runs:            newRunOwners(),
		idleTimeout:     authsession.DefaultIdleTimeout,
		absoluteTimeout: authsession.DefaultAbsoluteTimeout,
		startedAt:       time.Now(),
	}
}

/* ---------------------------------------------------------------------
   Where the state lives
   --------------------------------------------------------------------- */

// stateDir is where accounts, tokens and the audit trail are written.
//
// The same directory the metrics files use, which under Docker is /data
// because the image sets XDG_CONFIG_HOME. That matters more here than it does
// for metrics: an accounts file in the image layer is one the next deploy
// deletes, reopening first-run setup on an instance that already had an
// administrator.
func stateDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "3270Connect")
	}
	return filepath.Join(dir, "3270Connect")
}

func resolveUsersPath() string {
	if override := strings.TrimSpace(os.Getenv("USERS_PATH")); override != "" {
		return override
	}
	return filepath.Join(stateDir(), "users.json")
}

func resolveTokensPath() string {
	if override := strings.TrimSpace(os.Getenv("API_TOKENS_PATH")); override != "" {
		return override
	}
	return filepath.Join(stateDir(), "api-tokens.json")
}

func resolveAuditPath() string {
	if override := strings.TrimSpace(os.Getenv("AUDIT_LOG_PATH")); override != "" {
		return override
	}
	return filepath.Join(stateDir(), "audit.log")
}

// userStore opens the account database on first use.
func (a *authState) userStore() *users.Store {
	if a.usersStore == nil {
		a.usersStore = users.NewStore(resolveUsersPath())
	}
	return a.usersStore
}

// tokenStore opens the issued-token database on first use.
func (a *authState) tokenStore() *apitoken.Store {
	if a.tokensStore == nil {
		a.tokensStore = apitoken.NewStore(resolveTokensPath())
	}
	return a.tokensStore
}

// recorder opens the audit trail on first use. A nil *Recorder is usable and
// does nothing, so no call site has to check.
func (a *authState) recorder() *audit.Recorder {
	if a.trail == nil {
		a.trail = audit.NewRecorder(resolveAuditPath())
	}
	return a.trail
}

/* ---------------------------------------------------------------------
   Configuration
   --------------------------------------------------------------------- */

// configureAuth validates the settings and prepares the stores.
//
// Called before either listener starts, so a misconfiguration stops startup
// rather than surfacing at somebody's first sign-in. It returns an error
// instead of exiting so the caller decides how loud to be.
func (a *authState) configure() error {
	mode, err := authz.ParseMode(os.Getenv(authz.ModeEnv))
	if err != nil {
		return err
	}
	a.mode = mode

	a.idleTimeout = envDuration("AUTH_SESSION_IDLE", authsession.DefaultIdleTimeout)
	a.absoluteTimeout = envDuration("AUTH_SESSION_MAX", authsession.DefaultAbsoluteTimeout)
	a.bindIP = strings.TrimSpace(os.Getenv("AUTH_BIND_SESSION_IP"))
	switch strings.ToLower(a.bindIP) {
	case "", "auto", "true", "false":
	default:
		return fmt.Errorf("unsupported AUTH_BIND_SESSION_IP %q (supported: auto, true, false)", a.bindIP)
	}

	if a.limiter == nil {
		a.limiter = newLoginLimiter()
	}
	if a.runs == nil {
		a.runs = newRunOwners()
	}
	if a.sessions == nil {
		a.sessions = authsession.NewStore(a.idleTimeout, a.absoluteTimeout)
	}
	a.sharedToken = strings.TrimSpace(os.Getenv("API_TOKEN"))

	if a.mode == authz.ModeNone {
		return nil
	}

	// A single shared token would undo the mode that was just asked for: one
	// credential, held by every automated client, able to start and stop
	// everybody's runs. Starting anyway would leave an operator believing
	// users were separated while one environment variable said otherwise.
	if a.sharedToken != "" {
		return fmt.Errorf("API_TOKEN cannot be used with %s=%s: a single shared token would reach every "+
			"account's runs. Unset it and issue a token per account with `3270Connect token add <username> <name>`",
			authz.ModeEnv, a.mode)
	}

	if err := a.configureSSO(); err != nil {
		return err
	}

	// From here on the instance requires accounts, so arm first-run setup
	// rather than presenting a sign-in nobody can pass.
	return a.beginSetupIfNeeded()
}

// separatesUsers reports whether this deployment has more than one identity.
//
// Asked through one predicate so ownership, token authentication and the
// request gate cannot disagree about what a mode means.
func (a *authState) separatesUsers() bool {
	return a.mode == authz.ModeLocal || a.mode == authz.ModeOIDC
}

// envDuration reads a duration setting, falling back when unset or unusable.
func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("Warning: ignoring %s=%q: %v", name, raw, err)
		return fallback
	}
	return d
}

// envTruthy reads the spellings of "yes" an environment variable turns up
// with.
func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// splitList reads a comma-separated setting.
func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

/* ---------------------------------------------------------------------
   The principal on a request
   --------------------------------------------------------------------- */

// requestIdentity is everything the gate worked out about a caller: the
// principal used for every decision, plus the two facts that travel beside it
// and are not part of the identity.
type requestIdentity struct {
	Principal authz.Principal
	Username  string
	// MustChangePassword pins a login to the password-change page. It is a
	// property of the login rather than of the account, mirrored here at
	// sign-in so no request has to re-read the account store to learn it.
	MustChangePassword bool
}

type identityContextKey struct{}

// withIdentity attaches the resolved identity to a request.
func withIdentity(r *http.Request, id requestIdentity) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), identityContextKey{}, id))
}

// identityOf returns what the gate resolved for this request.
//
// A request that never passed through the gate yields the anonymous
// principal, which owns nothing and is not an administrator. A routing mistake
// therefore produces a refusal rather than an escalation.
func identityOf(r *http.Request) requestIdentity {
	if r == nil {
		return requestIdentity{}
	}
	id, _ := r.Context().Value(identityContextKey{}).(requestIdentity)
	return id
}

func principalOf(r *http.Request) authz.Principal { return identityOf(r).Principal }
func usernameOf(r *http.Request) string           { return identityOf(r).Username }

// bindSessionIP reports whether a login is pinned to the address that created
// it.
//
// Defaulting to on for plain HTTP is the point: there the cookie travels in
// the clear and can be copied off the wire, so requiring the replay to come
// from the same address is most of what stands between a passive eavesdropper
// and a usable session. Behind TLS the cookie is not visible in the first
// place, and pinning mostly punishes people whose address changes.
func (a *authState) bindSessionIP(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(a.bindIP)) {
	case "true":
		return true
	case "false":
		return false
	default: // "auto" or unset
		return !reqsec.IsTLS(r)
	}
}

// resolve works out who is making this request.
//
// Order matters. A bearer token is checked first because a client that
// presented one is telling us which credential it means to be judged on, and
// silently falling back to a cookie — or to the single local operator — would
// answer a caller whose token was refused as though it had never been sent.
func (a *authState) resolve(r *http.Request) (requestIdentity, error) {
	if presented, ok := bearerToken(r); ok {
		principal, err := a.authenticateToken(presented)
		if err != nil {
			return requestIdentity{}, err
		}
		return requestIdentity{Principal: principal, Username: a.displayNameFor(principal)}, nil
	}

	if !a.separatesUsers() {
		return requestIdentity{Principal: authz.Local(), Username: "local operator"}, nil
	}

	id := cookieValue(r, authCookieName)
	// A store that was never built cannot hold a login, so there is nobody to
	// be. Anonymous rather than a panic: an authState assembled by hand — in a
	// test, or by a future caller that skips configure — should deny, not
	// crash.
	if id == "" || a.sessions == nil {
		return requestIdentity{}, nil
	}
	sess, ok := a.sessions.Get(id, reqsec.ClientIP(r), a.bindSessionIP(r))
	if !ok {
		return requestIdentity{}, nil
	}
	return requestIdentity{
		Principal:          sess.Principal(),
		Username:           sess.Username,
		MustChangePassword: sess.MustChangePassword,
	}, nil
}

// displayNameFor names a token's owner for a log or an audit line. A token
// carries no username, so it is looked up — once, on a request that is not the
// busy path — rather than left blank for a reader to guess at.
func (a *authState) displayNameFor(p authz.Principal) string {
	if p.UserID == authz.LocalUserID {
		return "local operator"
	}
	if !a.separatesUsers() || p.UserID == "" {
		return ""
	}
	user, found, err := a.userStore().ByID(p.UserID)
	if err != nil || !found {
		return ""
	}
	return user.Username
}

func cookieValue(r *http.Request, name string) string {
	if r == nil {
		return ""
	}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// setAuthCookie writes, or with an empty value clears, the login cookie.
func (a *authState) setAuthCookie(w http.ResponseWriter, r *http.Request, value string) {
	maxAge := int(a.absoluteTimeout.Seconds())
	if value == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   reqsec.IsTLS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

/* ---------------------------------------------------------------------
   The gate
   --------------------------------------------------------------------- */

// Gate wraps the console's mux with authentication and authorization.
//
// One wrapper rather than per-route middleware, because the console registers
// its handlers on the default mux from a dozen places and a route that forgot
// to opt in would be a route with no gate on it. Here the default is "needs a
// login" and the exceptions are named.
func (a *authState) Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)

		path := r.URL.Path

		identity, err := a.resolve(r)
		if err != nil {
			// Only a presented-and-refused credential produces an error here.
			a.recorder().Log(audit.Entry{
				Event:    audit.EventTokenRefused,
				Outcome:  audit.Denied,
				ClientIP: reqsec.ClientIP(r),
				Detail:   map[string]string{"path": path},
			})
			writeAuthError(w, r, err)
			return
		}
		r = withIdentity(r, identity)

		// Static assets before every other gate, so the sign-in and setup
		// pages are not served unstyled.
		if isStaticAssetPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		if a.gateSetup(w, r) {
			return
		}
		if !a.checkCSRF(w, r) {
			return
		}
		if !a.requireLogin(w, r) {
			return
		}
		if !a.requireScope(w, r) {
			return
		}
		if !a.requireAdminPath(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders are set on everything the console serves.
//
// No content-security policy here, deliberately. The console's own page is one
// large document with inline script and an optional web font from a CDN, and a
// policy strict enough to be worth setting would break it. The sign-in and
// administration pages were written for one and set their own, which is where
// a policy actually buys something — see renderAuthPage.
func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
}

func isStaticAssetPath(path string) bool {
	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func isPublicPath(path string) bool {
	return publicPaths[path] || isStaticAssetPath(path)
}

// requireLogin refuses a request that carries no identity.
//
// It reports whether the request may continue. With AUTH_MODE=none the
// principal is never anonymous, so this admits everything and the default
// deployment is unaffected.
func (a *authState) requireLogin(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if !a.separatesUsers() || isPublicPath(path) {
		return true
	}

	identity := identityOf(r)
	if identity.Principal.IsAnonymous() {
		a.rejectUnauthenticated(w, r)
		return false
	}

	// An account whose password was issued by somebody else may do exactly one
	// thing until it is changed. Letting it roam would leave a credential an
	// administrator typed in circulation indefinitely.
	//
	// Only a browser login is pinned: a token is not something its owner types
	// a password to use, and refusing every automated client because a person
	// has not visited a form yet would be a strange way to enforce it.
	if identity.MustChangePassword && path != changePasswordPath && path != logoutPath {
		if wantsJSON(r) {
			writeJSONError(w, http.StatusForbidden, "password change required")
			return false
		}
		http.Redirect(w, r, changePasswordPath, http.StatusFound)
		return false
	}
	return true
}

// requireScope applies an API token's scopes.
//
// Scope is decided by method rather than by route: a read-only credential is
// one that cannot change anything, and "does this request change anything" is
// exactly what the method says. Deriving it from a list of routes would mean
// every endpoint added later defaulted to whatever somebody remembered.
//
// A principal with no scopes — a browser, the single local operator — is
// unrestricted within its role, so this is a no-op for them.
func (a *authState) requireScope(w http.ResponseWriter, r *http.Request) bool {
	principal := principalOf(r)
	if principal.Kind != authz.KindAPIToken {
		return true
	}
	if tokenScopeAllows(r.Method, principal) {
		return true
	}
	writeJSONError(w, http.StatusForbidden, fmt.Sprintf(
		"this token has scope %s; %s needs the %q scope",
		strings.Join(principal.Scopes, "+"), r.Method, apitoken.ScopeWrite))
	return false
}

// requireAdminPath gates the administration area.
//
// Under AUTH_MODE=none the single operator is an administrator, so this
// changes nothing for the default deployment.
func (a *authState) requireAdminPath(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != adminPathPrefix && !strings.HasPrefix(path, adminPathPrefix+"/") {
		return true
	}
	return a.requireAdmin(w, r)
}

// requireAdmin refuses a caller who may not change instance-wide state.
func (a *authState) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	principal := principalOf(r)
	if principal.IsAnonymous() {
		a.rejectUnauthenticated(w, r)
		return false
	}
	if !principal.IsAdmin() {
		if wantsJSON(r) {
			writeJSONError(w, http.StatusForbidden, "this action requires an administrator account")
			return false
		}
		renderAuthPage(w, r, http.StatusForbidden, "denied.gohtml", authPageData{
			Title:   "Not for you",
			Message: "This page requires an administrator account.",
		})
		return false
	}
	return true
}

// rejectUnauthenticated answers in the shape the caller expects: a redirect
// for a browser, JSON for anything scripted.
func (a *authState) rejectUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if wantsJSON(r) {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	target := loginPath
	if next := returnPathFor(r); next != defaultLandingPath {
		target += "?next=" + urlQueryEscape(next)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// returnPathFor is where to send somebody back to after they sign in. Only a
// GET is worth remembering: replaying a POST after a sign-in would resubmit
// whatever it carried, which for this console means starting a load run
// somebody did not ask for a second time.
func returnPathFor(r *http.Request) string {
	if r == nil || r.Method != http.MethodGet {
		return defaultLandingPath
	}
	return safeReturnPath(r.URL.RequestURI())
}

// wantsJSON reports whether the caller is a script rather than a browser
// following links. A redirect to an HTML sign-in page is useless to fetch()
// and actively confusing in an API client.
func wantsJSON(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/admin/api/") {
		return true
	}
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	if _, ok := bearerToken(r); ok {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, "{%q:%q}\n", "error", message)
}

// writeAuthError answers a credential that was presented and refused.
func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var answer apiAuthError
	if !errors.As(err, &answer) {
		answer = errBadToken
	}
	if wantsJSON(r) {
		writeJSONError(w, answer.status, answer.message)
		return
	}
	http.Error(w, answer.message, answer.status)
}

/* ---------------------------------------------------------------------
   Cross-site request forgery
   --------------------------------------------------------------------- */

// checkCSRF refuses an unsafe request that a browser made from another site.
//
// The attack is worth closing in both deployment shapes, for different
// reasons. With a login cookie, a page somebody visits could start a load run
// — or stop one — in their name. With no sign-in at all the console is open to
// whoever can reach the port, but "whoever can reach the port" is usually
// localhost, and a drive-by page in the operator's own browser is precisely
// how something that cannot reach localhost gets to.
//
// What it must not do is refuse a script. The console's POST endpoints predate
// any of this and are called by curl, by CI and by the installer's own checks;
// none of them sends an Origin, and treating "no Origin" as hostile would
// break every one of them for a threat they are not part of. So the test is
// specifically "did a browser send this from somewhere else", asked in the
// order the evidence is trustworthy:
//
//   - Sec-Fetch-Site is set by the browser itself and cannot be forged by the
//     page making the request, so where it is present it is the answer.
//   - Origin and Referer are the older signals, and are checked against the
//     address the request was actually addressed to.
//   - A request carrying none of the three did not come from a browser, and is
//     allowed. Any browser able to make a cross-site POST sends at least one.
//
// A bearer-token caller is exempt outright: it carries no cookie, so there is
// nothing for another site to ride on.
func (a *authState) checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	if principalOf(r).Kind == authz.KindAPIToken {
		return true
	}

	refuse := func(format string, args ...any) bool {
		log.Printf("auth: refusing %s %s: "+format, append([]any{r.Method, r.URL.Path}, args...)...)
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return false
	}

	// "none" is a request the person typed or bookmarked, with no initiating
	// page; "same-origin" is this console's own page. Everything else — a
	// cross-site form post, a sibling subdomain, an embedded frame — is what
	// this exists to stop.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		if site != "same-origin" && site != "none" {
			return refuse("Sec-Fetch-Site is %q", site)
		}
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		if !originMatchesHost(origin, r.Host) {
			return refuse("Origin %q is not %q", origin, r.Host)
		}
		return true
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		if !originMatchesHost(referer, r.Host) {
			return refuse("Referer %q is not %q", referer, r.Host)
		}
		return true
	}
	return true
}

// originMatchesHost reports whether a URL's host is the one this request was
// addressed to.
func originMatchesHost(raw, host string) bool {
	u, err := parseRequestURL(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

/* ---------------------------------------------------------------------
   Ownership of a load run
   --------------------------------------------------------------------- */

// runOwner records who started a run and when.
type runOwner struct {
	UserID    string
	Username  string
	StartedAt time.Time
}

// runOwners maps a load run's process id to the account that started it.
//
// In memory, and deliberately not persisted. A run is a child of this process
// and does not outlive a restart in any useful way: the metrics file is left
// behind but the process is gone, so there is nothing left to own. Writing
// ownership to disk would buy a record that is only ever read about processes
// that no longer exist.
//
// A run this console did not start — one launched from a command line on the
// same machine — has no owner here, and is therefore an administrator's to
// stop. That is the fail-closed reading: unowned is not "belongs to whoever
// asked".
type runOwners struct {
	mu    sync.Mutex
	byPID map[int]runOwner
}

func newRunOwners() *runOwners { return &runOwners{byPID: make(map[int]runOwner)} }

// claim records the account a run belongs to.
func (o *runOwners) claim(pid int, userID, username string) {
	if o == nil || pid <= 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.byPID[pid] = runOwner{UserID: userID, Username: username, StartedAt: time.Now()}
}

// owner returns who started a run, if this console started it.
func (o *runOwners) owner(pid int) (runOwner, bool) {
	if o == nil {
		return runOwner{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	owner, ok := o.byPID[pid]
	return owner, ok
}

// prune forgets runs whose process has gone and which are old enough that
// nobody is still looking at the row.
//
// Not on exit: a finished run stays on the console for a while and an
// administrator reading it should still see whose it was.
func (o *runOwners) prune(alive func(int) bool, olderThan time.Duration, now time.Time) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for pid, owner := range o.byPID {
		if now.Sub(owner.StartedAt) > olderThan && !alive(pid) {
			delete(o.byPID, pid)
		}
	}
}

// ownerNamesFor maps the process ids in a metrics list to the accounts that
// started them, for the console to label rows with.
//
// Empty where there is one operator: there is nobody to tell apart, and a
// column saying "local operator" against every row is noise. String keys
// because this becomes JSON, where an object's keys are strings whatever they
// started as.
func (a *authState) ownerNamesFor(list []ExtendedMetrics) map[string]string {
	if !a.separatesUsers() || len(list) == 0 {
		return nil
	}
	out := make(map[string]string, len(list))
	for _, m := range list {
		if owner, ok := a.runs.owner(m.PID); ok && owner.Username != "" {
			out[strconv.Itoa(m.PID)] = owner.Username
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mayStopRun reports whether this caller may stop the run with this pid, and
// says why not when they may not.
//
// An administrator may stop anything: reclaiming capacity from a run somebody
// left pointed at a production region is the reason the role exists here. The
// owner may stop their own. Everybody else is refused — including for a run
// with no recorded owner, which is what a run started outside this console
// looks like.
func (a *authState) mayStopRun(r *http.Request, pid int) (bool, string) {
	principal := principalOf(r)
	if principal.IsAdmin() {
		return true, ""
	}
	owner, known := a.runs.owner(pid)
	if !known {
		return false, "this run was not started from the console; an administrator can stop it"
	}
	if principal.Owns(owner.UserID) {
		return true, ""
	}
	return false, "this run belongs to somebody else; an administrator can stop it"
}

/* ---------------------------------------------------------------------
   The audit trail
   --------------------------------------------------------------------- */

// auditActor describes the caller of a request.
//
// The username comes from the login where there is one, so a line reads as a
// person rather than an identifier.
func auditActor(r *http.Request) audit.Actor {
	identity := identityOf(r)
	return audit.Actor{
		UserID:   identity.Principal.UserID,
		Username: identity.Username,
		Role:     string(identity.Principal.Role),
		Kind:     string(identity.Principal.Kind),
	}
}

// auditRequest records an event attributed to whoever made the request.
func (a *authState) auditRequest(r *http.Request, event audit.Event, outcome audit.Outcome, target string, detail map[string]string) {
	entry := audit.Entry{
		Event:   event,
		Outcome: outcome,
		Actor:   auditActor(r),
		Target:  target,
		Detail:  detail,
	}
	if r != nil {
		entry.ClientIP = reqsec.ClientIP(r)
	}
	a.recorder().Log(entry)
}

/* ---------------------------------------------------------------------
   Roles that come from a group
   --------------------------------------------------------------------- */

// groupRoles reads the group-to-role assignments, treating a store error as
// "none".
//
// The callers are authentication paths, where failing closed means the
// account's own role — never more — so degrading is safe.
func (a *authState) groupRoles() map[string]authz.Role {
	if !a.separatesUsers() {
		return nil
	}
	roles, err := a.userStore().GroupRoles()
	if err != nil {
		log.Printf("auth: could not read group roles: %v", err)
		return nil
	}
	return roles
}

// effectiveRoleFor is the role this account actually signs in with: its own,
// or one a group it belongs to grants, whichever is stronger.
func (a *authState) effectiveRoleFor(u users.User) authz.Role {
	return users.EffectiveRole(u, a.groupRoles())
}

// pushEffectiveRoles recomputes every account's effective role and writes it
// into the logins they already hold.
//
// Called after anything that can change an inherited role — the assignment
// itself, or a group's membership — because a promotion or demotion that waits
// for the next sign-in is not the change the administrator watched themselves
// make.
func (a *authState) pushEffectiveRoles() {
	if !a.separatesUsers() || a.sessions == nil {
		return
	}
	list, err := a.userStore().List()
	if err != nil {
		log.Printf("auth: could not read accounts to refresh roles: %v", err)
		return
	}
	roles := a.groupRoles()
	for _, u := range list {
		a.sessions.SetRoleFor(u.ID, users.EffectiveRole(u, roles))
	}
}

// selfAdminDependsOnGroup reports whether the caller's own administrator role
// comes from this group and nothing else.
//
// Used to refuse the changes that are self-demotion wearing different clothes:
// clearing the group's role, leaving the group, or deleting it.
func (a *authState) selfAdminDependsOnGroup(r *http.Request, group string) bool {
	// With a single operator there are no accounts to look up and no group
	// that could be the source of anybody's role. Asked first, because the
	// lookup below fails closed and would otherwise refuse a change on an
	// instance that has no groups at all.
	if !a.separatesUsers() {
		return false
	}
	principal := principalOf(r)
	if principal.UserID == "" || strings.TrimSpace(group) == "" {
		return false
	}
	user, found, err := a.userStore().ByID(principal.UserID)
	if err != nil || !found {
		// Cannot tell, so assume it does. Refusing a change an administrator
		// could legitimately make is recoverable; letting them strand
		// themselves is not.
		return true
	}
	if user.Role == authz.RoleAdmin {
		return false // their own role stands on its own.
	}
	if !user.InGroup(group) {
		return false
	}
	roles := a.groupRoles()
	if users.EffectiveRole(user, roles) != authz.RoleAdmin {
		return false
	}
	without := make(map[string]authz.Role, len(roles))
	for name, role := range roles {
		if !strings.EqualFold(name, group) {
			without[name] = role
		}
	}
	return users.EffectiveRole(user, without) != authz.RoleAdmin
}

/* ---------------------------------------------------------------------
   Periodic housekeeping
   --------------------------------------------------------------------- */

// sweep is the periodic tidy-up: expired logins dropped, logins belonging to
// accounts that have since been disabled or deleted ended, and the run-owner
// map pruned.
//
// The account re-check is what makes `3270Connect user disable` reach a
// browser that is already signed in. The console command edits the file
// directly and cannot reach into this process's memory, so without a sweep a
// disabled account would keep its session until it expired.
func (a *authState) sweep() {
	a.runs.prune(isProcessRunning, time.Hour, time.Now())

	if a.sessions == nil || !a.separatesUsers() {
		return
	}
	if n := a.sessions.Reap(); n > 0 {
		log.Printf("auth: dropped %d expired login(s)", n)
	}
	for _, id := range a.sessions.UserIDs() {
		user, found, err := a.userStore().ByID(id)
		if err != nil {
			continue
		}
		if found && !user.Disabled {
			// Roles change on disk too, through the console command.
			a.sessions.SetRoleFor(id, a.effectiveRoleFor(user))
			continue
		}
		if n := a.sessions.DeleteAllFor(id); n > 0 {
			log.Printf("auth: ended %d login(s) for an account that is gone or disabled", n)
		}
	}
}

// sweepInterval bounds how long a disabled account keeps a browser it is
// already signed in on. Disabling from the Accounts page ends those logins on
// the spot; this is the backstop for the console command, which has no way to
// reach this process.
const sweepInterval = 5 * time.Minute

// startAuthHousekeeping runs the sweep for as long as the console is up.
func (a *authState) startAuthHousekeeping() {
	go func() {
		for {
			time.Sleep(sweepInterval)
			a.sweep()
		}
	}()
}
