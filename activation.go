package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/3270io/3270Connect/internal/audit"
	"golang.org/x/term"
)

func plainTerminal() bool {
	return *plainOutput || !term.IsTerminal(int(os.Stdout.Fd())) || os.Getenv("NO_COLOR") != ""
}
func compactTerminal() bool {
	if plainTerminal() {
		return true
	}
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	return err == nil && width < 120
}
func workflowExitCode(failed, connections int64, interrupted bool) int {
	if interrupted {
		return 130
	}
	if connections > 0 {
		return 3
	}
	if failed > 0 {
		return 4
	}
	return 0
}
func printCLIHelp() {
	command := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stdout, `3270Connect — replay IBM 3270 workflows

Start here:
  %[1]s -sampleWorkflow sample-workflow.json   Create a runnable example
  %[1]s -config sample-workflow.json -headless Replay one workflow
  %[1]s -dashboard                            Open the operations console

Tasks:
  -config FILE -headless                    Replay once; exits after summary
  -config FILE -concurrent 5 -runtime 60     Run a bounded load test
  -runApp 1 -runApp-port 3270                Start the bundled sample host
  -profile -profileHost HOST -profilePort PORT  Check a terminal host
  -api                                     Serve the HTTP workflow API
  user help | token help | mcp -help        Account, token and MCP commands

Presentation:
  -plain              Plain logs; no interactive shutdown prompt
  -keepDashboard      Keep the console open after a run, in a terminal
  -help-all           Full flag reference
  -version            Print the version

Exit codes: 0 success, 1 startup error, 2 configuration/usage error,
            3 host connection failed, 4 workflow failed, 130 interrupted.
A load run exits by default. -headless selects the emulator, not log verbosity.
Documentation: https://3270connect.3270.io/basic-usage/
`, command)
	if *helpAll {
		flag.CommandLine.SetOutput(os.Stdout)
		flag.PrintDefaults()
	}
}

// The built-in example is deliberately separate from the external lab host.
func sampleConfiguration(host string, port int) map[string]interface{} {
	return map[string]interface{}{"Host": host, "Port": port, "OutputFilePath": fmt.Sprintf("sample-output-%d.html", port), "WaitForField": true, "Steps": []interface{}{
		map[string]interface{}{"Type": "Connect"},
		map[string]interface{}{"Type": "CheckValue", "Coordinates": map[string]int{"Row": 1, "Column": 29, "Length": 24}, "Text": "3270 Example Application"},
		map[string]interface{}{"Type": "AsciiScreenGrab"},
		map[string]interface{}{"Type": "FillString", "Coordinates": map[string]int{"Row": 5, "Column": 21}, "Text": "Sample"},
		map[string]interface{}{"Type": "FillString", "Coordinates": map[string]int{"Row": 6, "Column": 21}, "Text": "Replay"},
		map[string]interface{}{"Type": "PressEnter"},
		map[string]interface{}{"Type": "CheckValue", "Coordinates": map[string]int{"Row": 3, "Column": 2, "Length": len("Thank you for submitting your name.")}, "Text": "Thank you for submitting your name."},
		map[string]interface{}{"Type": "AsciiScreenGrab"}, map[string]interface{}{"Type": "Disconnect"},
	}}
}
func writeSampleWorkflow(path string) error {
	data, err := json.MarshalIndent(sampleConfiguration("127.0.0.1", 3270), "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("sample file: %w (choose a new filename)", err)
	}
	if _, err = file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Created %s\nStart the host in another terminal:\n  %s -runApp 1 -runApp-port 3270\nThen replay once:\n  %s -config %q -headless\n", path, filepath.Base(os.Args[0]), filepath.Base(os.Args[0]), path)
	return nil
}

var managedDone sync.Map

