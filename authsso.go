package main

// Signing in through an OpenID Connect identity provider.
//
// The provider says who somebody is. What they may do here is decided by
// internal/authz from an account this console keeps, so a misconfigured
// provider cannot grant a role nobody mapped to it.

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/3270io/3270Connect/internal/audit"
	"github.com/3270io/3270Connect/internal/authz"
	"github.com/3270io/3270Connect/internal/oidc"
	"github.com/3270io/3270Connect/internal/reqsec"
)

// pendingLoginTTL is how long a sign-in may take.
//
// Long enough for a password, a second factor and a consent screen; short
// enough that a state value captured from a browser's history is no longer
// redeemable by the time anybody reads it.
const pendingLoginTTL = 10 * time.Minute

// maxPendingLogins bounds what an unauthenticated caller can make this server
// remember. /auth/sso is reachable without credentials by design, so without a
// ceiling it is a way to fill memory one redirect at a time.
const maxPendingLogins = 512

// ssoSettings is the deployment's identity-provider configuration.
type ssoSettings struct {
	// UsernameClaim names the claim to take a display name from. Empty means
	// try the usual ones in turn.
	UsernameClaim string
	// GroupsClaim names the claim carrying group membership.
	GroupsClaim string
	// AdminGroups grants the admin role. Empty means the provider says nothing
	// about roles, and an administrator sets them here instead.
	AdminGroups []string
	// AllowedGroups restricts who may sign in at all. Empty means anybody the
	// provider authenticates.
	AllowedGroups []string
	// EndSessionOnLogout also ends the session at the provider.
	EndSessionOnLogout bool
}

// pendingLogin is one sign-in in flight.
//
// The nonce and the PKCE verifier are held here rather than in a cookie: a
// cookie is something the browser carries, and both of these are values the
// browser must never be able to choose. state is the map key, so a callback
// that names one this server did not issue finds nothing.
type pendingLogin struct {
	Nonce    string
	Verifier string
	// ReturnTo is where to land afterwards. Always a path on this server.
	ReturnTo  string
	ClientIP  string
	CreatedAt time.Time
}

// pendingLoginStore holds sign-ins between the redirect out and the callback
// back.
type pendingLoginStore struct {
	mu      sync.Mutex
	pending map[string]pendingLogin
	now     func() time.Time
}

func newPendingLoginStore() *pendingLoginStore {
	return &pendingLoginStore{pending: make(map[string]pendingLogin), now: time.Now}
}

// begin records a sign-in, dropping stale ones first.
func (s *pendingLoginStore) begin(state string, p pendingLogin) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if len(s.pending) >= maxPendingLogins {
		return false
	}
	s.pending[state] = p
	return true
}

