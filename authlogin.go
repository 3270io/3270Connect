package main

// Signing in, signing out, and changing your own password.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/3270io/3270Connect/internal/audit"
	"github.com/3270io/3270Connect/internal/authz"
	"github.com/3270io/3270Connect/internal/reqsec"
	"github.com/3270io/3270Connect/internal/users"
)

// registerAuthHandlers puts the sign-in routes on the console's mux.
//
// Registered whatever the mode: with AUTH_MODE=none they redirect to the
// console rather than 404, so a bookmark saved on an instance that had
// accounts does something sensible on one that does not.
func (a *authState) registerAuthHandlers(mux *http.ServeMux) {
	mux.HandleFunc(loginPath, a.loginHandler)
	mux.HandleFunc(logoutPath, a.logoutHandler)
	mux.HandleFunc(changePasswordPath, a.changePasswordHandler)
	mux.HandleFunc(whoamiPath, a.whoamiHandler)
	mux.HandleFunc(setupPath, a.setupHandler)
	mux.HandleFunc(ssoStartPath, a.ssoStartHandler)
	mux.HandleFunc(ssoCallbackPath, a.ssoCallbackHandler)
}

/* ---------------------------------------------------------------------
   Signing in
   --------------------------------------------------------------------- */

func (a *authState) loginHandler(w http.ResponseWriter, r *http.Request) {
	if !a.separatesUsers() {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !principalOf(r).IsAnonymous() {
			http.Redirect(w, r, safeReturnPath(r.URL.Query().Get("next")), http.StatusFound)
			return
		}
		a.renderLogin(w, r, http.StatusOK, "")
	case http.MethodPost:
		a.doLogin(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *authState) renderLogin(w http.ResponseWriter, r *http.Request, status int, message string) {
	renderAuthPage(w, r, status, "login.gohtml", authPageData{
		Title:          "Sign in",
		Error:          message,
		ShowNoTLS:      !reqsec.IsTLS(r),
		ProxySaysHTTPS: proxyClaimsHTTPS(r),
		SSOEnabled:     a.ssoEnabled(),
		SSOStartPath:   ssoStartPath,
		Next:           safeReturnPath(r.URL.Query().Get("next")),
	})
}

// doLogin verifies credentials and starts a login.
func (a *authState) doLogin(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	clientIP := reqsec.ClientIP(r)
	keys := loginLimitKeys(username, clientIP)

	if ok, retryIn := a.limiter.Allow(keys...); !ok {
		log.Printf("auth: login throttled for %q from %s (%s remaining)",
			username, clientIP, retryIn.Round(time.Second))
		// Recorded separately from a plain failure: a run of these is the
		// shape of somebody working through a password list, and it reads
		// differently from one person mistyping.
		a.recorder().Log(audit.Entry{
			Event:    audit.EventLoginLockedOut,
			Outcome:  audit.Denied,
			ClientIP: clientIP,
			Target:   username,
			Detail:   map[string]string{"retryIn": retryIn.Round(time.Second).String()},
		})
		a.failLogin(w, r, "Too many failed attempts. Try again shortly.", http.StatusTooManyRequests)
		return
	}

	user, err := a.userStore().Authenticate(username, password)
	if err != nil {
		a.limiter.RecordFailure(keys...)
		switch {
		case errors.Is(err, users.ErrUserDisabled):
			log.Printf("auth: login refused for disabled account %q from %s", username, clientIP)
		case errors.Is(err, users.ErrInvalidCredentials):
			log.Printf("auth: failed login for %q from %s", username, clientIP)
		default:
			log.Printf("auth: login error for %q from %s: %v", username, clientIP, err)
		}
		// The trail may say why, even though the reply must not: it is read by
		// an administrator who is entitled to know the account was disabled
		// rather than the password wrong.
		a.recorder().Log(audit.Entry{
			Event:    audit.EventLoginFailed,
			Outcome:  audit.Failure,
			ClientIP: clientIP,
			Target:   username,
			Detail:   map[string]string{"reason": loginFailureReason(err)},
		})
		// One message for every failure. Distinguishing them would report
		// which usernames exist and which accounts are disabled.
		a.failLogin(w, r, "Incorrect username or password.", http.StatusUnauthorized)
		return
	}

	// Rotate: any pre-existing cookie value is discarded rather than adopted,
	// so a value planted before the login cannot become an authenticated one.
	if existing := cookieValue(r, authCookieName); existing != "" {
		a.sessions.Delete(existing)
	}

	// The login carries the effective role — the account's own, or one a group
	// grants — because the session is what the role is read from on every
	// request that follows.
	sess, err := a.sessions.Create(user.ID, user.Username, a.effectiveRoleFor(user), clientIP, user.MustChangePassword)
	if err != nil {
		log.Printf("auth: could not create a session for %q: %v", username, err)
		a.failLogin(w, r, "Could not start a session. Try again.", http.StatusInternalServerError)
		return
	}

	a.limiter.Reset(keys...)
	a.setAuthCookie(w, r, sess.ID)
	log.Printf("auth: %s signed in from %s", user.Username, clientIP)
	a.recorder().Log(audit.Entry{
		Event:    audit.EventLoginSucceeded,
		Actor:    audit.Actor{UserID: user.ID, Username: user.Username, Role: string(user.Role), Kind: string(authz.KindWeb)},
		ClientIP: clientIP,
	})

	if user.MustChangePassword {
		http.Redirect(w, r, changePasswordPath, http.StatusFound)
		return
	}
	http.Redirect(w, r, safeReturnPath(r.FormValue("next")), http.StatusFound)
}

func (a *authState) failLogin(w http.ResponseWriter, r *http.Request, message string, status int) {
	if wantsJSON(r) {
		writeJSONError(w, status, message)
		return
	}
	a.renderLogin(w, r, status, message)
}

// loginFailureReason names why a sign-in was refused, for the audit trail
// only. The reply to the browser stays the same either way; an administrator
// reading the trail is entitled to the distinction the caller is not.
func loginFailureReason(err error) string {
	switch {
	case errors.Is(err, users.ErrUserDisabled):
		return "account disabled"
	case errors.Is(err, users.ErrInvalidCredentials):
		return "bad credentials"
	default:
		return "store error"
	}
}

/* ---------------------------------------------------------------------
   Signing out
   --------------------------------------------------------------------- */

func (a *authState) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if id := cookieValue(r, authCookieName); id != "" {
		// Before the delete, while the principal is still resolvable.
		a.auditRequest(r, audit.EventLogout, audit.Success, "", nil)
		a.sessions.Delete(id)
	}
	a.setAuthCookie(w, r, "")
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{"loggedOut": true})
		return
	}
	// Where the deployment asked for it, signing out here also ends the
	// session at the provider — otherwise the next visit is signed straight
	// back in without being asked for anything, which on a shared machine is
	// not what anybody means by "sign out".
	if target := a.ssoLogoutURL(r); target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	http.Redirect(w, r, loginPath, http.StatusFound)
}