// Start is synchronous; only Wait runs in the background. The returned PID is real.
func startOwnedProcess(args []string, owner audit.Actor) (*exec.Cmd, error) {
	cmd := exec.Command(getExecutablePath(), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	auth.runs.claim(cmd.Process.Pid, owner.UserID, owner.Username)
	done := make(chan struct{})
	managedDone.Store(cmd.Process.Pid, done)
	go func() {
		defer close(done)
		if err := cmd.Wait(); err != nil {
			storeLog(fmt.Sprintf("Process %d exited: %v", cmd.Process.Pid, err))
		}
	}()
	return cmd, nil
}
func writeLaunchResult(w http.ResponseWriter, pid int) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"pid": pid, "status": "starting"})
}
func waitSampleReady(cmd *exec.Cmd, port string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if done, ok := managedDone.Load(cmd.Process.Pid); ok {
			select {
			case <-done.(chan struct{}):
				return fmt.Errorf("sample host exited before becoming ready; check console logs")
			default:
			}
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return fmt.Errorf("sample host did not become ready; check console logs")
}
func decodeWorkflowRequest(w http.ResponseWriter, r *http.Request) (*Configuration, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("POST required")
	}
	var c Configuration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&c); err != nil {
		return nil, fmt.Errorf("invalid workflow JSON: %w", err)
	}
	if err := validateConfiguration(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
func validateWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	c, err := decodeWorkflowRequest(w, r)
	if err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"valid": true, "steps": len(c.Steps)})
}

var sampleMu sync.Mutex

func sampleWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, 405, "POST required")
		return
	}
	sampleMu.Lock()
	defer sampleMu.Unlock()
	// Lab deployments already mount a workflow aimed at their own sample host.
	if data, err := os.ReadFile("workflow-sampleapp.json"); err == nil {
		var c Configuration
		if json.Unmarshal(data, &c) == nil && c.Host == "sampleapps" && validateConfiguration(&c) == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"workflow": c, "name": "workflow-sampleapp.json", "hostReady": false})
			return
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd, err := startOwnedProcess([]string{"-runApp", "1", "-runApp-port", strconv.Itoa(port)}, auditActor(r))
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return
	}
	if err := waitSampleReady(cmd, strconv.Itoa(port)); err != nil {
		writeJSONError(w, 502, err.Error())
		return
	}
	auth.auditRequest(r, audit.EventSampleAppStarted, audit.Success, "app1", map[string]string{"port": strconv.Itoa(port)})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"workflow": sampleConfiguration("127.0.0.1", port), "name": "sample-workflow.json", "pid": cmd.Process.Pid, "hostReady": true})
}
func preflightWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	c, err := decodeWorkflowRequest(w, r)
	if err != nil {
		writeJSONError(w, 400, err.Error())
		return
	}
	file, err := os.CreateTemp("", "3270connect-preflight-*.json")
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(c)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		writeJSONError(w, 500, "Could not prepare terminal check")
		return
	}
	// Isolate emulator/global state from the running console and bound its lifetime.
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	args := []string{"-profile", "-headless", "-config", file.Name(), "-model", c.Model, "-oversize", c.Oversize, "-luName", c.LUName, "-codePage", c.CodePage}
	if c.TLS {
		args = append(args, "-profileTLS")
	}
	if c.TLSSkipVerify {
		args = append(args, "-tlsSkipVerify")
	}
	cmd := exec.CommandContext(ctx, getExecutablePath(), args...)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		writeJSONError(w, 500, "Could not allocate terminal check port")
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cmd.Env = append(os.Environ(), "AUTH_MODE=none", "PROFILE_SCRIPT_PORT="+strconv.Itoa(port))
	output, err := cmd.Output()
	if err != nil {
		writeJSONError(w, 502, "Terminal check failed. Check the host, TLS and terminal settings, then inspect console logs.")
		return
	}
	var profile interface{}
	if json.Unmarshal(output, &profile) != nil {
		writeJSONError(w, 502, "Terminal check returned an invalid result")
		return
	}
	auth.auditRequest(r, audit.EventConnectionTest, audit.Success, net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), nil)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Terminal negotiation succeeded. Login and workflow assertions have not been tested.", "profile": profile})
}