// take returns a sign-in and forgets it.
//
// One use only. A state that could be redeemed twice would let a captured
// callback URL be replayed into a second login.
func (s *pendingLoginStore) take(state string) (pendingLogin, bool) {
	if state == "" {
		return pendingLogin{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	p, ok := s.pending[state]
	delete(s.pending, state)
	return p, ok
}

func (s *pendingLoginStore) expireLocked() {
	cutoff := s.now().Add(-pendingLoginTTL)
	for state, p := range s.pending {
		if p.CreatedAt.Before(cutoff) {
			delete(s.pending, state)
		}
	}
}

// ssoEnabled reports whether this instance can sign somebody in through a
// provider.
func (a *authState) ssoEnabled() bool {
	return a.mode == authz.ModeOIDC && a.provider != nil
}

// configureSSO reads the identity-provider settings and builds the client.
//
// Called from configure, so a misconfigured provider stops startup rather than
// surfacing as a broken button. The exception is reachability: discovery is
// deferred, because a provider that is down must not stop an instance from
// starting and offering the local sign-in that is the way back in.
func (a *authState) configureSSO() error {
	if a.mode != authz.ModeOIDC {
		return nil
	}

	// Named one at a time, because this is read at startup by somebody setting
	// the mode up for the first time, and "no issuer configured" does not say
	// which of three variables they missed.
	for _, required := range []struct{ name, value string }{
		{"OIDC_ISSUER", os.Getenv("OIDC_ISSUER")},
		{"OIDC_CLIENT_ID", os.Getenv("OIDC_CLIENT_ID")},
		{"OIDC_REDIRECT_URL", os.Getenv("OIDC_REDIRECT_URL")},
	} {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("%s=%s needs %s to be set", authz.ModeEnv, authz.ModeOIDC, required.name)
		}
	}

	provider, err := oidc.New(oidc.Config{
		Issuer:       os.Getenv("OIDC_ISSUER"),
		ClientID:     os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		RedirectURL:  strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL")),
		Scopes:       splitList(os.Getenv("OIDC_SCOPES")),
	})
	if err != nil {
		return err
	}

	a.provider = provider
	a.sso = ssoSettings{
		UsernameClaim:      strings.TrimSpace(os.Getenv("OIDC_USERNAME_CLAIM")),
		GroupsClaim:        strings.TrimSpace(os.Getenv("OIDC_GROUPS_CLAIM")),
		AdminGroups:        splitList(os.Getenv("OIDC_ADMIN_GROUPS")),
		AllowedGroups:      splitList(os.Getenv("OIDC_ALLOWED_GROUPS")),
		EndSessionOnLogout: envTruthy("OIDC_END_SESSION"),
	}
	if a.sso.GroupsClaim == "" {
		a.sso.GroupsClaim = "groups"
	}
	if a.ssoPending == nil {
		a.ssoPending = newPendingLoginStore()
	}
	log.Printf("auth: sign-in through %s is configured", provider.Issuer())
	return nil
}

// ssoStartHandler sends the browser to the identity provider.
func (a *authState) ssoStartHandler(w http.ResponseWriter, r *http.Request) {
	if !a.ssoEnabled() {
		http.Redirect(w, r, loginPath, http.StatusFound)
		return
	}
	if !principalOf(r).IsAnonymous() {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}

	clientIP := reqsec.ClientIP(r)
	// Throttled on the same limiter as the password form. Each start costs a
	// remembered pending login and a call to the provider, so it is a cost an
	// anonymous caller can impose.
	if ok, _ := a.limiter.Allow("sso:" + clientIP); !ok {
		a.renderLogin(w, r, http.StatusTooManyRequests, "Too many sign-in attempts. Try again shortly.")
		return
	}

	state, err1 := oidc.RandomString(32)
	nonce, err2 := oidc.RandomString(32)
	verifier, err3 := oidc.RandomString(48)
	if err1 != nil || err2 != nil || err3 != nil {
		log.Printf("auth: could not start an SSO sign-in: %v %v %v", err1, err2, err3)
		a.renderLogin(w, r, http.StatusInternalServerError, "Could not start sign-in. Try again.")
		return
	}

	pending := pendingLogin{
		Nonce:     nonce,
		Verifier:  verifier,
		ReturnTo:  safeReturnPath(r.URL.Query().Get("next")),
		ClientIP:  clientIP,
		CreatedAt: time.Now(),
	}
	if !a.ssoPending.begin(state, pending) {
		a.renderLogin(w, r, http.StatusServiceUnavailable, "Too many sign-ins are in progress. Try again shortly.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	target, err := a.provider.AuthCodeURL(ctx, state, nonce, verifier)
	if err != nil {
		// The provider being unreachable is the common case here, and the
		// message has to point at the way in that still works.
		log.Printf("auth: could not reach the identity provider: %v", err)
		a.renderLogin(w, r, http.StatusBadGateway,
			"The identity provider could not be reached. An account with a password can still sign in below.")
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// ssoCallbackHandler completes a sign-in the provider has sent back.
func (a *authState) ssoCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if !a.ssoEnabled() {
		http.Redirect(w, r, loginPath, http.StatusFound)
		return
	}

	clientIP := reqsec.ClientIP(r)
	// Consumed before anything else is looked at, so a callback can only ever
	// be redeemed once, whatever it turns out to say.
	pending, found := a.ssoPending.take(r.URL.Query().Get("state"))
	if !found {
		// Either forged, replayed, or a sign-in that took longer than the
		// window. All three are answered the same way: start again.
		log.Printf("auth: SSO callback from %s with an unknown state", clientIP)
		a.failSSO(w, r, clientIP, "unknown state", "That sign-in has expired or was not started here. Try again.")
		return
	}
	if providerErr := r.URL.Query().Get("error"); providerErr != "" {
		log.Printf("auth: the identity provider refused a sign-in from %s: %s", clientIP, providerErr)
		a.failSSO(w, r, clientIP, "provider refused: "+providerErr,
			"The identity provider did not complete the sign-in.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		a.failSSO(w, r, clientIP, "no authorization code", "That sign-in did not complete. Try again.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	token, err := a.provider.Exchange(ctx, code, pending.Verifier, pending.Nonce)
	if err != nil {
		// Logged in full because an operator debugging a new provider needs
		// the reason; shown to the browser in one sentence because the person
		// signing in cannot act on it.
		log.Printf("auth: SSO sign-in from %s failed: %v", clientIP, err)
		a.failSSO(w, r, clientIP, err.Error(), "The identity provider's answer could not be accepted.")
		return
	}

	if !a.ssoGroupsAllowSignIn(token) {
		log.Printf("auth: %s is authenticated but in none of the groups allowed to sign in", token.Subject)
		a.failSSO(w, r, clientIP, "not in an allowed group",
			"Your account is not permitted to use this 3270Connect console.")
		return
	}

	username := a.ssoUsername(token)
	if username == "" {
		a.failSSO(w, r, clientIP, "no usable username claim",
			"The identity provider did not supply a username.")
		return
	}

	user, err := a.userStore().UpsertExternal(
		a.provider.Issuer(), token.Subject, username,
		a.ssoRole(token), a.ssoGroups(token))
	if err != nil {
		log.Printf("auth: could not resolve %q from the identity provider: %v", username, err)
		message := "Your account could not be set up on this console."
		if strings.Contains(err.Error(), "local account already uses") {
			message = "A local account already uses that name. An administrator has to rename it."
		}
		a.failSSO(w, r, clientIP, err.Error(), message)
		return
	}
	// Disabling is this instance's own veto, and it has to outrank the
	// provider: an administrator who disables somebody here means it whether
	// or not the directory still authenticates them.
	if user.Disabled {
		log.Printf("auth: refusing SSO sign-in for disabled account %q", user.Username)
		a.failSSO(w, r, clientIP, "account disabled", "That account is disabled on this console.")
		return
	}

	if existing := cookieValue(r, authCookieName); existing != "" {
		a.sessions.Delete(existing)
	}
	// The effective role: the account's own, which a directory claim may have
	// just refreshed, or one a group grants — directory groups included, since
	// they were written to the account a moment ago.
	sess, err := a.sessions.Create(user.ID, user.Username, a.effectiveRoleFor(user), clientIP, false)
	if err != nil {
		log.Printf("auth: could not create a session for %q: %v", user.Username, err)
		a.failSSO(w, r, clientIP, "session creation failed", "Could not start a session. Try again.")
		return
	}
	a.limiter.Reset("sso:" + clientIP)
	a.setAuthCookie(w, r, sess.ID)

	log.Printf("auth: %s signed in through the identity provider from %s", user.Username, clientIP)
	a.recorder().Log(audit.Entry{
		Event: audit.EventLoginSucceeded,
		Actor: audit.Actor{UserID: user.ID, Username: user.Username,
			Role: string(user.Role), Kind: string(authz.KindWeb)},
		ClientIP: clientIP,
		Detail:   map[string]string{"method": "sso", "issuer": a.provider.Issuer()},
	})

	http.Redirect(w, r, pending.ReturnTo, http.StatusFound)
}

// failSSO records a refused sign-in and shows the sign-in page saying why.
func (a *authState) failSSO(w http.ResponseWriter, r *http.Request, clientIP, reason, message string) {
	a.limiter.RecordFailure("sso:" + clientIP)
	a.recorder().Log(audit.Entry{
		Event:    audit.EventLoginFailed,
		Outcome:  audit.Failure,
		ClientIP: clientIP,
		Detail:   map[string]string{"method": "sso", "reason": reason},
	})
	a.renderLogin(w, r, http.StatusUnauthorized, message)
}

// ssoUsername picks the display name for an identity.
//
// The configured claim wins. Without one the usual candidates are tried in
// turn, ending at the subject — which is always present, so a provider that
// sends nothing else still produces a usable account rather than a refusal.
//
// This is a name, not the identity: the account is found by issuer and
// subject, so a collision or a rename changes what somebody is called and
// nothing else.
func (a *authState) ssoUsername(token *oidc.IDToken) string {
	candidates := []string{a.sso.UsernameClaim}
	if a.sso.UsernameClaim == "" {
		candidates = []string{"preferred_username", "email", "name"}
	}
	for _, claim := range candidates {
		if name := sanitiseUsername(token.Claim(claim)); name != "" {
			return name
		}
	}
	return sanitiseUsername(token.Subject)
}

// sanitiseUsername reshapes a claim into a name the account store accepts.
//
// The store's charset is deliberately narrow so that two accounts cannot look
// alike in a log or an audit line, and a claim is prose from somewhere else —
// an email address, a display name with a space in it. Characters outside the
// set become a dash rather than being dropped, so "a.b@c" and "a.bc" stay
// distinguishable.
func sanitiseUsername(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 64 {
		name = strings.Trim(name[:64], "-")
	}
	return name
}

// ssoRole maps the provider's groups onto a role.
//
// An empty result means "this deployment does not map roles", which
// UpsertExternal reads as leave the account as it is. That is what lets an
// administrator promote somebody on the Accounts page without the next
// sign-in undoing it.
func (a *authState) ssoRole(token *oidc.IDToken) authz.Role {
	if len(a.sso.AdminGroups) == 0 {
		return ""
	}
	if intersects(token.ClaimList(a.sso.GroupsClaim), a.sso.AdminGroups) {
		return authz.RoleAdmin
	}
	// Explicitly the user role rather than "leave it": where the provider is
	// the authority on who administers, leaving somebody an administrator
	// after they left the group would be the provider failing to revoke.
	return authz.RoleUser
}

// ssoGroups is the directory's group list for this identity, or nil where the
// deployment maps no claim.
//
// nil rather than empty, because the two mean different things to the account
// store: nil leaves whatever an administrator set here, empty says the
// directory is the authority and this person is in nothing.
func (a *authState) ssoGroups(token *oidc.IDToken) []string {
	if a.sso.GroupsClaim == "" {
		return nil
	}
	groups := token.ClaimList(a.sso.GroupsClaim)
	if groups == nil {
		// The claim is configured but absent from the token. Treated as "in
		// nothing" rather than "say nothing": a provider that stopped sending
		// the claim has removed the person from every group as far as anyone
		// here can tell, and keeping stale membership would keep access it was
		// meant to withdraw.
		return []string{}
	}
	return groups
}

// ssoGroupsAllowSignIn reports whether the identity may sign in at all.
func (a *authState) ssoGroupsAllowSignIn(token *oidc.IDToken) bool {
	if len(a.sso.AllowedGroups) == 0 {
		return true
	}
	return intersects(token.ClaimList(a.sso.GroupsClaim), a.sso.AllowedGroups)
}

// intersects reports whether the two lists share a value, comparing without
// regard to case because directories are inconsistent about it.
func intersects(have, want []string) bool {
	for _, h := range have {
		for _, w := range want {
			if strings.EqualFold(strings.TrimSpace(h), w) {
				return true
			}
		}
	}
	return false
}

// ssoLogoutURL is where to send the browser after signing out, when the
// deployment asked for the provider's session to end too.
func (a *authState) ssoLogoutURL(r *http.Request) string {
	if !a.ssoEnabled() || !a.sso.EndSessionOnLogout {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	return a.provider.EndSessionURL(ctx, "", absoluteURL(r, loginPath))
}
