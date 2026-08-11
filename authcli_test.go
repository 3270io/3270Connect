package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3270io/3270Connect/internal/apitoken"
	"github.com/3270io/3270Connect/internal/users"
)

// cliDir points the console commands at a temporary state directory, so a test
// never edits the accounts a real console keeps.
func cliDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERS_PATH", filepath.Join(dir, "users.json"))
	t.Setenv("API_TOKENS_PATH", filepath.Join(dir, "api-tokens.json"))
	t.Setenv("AUDIT_LOG_PATH", filepath.Join(dir, "audit.log"))
	return dir
}

// runUser calls the user command with a password on stdin, the way a script
// pipes one in.
func runUser(args []string, password string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = runUserCLI(args, &out, &errOut, strings.NewReader(password+"\n"))
	return code, out.String(), errOut.String()
}

func runToken(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = runTokenCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUserCLIAddListDisable(t *testing.T) {
	cliDir(t)

	if code, _, errOut := runUser([]string{"add", "root", "--admin"}, "correct-horse-battery"); code != 0 {
		t.Fatalf("add root: exit %d, %s", code, errOut)
	}
	if code, _, errOut := runUser([]string{"add", "alice"}, "correct-horse-battery"); code != 0 {
		t.Fatalf("add alice: exit %d, %s", code, errOut)
	}

	code, out, _ := runUser([]string{"list"}, "")
	if code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	for _, want := range []string{"root", "alice", "admin", "user"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output is missing %q:\n%s", want, out)
		}
	}

	if code, out, _ := runUser([]string{"disable", "alice"}, ""); code != 0 {
		t.Fatalf("disable: exit %d", code)
	} else if !strings.Contains(out, "API tokens stop working at once") {
		// The two halves genuinely differ in when they take effect, and saying
		// so is the difference between an operator who waits and one who
		// assumes it did not work.
		t.Errorf("disable should say what happens to tokens and to open sessions:\n%s", out)
	}

	if code, _, _ := runUser([]string{"enable", "alice"}, ""); code != 0 {
		t.Fatal("enable should succeed")
	}
}

func TestUserCLIRefusesAPasswordOnTheCommandLine(t *testing.T) {
	// A password in argv is visible to every other process on the machine and
	// lands in shell history, so there is deliberately no flag for it.
	cliDir(t)
	code, _, errOut := runUser([]string{"add", "alice", "--password", "hunter2hunter2"}, "correct-horse-battery")
	if code == 0 {
		t.Fatal("an unknown option must not be accepted silently")
	}
	if !strings.Contains(errOut, "unknown option") {
		t.Errorf("stderr should name the unknown option: %s", errOut)
	}
}

func TestUserCLIRejectsAShortPassword(t *testing.T) {
	cliDir(t)
	code, _, errOut := runUser([]string{"add", "alice"}, "short")
	if code == 0 {
		t.Fatal("a password under the floor must be refused")
	}
	if !strings.Contains(errOut, "at least") {
		t.Errorf("stderr should say what the rule is: %s", errOut)
	}
}

func TestUserCLIWillNotStrandAnInstance(t *testing.T) {
	cliDir(t)
	if code, _, errOut := runUser([]string{"add", "root", "--admin"}, "correct-horse-battery"); code != 0 {
		t.Fatalf("add: exit %d, %s", code, errOut)
	}
	code, _, errOut := runUser([]string{"disable", "root"}, "")
	if code == 0 {
		t.Fatal("disabling the only administrator leaves an instance nobody can administer")
	}
	if !strings.Contains(errOut, "only enabled admin") {
		t.Errorf("stderr should say why: %s", errOut)
	}
}