/* ---------------------------------------------------------------------
   Changing your own password
   --------------------------------------------------------------------- */

func (a *authState) changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	if !a.separatesUsers() {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	if principalOf(r).IsAnonymous() {
		a.rejectUnauthenticated(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.renderChangePassword(w, r, http.StatusOK, "")
	case http.MethodPost:
		a.doChangePassword(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *authState) renderChangePassword(w http.ResponseWriter, r *http.Request, status int, message string) {
	renderAuthPage(w, r, status, "change-password.gohtml", authPageData{
		Title:     "Change your password",
		Error:     message,
		MinLength: users.MinPasswordLength,
		Forced:    identityOf(r).MustChangePassword,
	})
}

// doChangePassword updates the caller's own password.
//
// Changing a password ends every other login for the account. A password
// change that left old sessions working would not actually revoke anything,
// which is the main reason people change one.
func (a *authState) doChangePassword(w http.ResponseWriter, r *http.Request) {
	principal := principalOf(r)
	current := r.FormValue("currentPassword")
	next := r.FormValue("newPassword")
	confirm := r.FormValue("confirmPassword")

	user, ok, err := a.userStore().ByID(principal.UserID)
	if err != nil || !ok {
		a.failChangePassword(w, r, "Could not load your account.", http.StatusInternalServerError)
		return
	}

	// Re-authenticate even though the caller is signed in: this is the check
	// that stops a borrowed session from locking its owner out of the account.
	if _, err := a.userStore().Authenticate(user.Username, current); err != nil {
		a.failChangePassword(w, r, "Your current password is incorrect.", http.StatusUnauthorized)
		return
	}
	if next != confirm {
		a.failChangePassword(w, r, "The new passwords do not match.", http.StatusBadRequest)
		return
	}
	if err := users.ValidatePassword(next); err != nil {
		a.failChangePassword(w, r, humanStoreError(err), http.StatusBadRequest)
		return
	}
	if next == current {
		a.failChangePassword(w, r, "The new password must be different.", http.StatusBadRequest)
		return
	}

	if err := a.userStore().SetPassword(user.Username, next); err != nil {
		a.failChangePassword(w, r, humanStoreError(err), http.StatusBadRequest)
		return
	}

	a.sessions.DeleteAllFor(user.ID)
	log.Printf("auth: %s changed their password; other sessions ended", user.Username)
	a.auditRequest(r, audit.EventPasswordChanged, audit.Success, user.Username,
		map[string]string{"otherSessions": "ended"})

	// Issue a fresh login so the person who just changed it stays signed in.
	sess, err := a.sessions.Create(user.ID, user.Username, a.effectiveRoleFor(user), reqsec.ClientIP(r), false)
	if err != nil {
		a.setAuthCookie(w, r, "")
		http.Redirect(w, r, loginPath, http.StatusFound)
		return
	}
	a.setAuthCookie(w, r, sess.ID)

	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{"changed": true})
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (a *authState) failChangePassword(w http.ResponseWriter, r *http.Request, message string, status int) {
	if wantsJSON(r) {
		writeJSONError(w, status, message)
		return
	}
	a.renderChangePassword(w, r, status, message)
}

/* ---------------------------------------------------------------------
   Who am I
   --------------------------------------------------------------------- */

// whoamiHandler reports the current login, for the console to show who is
// signed in and whether administrative controls belong on the page.
func (a *authState) whoamiHandler(w http.ResponseWriter, r *http.Request) {
	view := a.consoleAuthView(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": view.SignedIn,
		"authMode":      string(a.mode),
		"userId":        principalOf(r).UserID,
		"username":      view.Username,
		"role":          string(principalOf(r).Role),
		"isAdmin":       principalOf(r).IsAdmin(),
		"canAdminister": view.CanAdminister,
	})
}

// consoleAuthView is the small bundle of identity the console's own pages
// need. Kept as one value so a template never has to reason about which of
// several flags to consult.
type consoleAuthView struct {
	// Enabled is false under AUTH_MODE=none, where there is nobody to show.
	Enabled  bool
	SignedIn bool
	Username string
	// CanAdminister is what a template should ask before rendering a control
	// only an administrator may use.
	//
	// It is not IsAdmin. Under AUTH_MODE=none there is no principal to be an
	// administrator, but the single operator may do everything, which is
	// exactly what requireAdmin allows. A template gating on IsAdmin would
	// hide the administration area from the one person the default deployment
	// is for.
	CanAdminister bool
}

func (a *authState) consoleAuthView(r *http.Request) consoleAuthView {
	if !a.separatesUsers() {
		return consoleAuthView{CanAdminister: true}
	}
	principal := principalOf(r)
	if principal.IsAnonymous() {
		return consoleAuthView{Enabled: true}
	}
	return consoleAuthView{
		Enabled:       true,
		SignedIn:      true,
		Username:      usernameOf(r),
		CanAdminister: principal.IsAdmin(),
	}
}

/* ---------------------------------------------------------------------
   Small shared helpers
   --------------------------------------------------------------------- */

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("auth: could not write a JSON response: %v", err)
	}
}

