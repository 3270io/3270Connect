package main

// The credentials automated clients present.
//
// Two shapes, and which one is accepted follows from the deployment:
//
//   - With a single operator there is one person, so one shared token in the
//     environment says everything there is to say about who is calling. It
//     resolves to that operator, which is why a run started over the API can
//     be stopped from the console.
//   - Where users are separated a shared token would be a hole straight
//     through that separation: one credential, held by everyone, able to start
//     and stop everyone's runs. Only tokens issued to an account are accepted,
//     and each reaches exactly what its owner reaches.
//
// The default — a single operator with no API_TOKEN set — leaves the REST API
// exactly as it has always been: open, on a listener that binds localhost.
// That is a decision about the listener, not about credentials, and it is the
// one an existing installation is relying on.

import (
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/3270io/3270Connect/internal/apitoken"
	"github.com/3270io/3270Connect/internal/audit"
	"github.com/3270io/3270Connect/internal/authz"
	"github.com/3270io/3270Connect/internal/reqsec"
)

// bearerToken extracts the credential from the Authorization header.
func bearerToken(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	value := strings.TrimSpace(header[len(prefix):])
	return value, value != ""
}

// apiAuthError is an answer already decided: a status and a message. Keeping
// the two together stops a caller from pairing "unauthorized" with a body that
// says something else.
type apiAuthError struct {
	status  int
	message string
}

func (e apiAuthError) Error() string { return e.message }

var (
	errNoToken  = apiAuthError{http.StatusUnauthorized, "missing Bearer token"}
	errBadToken = apiAuthError{http.StatusUnauthorized, "invalid token"}
)

// authenticateToken resolves a presented bearer token into the principal it
// speaks for.
func (a *authState) authenticateToken(presented string) (authz.Principal, error) {
	if !a.separatesUsers() {
		configured := a.sharedToken
		if configured == "" {
			// Nothing to check against. A caller that sent a token to an
			// instance with none configured is the single operator talking to
			// their own console with a header left over from somewhere else;
			// refusing them would break a default that has always worked.
			return authz.Local(), nil
		}
		if presented == "" {
			return authz.Anonymous(), errNoToken
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) != 1 {
			return authz.Anonymous(), errBadToken
		}
		return authz.Service(), nil
	}

	if presented == "" {
		return authz.Anonymous(), errNoToken
	}
	token, err := a.tokenStore().Verify(presented)
	if err != nil {
		// One answer for every way a token can be unusable. Telling a caller
		// that the token is real but expired, or real but revoked, confirms
		// that the token is real.
		if !errors.Is(err, apitoken.ErrMalformed) && !errors.Is(err, apitoken.ErrNotFound) {
			log.Printf("auth: refusing API token: %v", err)
		}
		return authz.Anonymous(), errBadToken
	}

	owner, found, err := a.userStore().ByID(token.UserID)
	if err != nil {
		return authz.Anonymous(), apiAuthError{http.StatusInternalServerError, "could not read the account"}
	}
	// A token outliving its account, or surviving the account being disabled,
	// would make both of those things mean less than they say.
	if !found || owner.Disabled {
		log.Printf("auth: refusing token %s: its account is gone or disabled", token.ID)
		return authz.Anonymous(), errBadToken
	}

	// The effective role, so a token issued to somebody whose administration
	// comes from a group carries the same rights their browser does.
	return authz.Token(owner.ID, a.effectiveRoleFor(owner), token.Scopes), nil
}

// tokenScopeAllows reports whether a principal's scopes permit this request.
func tokenScopeAllows(method string, principal authz.Principal) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return principal.HasScope(apitoken.ScopeRead)
	default:
		return principal.HasScope(apitoken.ScopeWrite)
	}
}

// apiRequiresToken reports whether the REST API refuses an anonymous caller.
//
// False for the historical default — a single operator with no API_TOKEN — and
// true as soon as either a shared token or accounts exist.
func (a *authState) apiRequiresToken() bool {
	return a.separatesUsers() || a.sharedToken != ""
}

// missingCredentialMessage names the credential this deployment expects, so a
// 401 says what to go and get rather than only that something is missing.
func (a *authState) missingCredentialMessage() string {
	if a.separatesUsers() {
		return "authentication required: present a Bearer token issued with `3270Connect token add <username> <name>`"
	}
	return "authentication required: present the Bearer token configured in API_TOKEN"
}

/* ---------------------------------------------------------------------
   The REST API listener
   --------------------------------------------------------------------- */

// GinAuth is the same gate, for the `-api` listener.
//
// The REST API runs on Gin and on a port of its own, so it does not pass
// through the console's wrapper. It resolves the same principals from the same
// stores and applies the same scope rule; what it does not do is redirect
// anybody to a sign-in page, because nothing that speaks to this listener has
// a browser to follow one.
func (a *authState) GinAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		refuse := func(status int, message string) {
			a.recorder().Log(audit.Entry{
				Event:    audit.EventTokenRefused,
				Outcome:  audit.Denied,
				ClientIP: reqsec.ClientIP(c.Request),
				Detail:   map[string]string{"path": c.Request.URL.Path},
			})
			c.AbortWithStatusJSON(status, gin.H{"error": message})
		}

		// Asked here rather than inside resolve, because the two listeners
		// disagree about what a missing credential means and should. On the
		// console with a single operator there is nobody to authenticate and
		// the page must open; on this listener, an API_TOKEN that is set is
		// somebody asking for the API to be closed, and a request that sends
		// no header at all must not be answered as that operator.
		if _, presented := bearerToken(c.Request); !presented && a.apiRequiresToken() {
			refuse(http.StatusUnauthorized, a.missingCredentialMessage())
			return
		}

		identity, err := a.resolve(c.Request)
		if err != nil {
			var answer apiAuthError
			if !errors.As(err, &answer) {
				answer = errBadToken
			}
			refuse(answer.status, answer.message)
			return
		}

		if identity.Principal.IsAnonymous() {
			refuse(http.StatusUnauthorized, a.missingCredentialMessage())
			return
		}
		if identity.Principal.Kind == authz.KindAPIToken &&
			!tokenScopeAllows(c.Request.Method, identity.Principal) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "this token is read-only; executing a workflow needs the \"write\" scope",
			})
			return
		}

		c.Request = withIdentity(c.Request, identity)
		c.Next()
	}
}
