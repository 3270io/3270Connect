package main

// First-run setup: creating the administrator an instance with accounts needs
// before anybody can sign in.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/3270io/3270Connect/internal/audit"
	"github.com/3270io/3270Connect/internal/reqsec"
	"github.com/3270io/3270Connect/internal/users"
)

// setupState tracks whether the instance still needs its first administrator.
//
// The flag is cached rather than recomputed from the account store on every
// request: it is consulted by the gate on the hot path, and re-reading a file
// per request to answer a question that changes exactly once would be waste.
type setupState struct {
	mu       sync.RWMutex
	required bool
	// code must be presented on the setup form. Generated per start, and only
	// while setup is pending.
	code string
}

func (s *setupState) begin(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.required = true
	s.code = code
}

func (s *setupState) complete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.required = false
	s.code = ""
}

func (s *setupState) pending() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.required
}

// matches compares a submitted code in constant time.
func (s *setupState) matches(candidate string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.code == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(normaliseSetupCode(candidate)), []byte(s.code)) == 1
}

// newSetupCode returns a short, unambiguous code for reading off a log and
// typing into a form.
//
// Base32 without padding avoids the character pairs people misread when
// copying by hand (0/O, 1/I/l), which matters because this is a value somebody
// transcribes from a terminal rather than pastes.
func newSetupCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate setup code: %w", err)
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return strings.ToUpper(raw), nil
}

// normaliseSetupCode makes transcription forgiving: case, and the spaces or
// dashes people insert while copying, are ignored.
func normaliseSetupCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// formatSetupCode groups the code for legibility in the log.
func formatSetupCode(code string) string {
	var parts []string
	for i := 0; i < len(code); i += 4 {
		end := i + 4
		if end > len(code) {
			end = len(code)
		}
		parts = append(parts, code[i:end])
	}
	return strings.Join(parts, "-")
}

// beginSetupIfNeeded arms first-run setup when the mode needs accounts and
// none exist yet.
//
// Requiring a code from the server's own log, rather than letting the first
// visitor claim the instance, is what keeps the window between "started" and
// "has an administrator" from being a land grab. Whoever can read the log is
// already trusted; whoever merely reached the port is not.
func (a *authState) beginSetupIfNeeded() error {
	// Every mode that separates users needs a first account, including the one
	// that signs people in through an identity provider: that account is the
	// local administrator who can still get in when the provider cannot be
	// reached, and somebody has to exist before anybody can be given a role.
	if !a.separatesUsers() {
		return nil
	}

	count, err := a.userStore().Count()
	if err != nil {
		return fmt.Errorf("read the account store %s: %w", a.userStore().Path(), err)
	}
	if count > 0 {
		a.setup.complete()
		return nil
	}

	code, err := newSetupCode()
	if err != nil {
		return err
	}
	a.setup.begin(code)
	announceSetupCode(code)
	return nil
}

// announceSetupCode puts the code where whoever started the server will look.
//
// To the log and also to stderr, because those are different places under
// Docker: `docker compose logs` shows the process's output, so a code that
// only reached a file would make the documented first-run flow impossible to
// complete without exec'ing into the container. On a desktop the two land in
// the same terminal, which costs a duplicate line in the one case where
// nobody needed the help.
func announceSetupCode(code string) {
	const (
		intro  = "auth: no accounts yet — open the console to create the first administrator"
		expiry = "auth: the code is required once, and stops working as soon as the account exists"
	)
	line := fmt.Sprintf("auth: setup code: %s", formatSetupCode(code))

	log.Print(intro)
	log.Print(line)
	log.Print(expiry)

	fmt.Fprintln(os.Stderr, intro)
	fmt.Fprintln(os.Stderr, line)
	fmt.Fprintln(os.Stderr, expiry)
}

// setupStillPending reports whether the instance is waiting for its first
// administrator, re-reading the account store if it thinks so.
//
// The check at startup is a snapshot and an account can appear afterwards:
// `3270Connect user add` edits the same file while the console runs, which is
// the documented way to create one without a browser. Without this re-read the
// instance would go on funnelling every request into setup — showing a code
// that no longer works — until somebody restarted it.
//
// The extra read costs nothing in the state that matters: it happens only
// while setup is pending, and pending ends the moment an account exists.
func (a *authState) setupStillPending() bool {
	if !a.setup.pending() {
		return false
	}
	count, err := a.userStore().Count()
	if err != nil || count == 0 {
		return true
	}
	a.setup.complete()
	log.Print("auth: an account now exists; first-run setup is closed")
	return false
}