// humanStoreError strips the package prefix from a store error so a form does
// not show "users: " to somebody choosing a password.
func humanStoreError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "users: ") {
		msg = strings.TrimPrefix(msg, "users: ")
	} else if strings.HasPrefix(msg, "apitoken: ") {
		msg = strings.TrimPrefix(msg, "apitoken: ")
	}
	if msg == "" {
		return "That cannot be used."
	}
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
}

// proxyClaimsHTTPS reports that something in front of this server said the
// browser reached it over HTTPS, and that this server has not been told to
// believe it.
//
// It changes nothing about how the request is treated — IsTLS has already had
// its say, and the header is chosen by whoever sent the request. It only
// changes what the sign-in page says. "This connection is not encrypted" is
// alarming, correct, and useless to somebody whose ingress is doing exactly
// what they configured it to do; what they need is the name of the setting
// that closes the gap.
func proxyClaimsHTTPS(r *http.Request) bool {
	if r == nil || reqsec.IsTLS(r) {
		return false
	}
	return reqsec.ForwardedProtoClaimsHTTPS(r)
}

// safeReturnPath keeps a "next" parameter pointing at this server.
//
// Only a rooted path, never a URL and never a protocol-relative "//host" —
// otherwise the sign-in page is an open redirect, and one that hands somebody
// a landing page immediately after they typed a password is the most
// convincing place to have one.
func safeReturnPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.Contains(raw, "\\") {
		return defaultLandingPath
	}
	return raw
}

// defaultLandingPath is where somebody ends up when there is nowhere in
// particular to send them. The console itself, not "/", which only redirects
// here anyway.
const defaultLandingPath = "/dashboard"

func parseRequestURL(raw string) (*url.URL, error) { return url.Parse(raw) }

func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// absoluteURL renders a path on this server as a full URL, which is what a
// provider's post-logout redirect has to be given.
func absoluteURL(r *http.Request, path string) string {
	if r == nil || r.Host == "" {
		return ""
	}
	scheme := "http"
	if reqsec.IsTLS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}
