package main

// `3270Connect user ...` and `3270Connect token ...`.
//
// Both edit the same files the console reads, so they work whether or not a
// console is running — a new account can sign in immediately, with no restart.
// The one thing they cannot do is reach into a running process's memory: a
// browser already signed in is ended by the periodic sweep rather than on the
// spot, which is what the messages below say.
//
// They write to the writers they are given and return an exit code rather than
// calling os.Exit, so the whole thing stays testable.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/3270io/3270Connect/internal/apitoken"
	"github.com/3270io/3270Connect/internal/audit"
	"github.com/3270io/3270Connect/internal/authz"
	"github.com/3270io/3270Connect/internal/users"
)

const userUsage = `Manage local 3270Connect accounts.

Usage:
  3270Connect user add <username> [--admin]   Create an account
  3270Connect user list                       List accounts
  3270Connect user passwd <username>          Set an account's password
  3270Connect user enable <username>          Re-enable a disabled account
  3270Connect user disable <username>         Disable an account

Accounts apply when AUTH_MODE is local or oidc. Passwords are read from the
terminal, or from stdin when it is not a terminal; they are never taken from
the command line, where they would be visible in the process list and recorded
in shell history.
`

// runUserCLI implements the `user` subcommand.
func runUserCLI(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, userUsage)
		return 2
	}

	store := users.NewStore(resolveUsersPath())

	switch args[0] {
	case "add":
		return userAdd(store, args[1:], stdout, stderr, stdin)
	case "list", "ls":
		return userList(store, stdout, stderr)
	case "passwd":
		return userPasswd(store, args[1:], stdout, stderr, stdin)
	case "enable":
		return userSetDisabled(store, args[1:], false, stdout, stderr)
	case "disable":
		return userSetDisabled(store, args[1:], true, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, userUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown user command %q\n\n%s", args[0], userUsage)
		return 2
	}
}

func userAdd(store *users.Store, args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	var username string
	role := authz.RoleUser
	for _, arg := range args {
		switch arg {
		case "--admin":
			role = authz.RoleAdmin
		case "--user":
			role = authz.RoleUser
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(stderr, "unknown option %q\n", arg)
				return 2
			}
			if username != "" {
				fmt.Fprintf(stderr, "unexpected argument %q\n", arg)
				return 2
			}
			username = arg
		}
	}
	if username == "" {
		fmt.Fprint(stderr, "a username is required\n\n"+userUsage)
		return 2
	}
	if err := users.ValidateUsername(username); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	password, err := readNewPassword(stdout, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	user, err := store.Add(username, password, role, false)
	if err != nil {
		if errors.Is(err, users.ErrUserExists) {
			fmt.Fprintf(stderr, "a user named %q already exists\n", username)
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "created %s (%s) in %s\n", user.Username, user.Role, store.Path())
	return 0
}

func userList(store *users.Store, stdout, stderr io.Writer) int {
	list, err := store.List()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintf(stdout, "no accounts yet (%s)\n", store.Path())
		return 0
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].Username) < strings.ToLower(list[j].Username)
	})

	roles, _ := store.GroupRoles()

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tROLE\tSTATUS\tGROUPS\tCREATED")
	for _, u := range list {
		status := "enabled"
		switch {
		case u.Disabled:
			status = "disabled"
		case u.MustChangePassword:
			status = "must change password"
		}
		// The effective role, with the group named when it is the reason —
		// "why is this person an administrator" has to be answerable from the
		// line, exactly as it is on the Accounts page.
		role := string(users.EffectiveRole(u, roles))
		if granting := users.RoleGrantingGroups(u, roles); len(granting) > 0 && u.Role != authz.RoleAdmin {
			role += " (via " + strings.Join(granting, ",") + ")"
		}
		groups := strings.Join(u.Groups, ",")
		if groups == "" {
			groups = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", u.Username, role, status, groups, u.CreatedAt.Format("2006-01-02"))
	}
	w.Flush()
	return 0
}

