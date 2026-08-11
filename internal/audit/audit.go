// Package audit records who did what, separately from the debug log.
//
// The debug log is for diagnosing the server: it is verbose, it is written by
// whatever code happens to be running, and its format changes whenever a
// message reads badly. Neither property suits the question an audit answers —
// "who pointed two hundred virtual users at that host, and when" — which has
// to survive being asked months later, by somebody who was not there.
//
// So this is a separate file, append-only, one JSON object per line, holding
// only security-relevant events. It is deliberately small: a handful of event
// names, a principal, an address, and a few named details. Recording
// everything would make it another debug log, and the reason to keep the two
// apart is precisely that one of them can be read.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event names what happened. They are constants rather than free text so that
// searching for every login failure is a match on one string, not a guess at
// how each call site phrased it.
type Event string

const (
	EventLoginSucceeded Event = "login.succeeded"
	EventLoginFailed    Event = "login.failed"
	EventLoginLockedOut Event = "login.locked_out"
	EventLogout         Event = "logout"

	EventPasswordChanged Event = "account.password_changed"
	EventAccountCreated  Event = "account.created"
	EventAccountUpdated  Event = "account.updated"
	EventAccountDeleted  Event = "account.deleted"
	EventFirstAdmin      Event = "account.first_admin_created"

	EventTokenIssued  Event = "token.issued"
	EventTokenRevoked Event = "token.revoked"
	EventTokenRefused Event = "token.refused"

	// A role granted to or removed from a group changes what every member may
	// do, so it is recorded with the same weight as changing an account.
	EventGroupRoleSet Event = "group.role_changed"

	// A group's membership decides who inherits the role assigned to it, so
	// maintaining a group is maintaining what its members may do. Each change
	// is recorded with the group as its target.
	EventGroupCreated Event = "group.created"
	EventGroupUpdated Event = "group.updated"
	EventGroupDeleted Event = "group.deleted"

	// Starting a load run is the act this console exists for, and the one with
	// consequences somewhere else: it points a named number of virtual users
	// at a named host, which is traffic somebody else's mainframe has to
	// absorb. Whose run it is, where it was aimed and how hard are exactly
	// what a trail is asked for afterwards.
	EventRunStarted Event = "run.started"
	// Stopping one is recorded because a run can be stopped by somebody other
	// than the person who started it — that is what an administrator is for
	// here — and "who killed my soak test" has to be answerable.
	EventRunKilled Event = "run.killed"
	// A single workflow executed through the REST API or the console. Distinct
	// from a load run: one virtual user, synchronous, no child process. It
	// still reaches a host and still types whatever the workflow says.
	EventWorkflowRun Event = "workflow.executed"
	// Reaching for a host to see whether the port answers. Cheap, and a run of
	// them against addresses that are not this deployment's is what scanning
	// from inside the console would look like.
	EventConnectionTest Event = "connection.tested"
	// A bundled sample application started as a TN3270 host. It binds a port on
	// this machine, so it is a change to what the machine is serving.
	EventSampleAppStarted Event = "sampleapp.started"
)

// Outcome says whether the thing being recorded happened.
//
// A refusal is as much a record as a success — more so, since a run of them is
// what somebody reading this is usually looking for.
type Outcome string

const (
	Success Outcome = "success"
	Failure Outcome = "failure"
	Denied  Outcome = "denied"
)

