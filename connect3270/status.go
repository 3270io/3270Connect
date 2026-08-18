package connect3270

import (
	"strconv"
	"strings"
)

// Status is the s3270/x3270 status line, which the emulator returns with
// every command on the scripting connection.
//
// The line is twelve space-separated fields and is part of the documented
// scripting protocol, so it is stable across emulator versions:
//
//	U F U C(mvs.example.com) I 2 24 80 4 20 0x0 0.000
//	│ │ │ │                  │ │ │  │  │ │  │   └ command execution time
//	│ │ │ │                  │ │ │  │  │ │  └ window id (0x0 for s3270)
//	│ │ │ │                  │ │ │  │  └─┴ cursor row and column, 0-based
//	│ │ │ │                  │ │ └──┴ screen rows and columns
//	│ │ │ │                  │ └ model number
//	│ │ │ │                  └ emulator mode
//	│ │ │ └ connection state, C(host) when connected
//	│ │ └ field protection at the cursor
//	│ └ screen formatting
//	└ keyboard state
//
// Reading it is what lets the emulator know the negotiated geometry, the
// keyboard lock and the connection state without spending a round trip on a
// Query, and it is why a captured screen no longer has a line of protocol
// state pasted onto the bottom of it.
type Status struct {
	// Valid is false for the zero value, i.e. before any command has run.
	Valid bool

	// KeyboardLocked is true when the host has locked the keyboard, either
	// waiting (L) or in an error state (E).
	KeyboardLocked bool
	// KeyboardError is true only for the error lock (X clock / X ^), the
	// state a Reset clears.
	KeyboardError bool

	// Formatted reports whether the screen is field-formatted.
	Formatted bool
	// Protected reports whether the field under the cursor is protected.
	Protected bool

	// Connected reports whether a host session is up, and Host is the name
	// the emulator connected to.
	Connected bool
	Host      string

	// Mode is the emulator mode field: I for 3270, C or L for NVT, P while
	// 3270 negotiation is pending, N when not connected.
	Mode string

	// Model is the 3278/3279 model number, 2 to 5.
	Model int

	// Rows and Cols are the dimensions of the screen the host is currently
	// using. A host that switches to the alternate size mid-session changes
	// these, which is why they are read from every reply rather than cached
	// at connect time.
	Rows, Cols int

	// CursorRow and CursorCol are 1-based, matching how workflow
	// coordinates are written, rather than the 0-based values on the wire.
	CursorRow, CursorCol int

	// Raw is the status line as it arrived.
	Raw string
}

// isStatusLine reports whether a reply line is the status line rather than a
// data line. The scripting protocol answers with zero or more "data:" lines
// followed by exactly one status line, so anything that is not data is it.
func isStatusLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	return !strings.HasPrefix(trimmed, "data:")
}

// ParseStatus reads an s3270 status line. A line that does not have the
// expected shape yields a zero Status, so a future emulator that adds fields
// degrades to "we do not know" rather than to wrong numbers.
func ParseStatus(line string) Status {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 10 {
		return Status{}
	}

	s := Status{Valid: true, Raw: strings.TrimSpace(line)}

	switch fields[0] {
	case "L":
		s.KeyboardLocked = true
	case "E":
		s.KeyboardLocked = true
		s.KeyboardError = true
	}
	s.Formatted = fields[1] == "F"
	s.Protected = fields[2] == "P"

	if conn := fields[3]; conn != "N" {
		s.Connected = true
		if open := strings.IndexByte(conn, '('); open >= 0 && strings.HasSuffix(conn, ")") {
			s.Host = conn[open+1 : len(conn)-1]
		}
	}
	s.Mode = fields[4]

	s.Model, _ = strconv.Atoi(fields[5])
	s.Rows, _ = strconv.Atoi(fields[6])
	s.Cols, _ = strconv.Atoi(fields[7])
	if row, err := strconv.Atoi(fields[8]); err == nil {
		s.CursorRow = row + 1
	}
	if col, err := strconv.Atoi(fields[9]); err == nil {
		s.CursorCol = col + 1
	}

	return s
}

// quoteActionArg renders a value as a quoted argument to an s3270 action.
//
// This is not cosmetic. An action argument is parsed, so an unquoted value
// loses every comma in it — the argument separator — and a value holding a
// bracket is a syntax error rather than a typed character. "SMITH,JOHN" was
// typed onto the host screen as "SMITHJOHN", silently and with no error to
// report, which is the sort of thing that is only ever noticed downstream.
//
// Inside double quotes the emulator takes the value literally, with
// backslash and quote escaped. Control characters are escaped rather than
// passed through: a bare newline would end the command line and turn the
// rest of the value into a second, arbitrary action.
func quoteActionArg(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// connectionStateIsConnected reads the answer to Query(ConnectionState).
//
// The emulator answers "not-connected" when idle, and it is the answer that
// mattered: the previous check treated any non-empty reply as connected, so
// a session that never reached the host reported itself connected and the
// connect retry loop had nothing to retry. Everything downstream then failed
// one step at a time instead.
//
// States are "not-connected", "reconnecting", "resolving", "tcp-pending",
// "tls-pending", "proxy-pending", "telnet-pending", and the "connected-*"
// family, of which "connected-unbound" is a TN3270E session that has not yet
// been bound to an LU — connected, but not yet ready to drive.
func connectionStateIsConnected(state string) bool {
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		return false
	}
	return strings.HasPrefix(state, "connected")
}