func userPasswd(store *users.Store, args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a username is required\n\n"+userUsage)
		return 2
	}
	password, err := readNewPassword(stdout, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if err := store.SetPassword(args[0], password); err != nil {
		if errors.Is(err, users.ErrUserNotFound) {
			fmt.Fprintf(stderr, "no user named %q\n", args[0])
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "password updated for %s\n", args[0])
	fmt.Fprintln(stdout, "note: a running console notices within a few minutes; sessions already signed in are ended then")
	return 0
}

func userSetDisabled(store *users.Store, args []string, disabled bool, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a username is required\n\n"+userUsage)
		return 2
	}
	if err := store.SetDisabled(args[0], disabled); err != nil {
		if errors.Is(err, users.ErrUserNotFound) {
			fmt.Fprintf(stderr, "no user named %q\n", args[0])
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if disabled {
		fmt.Fprintf(stdout, "disabled %s\n", args[0])
		// Said plainly, because the two halves genuinely differ: a token is
		// checked against the account on every call, and a browser session
		// lives in the console's memory until the sweep looks at it.
		fmt.Fprintln(stdout, "their API tokens stop working at once; a browser already signed in is ended within a few minutes")
	} else {
		fmt.Fprintf(stdout, "enabled %s\n", args[0])
	}
	return 0
}

// readNewPassword prompts twice on a terminal, or reads one line from stdin
// when it is piped.
func readNewPassword(stdout io.Writer, stdin io.Reader) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(stdout, "New password: ")
		first, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		fmt.Fprint(stdout, "Confirm password: ")
		second, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		if string(first) != string(second) {
			return "", errors.New("the passwords do not match")
		}
		return validatedPassword(string(first))
	}

	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no password on stdin")
	}
	return validatedPassword(strings.TrimRight(line, "\r\n"))
}

func validatedPassword(password string) (string, error) {
	if err := users.ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}

/* ---------------------------------------------------------------------
   Tokens
   --------------------------------------------------------------------- */

const tokenUsage = `Manage API tokens for automated clients.

Usage:
  3270Connect token add <username> <name> [--read-only] [--expires <duration>]
                                              Issue a token for an account
  3270Connect token list [username]           List tokens
  3270Connect token revoke <id>               Stop a token working
  3270Connect token revoke-all <username>     Stop all of an account's tokens

A token reaches exactly what its owner reaches: their load runs, and nothing
belonging to anybody else. --read-only issues one that can watch runs but not
start or stop them.

The token is shown once, when it is issued. It is stored as a hash, so a lost
token is replaced rather than recovered.

Tokens apply when AUTH_MODE is local or oidc. Without accounts there is one
operator and one API_TOKEN environment variable, which is all there is to say
about who is calling.
`

// runTokenCLI implements the `token` subcommand.
func runTokenCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, tokenUsage)
		return 2
	}

	tokens := apitoken.NewStore(resolveTokensPath())
	accounts := users.NewStore(resolveUsersPath())
	// The console command writes to the same trail the server does. Issuing a
	// credential from a container shell is exactly the act somebody would want
	// a record of, and it leaves no other trace.
	trail := audit.NewRecorder(resolveAuditPath())

	switch args[0] {
	case "add", "create":
		return tokenAdd(tokens, accounts, trail, args[1:], stdout, stderr)
	case "list", "ls":
		return tokenList(tokens, accounts, args[1:], stdout, stderr)
	case "revoke":
		return tokenRevoke(tokens, trail, args[1:], stdout, stderr)
	case "revoke-all":
		return tokenRevokeAll(tokens, accounts, trail, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, tokenUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown token command %q\n\n%s", args[0], tokenUsage)
		return 2
	}
}