// Actor is who did it, flattened into the entry rather than referenced by ID.
//
// The username is copied in even though the ID identifies the account: an
// audit read months later must not depend on the account still existing, and
// "deleted account 7f3c" is not an answer to "who did this".
type Actor struct {
	UserID   string `json:"userId,omitempty"`
	Username string `json:"username,omitempty"`
	// Role and Kind record the rights used and the door came in by — a token
	// and a browser are different things to see in a trail.
	Role string `json:"role,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// Entry is one line of the log.
type Entry struct {
	Time     time.Time `json:"time"`
	Event    Event     `json:"event"`
	Outcome  Outcome   `json:"outcome"`
	Actor    Actor     `json:"actor"`
	ClientIP string    `json:"clientIp,omitempty"`
	// Target is what was acted on: a hostname, an account name, a token ID.
	Target string `json:"target,omitempty"`
	// Detail carries a few extra named values. Deliberately not a dumping
	// ground — see Log for what must never go in it.
	Detail map[string]string `json:"detail,omitempty"`
}

// Recorder appends entries to a file.
//
// A nil *Recorder is usable and does nothing, so a call site never has to
// check whether auditing is configured before recording something. Making the
// caller check would mean every one of them is a place the check can be
// forgotten, and a missing audit line is invisible.
type Recorder struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	now     func() time.Time
}

// DefaultMaxSize is when the file is rolled over. One previous generation is
// kept; beyond that, a host that needs long retention needs somewhere to ship
// the lines to, not a bigger file on the same disk.
const DefaultMaxSize = 8 << 20 // 8 MiB

// NewRecorder opens (lazily) the audit log at path.
func NewRecorder(path string) *Recorder {
	return &Recorder{path: path, maxSize: DefaultMaxSize, now: time.Now}
}

// Path reports where entries are written.
func (r *Recorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// Log records an entry.
//
// Never pass a password, a token, a screen's contents or a field's value.
// The point of the file is that an administrator can read it, which means
// anything written here is disclosed to every administrator, for as long as
// the file exists. What belongs here is who, what, where and whether it
// worked.
//
// Failure to write is logged and swallowed. An audit that can refuse a login
// because a disk filled up is a denial-of-service dressed as a safeguard, and
// the alternative — carrying on unrecorded, loudly — is the lesser harm.
func (r *Recorder) Log(entry Entry) {
	if r == nil {
		return
	}
	if entry.Time.IsZero() {
		entry.Time = r.now()
	}
	if entry.Outcome == "" {
		entry.Outcome = Success
	}

	line, err := json.Marshal(entry)
	if err != nil {
		log.Printf("audit: could not encode %s: %v", entry.Event, err)
		return
	}
	line = append(line, '\n')

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.append(line); err != nil {
		log.Printf("audit: could not record %s: %v", entry.Event, err)
	}
}

func (r *Recorder) append(line []byte) error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return fmt.Errorf("create audit dir: %w", err)
	}
	r.rollIfLarge(int64(len(line)))

	// Append-only and owner-only. O_APPEND rather than seek-and-write so
	// concurrent writers — this process and, say, the token CLI run in a
	// container shell — interleave whole lines instead of overwriting each
	// other.
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// rollIfLarge renames the current file aside once it passes the size limit.
func (r *Recorder) rollIfLarge(incoming int64) {
	if r.maxSize <= 0 {
		return
	}
	info, err := os.Stat(r.path)
	if err != nil || info.Size()+incoming <= r.maxSize {
		return
	}
	previous := r.path + ".1"
	_ = os.Remove(previous)
	if err := os.Rename(r.path, previous); err != nil {
		log.Printf("audit: could not roll over %s: %v", r.path, err)
	}
}

// Read returns the most recent entries, newest first.
//
// Both generations are considered, so rolling over does not make the last
// few minutes vanish from a view that asked for the last hundred lines.
// Unparseable lines are skipped rather than failing the read: a truncated
// final line from a killed process must not hide everything before it.
func (r *Recorder) Read(limit int) ([]Entry, error) {
	if r == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var entries []Entry
	// Oldest generation first, so the combined slice is in file order before
	// it is reversed.
	for _, path := range []string{r.path + ".1", r.path} {
		found, err := readEntries(path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, found...)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Time.After(entries[j].Time)
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func readEntries(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	var out []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

// Dump streams the whole log, oldest first, for an administrator
// downloading it. Both generations, so a download is the same set of lines a
// read would have seen.
func (r *Recorder) Dump(w io.Writer) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, path := range []string{r.path + ".1", r.path} {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}