func TestUserCLIListShowsAnInheritedRole(t *testing.T) {
	// "Why is this person an administrator" has to be answerable from the
	// line, exactly as it is on the Accounts page.
	dir := cliDir(t)
	store := users.NewStore(filepath.Join(dir, "users.json"))
	if _, err := store.Add("root", "correct-horse-battery", "admin", false); err != nil {
		t.Fatalf("add root: %v", err)
	}
	if _, err := store.Add("alice", "correct-horse-battery", "user", false); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if err := store.SetGroups("alice", []string{"ops"}); err != nil {
		t.Fatalf("set groups: %v", err)
	}
	if err := store.SetGroupRole("ops", "admin"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	_, out, _ := runUser([]string{"list"}, "")
	if !strings.Contains(out, "admin (via ops)") {
		t.Fatalf("the inherited role and its source should both be shown:\n%s", out)
	}
}

func TestTokenCLIIssueListRevoke(t *testing.T) {
	dir := cliDir(t)
	store := users.NewStore(filepath.Join(dir, "users.json"))
	if _, err := store.Add("alice", "correct-horse-battery", "user", false); err != nil {
		t.Fatalf("add: %v", err)
	}

	code, out, errOut := runToken("add", "alice", "ci pipeline")
	if code != 0 {
		t.Fatalf("issue: exit %d, %s", code, errOut)
	}
	if !strings.Contains(out, apitoken.Prefix+"_") {
		t.Fatalf("the secret should be printed once, and be recognisable:\n%s", out)
	}
	if !strings.Contains(out, "only time the token is shown") {
		t.Errorf("the output should say the secret is not recoverable:\n%s", out)
	}

	// Pull the id back out of the list rather than parsing the secret, which
	// is the shape a person would use.
	code, listed, _ := runToken("list")
	if code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	if !strings.Contains(listed, "alice") || !strings.Contains(listed, "ci pipeline") {
		t.Fatalf("the list should read in terms of people and purposes:\n%s", listed)
	}

	fields := strings.Fields(strings.Split(strings.TrimSpace(listed), "\n")[1])
	id := fields[0]
	if code, _, errOut := runToken("revoke", id); code != 0 {
		t.Fatalf("revoke: exit %d, %s", code, errOut)
	}
	_, listed, _ = runToken("list")
	if !strings.Contains(listed, "revoked") {
		t.Fatalf("a revoked token should say so:\n%s", listed)
	}
}

func TestTokenCLIReadOnly(t *testing.T) {
	dir := cliDir(t)
	store := users.NewStore(filepath.Join(dir, "users.json"))
	if _, err := store.Add("alice", "correct-horse-battery", "user", false); err != nil {
		t.Fatalf("add: %v", err)
	}
	if code, _, errOut := runToken("add", "alice", "watcher", "--read-only"); code != 0 {
		t.Fatalf("issue: exit %d, %s", code, errOut)
	}
	_, listed, _ := runToken("list")
	if strings.Contains(listed, "read+write") {
		t.Fatalf("--read-only must not grant write:\n%s", listed)
	}
	if !strings.Contains(listed, "read") {
		t.Fatalf("the scope should be shown:\n%s", listed)
	}
}

func TestTokenCLIRefusesAnUnknownAccount(t *testing.T) {
	cliDir(t)
	code, _, errOut := runToken("add", "nobody", "ci")
	if code == 0 {
		t.Fatal("a token for an account that does not exist must be refused")
	}
	if !strings.Contains(errOut, "no user named") {
		t.Errorf("stderr should name the account: %s", errOut)
	}
}

func TestTokenCLIRevokeAll(t *testing.T) {
	dir := cliDir(t)
	store := users.NewStore(filepath.Join(dir, "users.json"))
	if _, err := store.Add("alice", "correct-horse-battery", "user", false); err != nil {
		t.Fatalf("add: %v", err)
	}
	for _, name := range []string{"one", "two", "three"} {
		if code, _, errOut := runToken("add", "alice", name); code != 0 {
			t.Fatalf("issue %s: exit %d, %s", name, code, errOut)
		}
	}
	code, out, _ := runToken("revoke-all", "alice")
	if code != 0 {
		t.Fatalf("revoke-all: exit %d", code)
	}
	if !strings.Contains(out, "revoked 3 token(s)") {
		t.Fatalf("got %q, want all three revoked", out)
	}
}

func TestConsoleCommandsPrintUsageWithNoArguments(t *testing.T) {
	cliDir(t)
	for name, run := range map[string]func() (int, string, string){
		"user":  func() (int, string, string) { return runUser(nil, "") },
		"token": func() (int, string, string) { return runToken() },
	} {
		code, _, errOut := run()
		if code != 2 {
			t.Errorf("%s: exit %d, want 2 for a usage error", name, code)
		}
		if !strings.Contains(errOut, "Usage:") {
			t.Errorf("%s: stderr should carry the usage:\n%s", name, errOut)
		}
	}
}