func tokenAdd(tokens *apitoken.Store, accounts *users.Store, trail *audit.Recorder, args []string, stdout, stderr io.Writer) int {
	var positional []string
	scopes := []string{apitoken.ScopeRead, apitoken.ScopeWrite}
	var expiresAt *time.Time

	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--read-only":
			scopes = []string{apitoken.ScopeRead}
		case "--expires":
			if i+1 >= len(args) {
				fmt.Fprint(stderr, "--expires needs a duration, for example 720h\n")
				return 2
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil || d <= 0 {
				fmt.Fprintf(stderr, "cannot read %q as a duration; try 24h or 720h\n", args[i])
				return 2
			}
			when := time.Now().Add(d)
			expiresAt = &when
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(stderr, "unknown option %q\n", arg)
				return 2
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) != 2 {
		fmt.Fprint(stderr, "a username and a name for the token are required\n\n"+tokenUsage)
		return 2
	}
	username, name := positional[0], positional[1]

	owner, ok := findAccount(accounts, username, stderr)
	if !ok {
		return 1
	}

	record, secret, err := tokens.Issue(owner.ID, name, scopes, expiresAt)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	trail.Log(audit.Entry{
		Event:  audit.EventTokenIssued,
		Actor:  audit.Actor{Username: "console"},
		Target: record.ID,
		Detail: map[string]string{
			"account": owner.Username,
			"name":    record.Name,
			"scopes":  strings.Join(record.Scopes, "+"),
		},
	})

	fmt.Fprintf(stdout, "issued %s for %s (%s)\n", record.ID, owner.Username, strings.Join(record.Scopes, "+"))
	if record.ExpiresAt != nil {
		fmt.Fprintf(stdout, "expires %s\n", record.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintf(stdout, "\n  %s\n\n", secret)
	fmt.Fprintln(stdout, "This is the only time the token is shown. Store it somewhere the client can read")
	fmt.Fprintln(stdout, "and nobody else can.")
	return 0
}

func tokenList(tokens *apitoken.Store, accounts *users.Store, args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprint(stderr, "at most one username\n\n"+tokenUsage)
		return 2
	}

	filterID := ""
	if len(args) == 1 {
		owner, ok := findAccount(accounts, args[0], stderr)
		if !ok {
			return 1
		}
		filterID = owner.ID
	}

	list, err := tokens.List()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	names := accountNames(accounts)
	rows := make([]apitoken.Token, 0, len(list))
	for _, tok := range list {
		if filterID == "" || tok.UserID == filterID {
			rows = append(rows, tok)
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "no tokens issued")
		return 0
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })

	now := time.Now()
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tACCOUNT\tNAME\tSCOPES\tSTATUS\tLAST USED")
	for _, tok := range rows {
		owner := names[tok.UserID]
		if owner == "" {
			owner = "(deleted)"
		}
		lastUsed := "never"
		if tok.LastUsedAt != nil {
			lastUsed = tok.LastUsedAt.Format("2006-01-02")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			tok.ID, owner, tok.Name, strings.Join(tok.Scopes, "+"), tokenStatus(tok, now), lastUsed)
	}
	w.Flush()
	return 0
}

func tokenRevoke(tokens *apitoken.Store, trail *audit.Recorder, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a token id is required\n\n"+tokenUsage)
		return 2
	}
	if err := tokens.Revoke(args[0]); err != nil {
		if errors.Is(err, apitoken.ErrNotFound) {
			fmt.Fprintf(stderr, "no token with id %q\n", args[0])
			return 1
		}
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	trail.Log(audit.Entry{
		Event: audit.EventTokenRevoked, Actor: audit.Actor{Username: "console"}, Target: args[0],
	})
	fmt.Fprintf(stdout, "revoked %s\n", args[0])
	return 0
}

func tokenRevokeAll(tokens *apitoken.Store, accounts *users.Store, trail *audit.Recorder, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, "a username is required\n\n"+tokenUsage)
		return 2
	}
	owner, ok := findAccount(accounts, args[0], stderr)
	if !ok {
		return 1
	}
	n, err := tokens.RevokeAllFor(owner.ID)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if n > 0 {
		trail.Log(audit.Entry{
			Event: audit.EventTokenRevoked, Actor: audit.Actor{Username: "console"},
			Target: owner.Username, Detail: map[string]string{"count": strconv.Itoa(n)},
		})
	}
	fmt.Fprintf(stdout, "revoked %d token(s) for %s\n", n, owner.Username)
	return 0
}

// findAccount resolves a username, reporting the failure itself so each caller
// does not repeat the message.
func findAccount(accounts *users.Store, username string, stderr io.Writer) (users.User, bool) {
	list, err := accounts.List()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return users.User{}, false
	}
	for _, u := range list {
		if strings.EqualFold(u.Username, username) {
			return u, true
		}
	}
	fmt.Fprintf(stderr, "no user named %q\n", username)
	return users.User{}, false
}

// accountNames maps account identifiers to usernames, so a token list reads in
// terms of people rather than hex strings.
func accountNames(accounts *users.Store) map[string]string {
	names := map[string]string{}
	list, err := accounts.List()
	if err != nil {
		return names
	}
	for _, u := range list {
		names[u.ID] = u.Username
	}
	return names
}