// gateSetup funnels every request to the setup page until an administrator
// exists, and reports whether it answered the request itself.
//
// Without this an operator would meet a sign-in form with no account that can
// pass it, which looks like a broken instance rather than one waiting to be
// configured.
func (a *authState) gateSetup(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	pending := a.setupStillPending()

	if !pending {
		// Setup is a one-time door; leaving it open afterwards would be a
		// second way to create an administrator.
		if path == setupPath {
			http.Redirect(w, r, loginPath, http.StatusFound)
			return true
		}
		return false
	}

	if path == setupPath || path == "/healthz" {
		return false
	}
	// A token client cannot complete setup and should not be redirected into
	// it; the 503 already says the instance is not ready.
	if wantsJSON(r) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "3270Connect is not set up yet",
			"setup": setupPath,
		})
		return true
	}
	http.Redirect(w, r, setupPath, http.StatusFound)
	return true
}

func (a *authState) setupHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.renderSetup(w, r, http.StatusOK, "")
	case http.MethodPost:
		a.doSetup(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *authState) renderSetup(w http.ResponseWriter, r *http.Request, status int, message string) {
	renderAuthPage(w, r, status, "setup.gohtml", authPageData{
		Title:          "First run",
		Error:          message,
		MinLength:      users.MinPasswordLength,
		ShowNoTLS:      !reqsec.IsTLS(r),
		ProxySaysHTTPS: proxyClaimsHTTPS(r),
	})
}

// doSetup creates the first administrator.
func (a *authState) doSetup(w http.ResponseWriter, r *http.Request) {
	if !a.setup.pending() {
		http.Redirect(w, r, loginPath, http.StatusFound)
		return
	}

	clientIP := reqsec.ClientIP(r)
	code := r.FormValue("setupCode")
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirmPassword")

	// Throttle the code as if it were a password, because that is what it is.
	limitKeys := []string{"setup:" + clientIP}
	if ok, _ := a.limiter.Allow(limitKeys...); !ok {
		a.renderSetup(w, r, http.StatusTooManyRequests, "Too many attempts. Try again shortly.")
		return
	}

	if !a.setup.matches(code) {
		a.limiter.RecordFailure(limitKeys...)
		log.Printf("auth: setup attempted from %s with an incorrect code", clientIP)
		a.recorder().Log(audit.Entry{
			Event: audit.EventFirstAdmin, Outcome: audit.Denied, ClientIP: clientIP,
			Detail: map[string]string{"reason": "incorrect setup code"},
		})
		a.renderSetup(w, r, http.StatusUnauthorized,
			"That setup code is not correct. It is printed in the server log.")
		return
	}
	if err := users.ValidateUsername(username); err != nil {
		a.renderSetup(w, r, http.StatusBadRequest, humanStoreError(err))
		return
	}
	if password != confirm {
		a.renderSetup(w, r, http.StatusBadRequest, "The passwords do not match.")
		return
	}
	if err := users.ValidatePassword(password); err != nil {
		a.renderSetup(w, r, http.StatusBadRequest, humanStoreError(err))
		return
	}

	user, err := a.userStore().AddFirstAdmin(username, password)
	if err != nil {
		if errors.Is(err, users.ErrNotFirstUser) {
			// Another request won the race; there is nothing to set up.
			a.setup.complete()
			http.Redirect(w, r, loginPath, http.StatusFound)
			return
		}
		log.Printf("auth: setup failed: %v", err)
		a.renderSetup(w, r, http.StatusInternalServerError,
			"Could not create the account. Check the server log.")
		return
	}

	a.setup.complete()
	a.limiter.Reset(limitKeys...)
	log.Printf("auth: first administrator %q created from %s; setup is now closed", user.Username, clientIP)
	a.recorder().Log(audit.Entry{
		Event:    audit.EventFirstAdmin,
		Actor:    audit.Actor{UserID: user.ID, Username: user.Username, Role: string(user.Role)},
		ClientIP: clientIP,
		Target:   user.Username,
	})

	// Sign the new administrator straight in — making them retype the password
	// they just chose proves nothing.
	sess, err := a.sessions.Create(user.ID, user.Username, user.Role, clientIP, false)
	if err != nil {
		http.Redirect(w, r, loginPath, http.StatusFound)
		return
	}
	a.setAuthCookie(w, r, sess.ID)
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}
