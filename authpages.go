package main

// The pages authentication and administration need, and the one place they
// are rendered from.
//
// They are separate from templates/dashboard.gohtml because they are a
// different kind of page: the console is one long live document that refreshes
// itself, and these are short forms and tables that are correct at the moment
// they are served. Sharing a layout with it would mean the sign-in page could
// not render until the metrics did.

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed templates/auth/layout.gohtml templates/auth/pages/*.gohtml
var authTemplateFS embed.FS

// authPages holds one parsed template set per page.
//
// One set each rather than one set for all of them, because every page defines
// a block called "content" and they would otherwise overwrite each other —
// html/template has a single namespace per set, and the last file parsed would
// silently become every page's body. Parsing the shared layout alongside each
// page keeps the blocks scoped to the page that meant them.
var authPages = parseAuthPages()

var authTemplateFuncs = template.FuncMap{
	"year":     func() int { return time.Now().Year() },
	"adminNav": adminNavHTML,
}

func parseAuthPages() map[string]*template.Template {
	out := map[string]*template.Template{}
	entries, err := fs.Glob(authTemplateFS, "templates/auth/pages/*.gohtml")
	if err != nil {
		log.Printf("auth: could not list the console's page templates: %v", err)
		return out
	}
	for _, entry := range entries {
		name := path.Base(entry)
		t, err := template.New(name).Funcs(authTemplateFuncs).
			ParseFS(authTemplateFS, "templates/auth/layout.gohtml", entry)
		if err != nil {
			// Fatal to the page, not to the process: an operator whose console
			// still works can read the log and reinstall, which beats a binary
			// that refuses to start.
			log.Printf("auth: could not parse %s: %v", entry, err)
			continue
		}
		out[name] = t
	}
	return out
}

// authPageData is everything any of these pages can need.
//
// One struct rather than one per page: they share a layout, the layout reads
// the chrome fields, and a page that leaves a field zero simply does not
// render it. Splitting them would mean the layout could not be a layout.
type authPageData struct {
	// Title is the browser tab and the heading.
	Title string
	// Version is stamped on asset URLs so an upgrade does not serve yesterday's
	// stylesheet out of a cache.
	Version string

	// Error and Notice are the two things a form says back. Error is a
	// refusal; Notice is an explanation that is not a refusal.
	Error  string
	Notice string
	// Message is body prose for a page that is not a form.
	Message string

	// MinLength is the password floor, so a form states the rule it enforces.
	MinLength int
	// Forced marks a password change the account cannot skip.
	Forced bool

	// ShowNoTLS and ProxySaysHTTPS drive the connection warning. See
	// proxyClaimsHTTPS for why the second exists.
	ShowNoTLS      bool
	ProxySaysHTTPS bool

	SSOEnabled   bool
	SSOStartPath string
	// Next is where to land after signing in.
	Next string

	// Auth is who is looking at the page, for the header chip and for deciding
	// whether administrative controls belong on it.
	Auth consoleAuthView
	// Active names the administration tab to mark as current.
	Active string
	// AuthMode is shown on the administration pages, because "why can I not
	// add an account" is nearly always answered by it.
	AuthMode string
	// MaxGroupName and MaxGroupDescription let a form bound its inputs to what
	// the store accepts. A form that takes more than the store does is a
	// refusal somebody only meets after filling in the whole dialog.
	MaxGroupName        int
	MaxGroupDescription int
	// Limit is how many audit entries the page asked for.
	Limit int
}

// renderAuthPage writes one of these pages.
func renderAuthPage(w http.ResponseWriter, r *http.Request, status int, page string, data authPageData) {
	t, ok := authPages[page]
	if !ok || t == nil {
		log.Printf("auth: no template named %q", page)
		http.Error(w, "this page could not be loaded; check the server log", http.StatusInternalServerError)
		return
	}

	data.Version = version
	if data.AuthMode == "" {
		data.AuthMode = string(auth.mode)
	}
	if (data.Auth == consoleAuthView{}) {
		data.Auth = auth.consoleAuthView(r)
	}

	// These pages were written for a strict policy: every asset comes from
	// this binary, there is no inline script and no inline style, and nothing
	// they show is worth framing. The console's own page cannot have this —
	// see securityHeaders — which is exactly why these get it.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+
			"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		// Too late for a status code — the header is already out — so this
		// leaves the note in the log and ends the response.
		log.Printf("auth: could not render %s: %v", page, err)
	}
}

// adminNavItems is the administration area's navigation, in the order it is
// shown. Kept here rather than in the template so a page and its tab cannot
// drift apart.
var adminNavItems = []struct{ Key, Label, Href string }{
	{"overview", "Overview", "/admin"},
	{"users", "Accounts", "/admin/users"},
	{"groups", "Groups", "/admin/groups"},
	{"tokens", "API tokens", "/admin/tokens"},
	{"runs", "Load runs", "/admin/runs"},
	{"audit", "Audit trail", "/admin/audit"},
}

// adminNavHTML renders the navigation for a page.
//
// Built in Go rather than ranged over in the template because the template
// needs to know which item is current, and a one-line helper reads better than
// the conditional it would otherwise carry.
func adminNavHTML(active string) template.HTML {
	var b strings.Builder
	for _, item := range adminNavItems {
		class := "admin-nav-item"
		aria := ""
		if item.Key == active {
			class += " is-current"
			aria = ` aria-current="page"`
		}
		fmt.Fprintf(&b, `<a class=%q href=%q%s>%s</a>`,
			class, item.Href, aria, template.HTMLEscapeString(item.Label))
	}
	return template.HTML(b.String()) //nolint:gosec // labels and hrefs are constants above
}
