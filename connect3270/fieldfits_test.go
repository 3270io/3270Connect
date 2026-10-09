package connect3270

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
)

// fakeScriptHost answers every command with the given data lines followed by
// a formatted-screen status line, the way s3270's script port does.
func fakeScriptHost(t *testing.T, dataLines ...string) *Emulator {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for {
			if _, err := r.ReadString('\n'); err != nil {
				return
			}
			for _, l := range dataLines {
				conn.Write([]byte("data: " + l + "\n"))
			}
			conn.Write([]byte("U F U C(127.0.0.1) I 2 24 80 4 20 0x0 0.000\nok\n"))
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	e := &Emulator{ScriptPort: port}
	t.Cleanup(e.closeScriptConn)
	return e
}

// An over-long value would run on into the next field, which on a logon
// screen is the password. The boundary is the field length exactly.
func TestCheckFieldFitsRefusesOnlyWhatOverflows(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty value", "", false},
		{"shorter than the field", "SMITH", false},
		{"exactly the field length", "ABCDEFGH", false},
		{"one character too many", "ABCDEFGHI", true},
		{"counts characters, not bytes", "ÄÖÅÄÖÅÄÖ", false},
		{"tab moves between fields on purpose", "ABCDEFGHIJKLMNOP\tX", false},
		{"newline moves between fields on purpose", "ABCDEFGHIJKLMNOP\nX", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fakeScriptHost(t, "        ")
			err := e.checkFieldFits(tc.value)
			if tc.wantErr != (err != nil) {
				t.Fatalf("checkFieldFits(%q) error = %v, want error: %v", tc.value, err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "overflow") {
				t.Errorf("error should say what would happen, got: %v", err)
			}
		})
	}
}
