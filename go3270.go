package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxRetries = 3
const retryInterval = 2 * time.Second

type Session struct {
	lock   sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

type Config struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Login    string `json:"login"`
	Password string `json:"password"`
	Steps    []Step `json:"steps"`
}

type Step struct {
	Action  string `json:"action"`
	Message string `json:"message"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Data    string `json:"data"`
}

func main() {
	filename := "App_Functional_Test_Results_" + time.Now().Format("20060102-150405") + ".html"

	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Error loading config: %s", err)
	}

	session, err := startScriptingSession(config.Host, config.Port)
	if err != nil {
		log.Fatalf("Error starting scripting session: %s", err)
	}
	if session == nil {
		log.Fatalf("Session is nil")
	}
	defer closeResources(session)

	if err := createInitialHTML(filename); err != nil {
		log.Fatalf("Failed to create initial HTML: %s", err)
	}

	saveScreen(filename, session)

	for _, step := range config.Steps {
		log.Printf("Executing step: %s", step.Action)
		switch step.Action {
		case "string_found":
			verifyString(step.X, step.Y, step.Data, step.Message, session)
		case "fill_field":
			log.Printf("Filling field at X: %d, Y: %d with data: %s", step.X, step.Y, step.Data)
			fillField(session.stdin, step.X, step.Y, step.Data)
		case "send_enter":
			log.Println("Sending Enter key")
			sendEnter(session.stdin)
		case "clear":
			log.Println("Clearing screen")
			clearScreen(session.stdin)
			saveScreen(filename, session)
		case "wait":
			log.Printf("Waiting for %d seconds", step.X)
			time.Sleep(time.Duration(step.X) * time.Second)
		}
		saveScreen(filename, session)
		log.Printf("Step completed: %s", step.Action)
	}

	appendToFile(filename, "</body></html>")
	sendCmd(session.stdin, "disconnect")
	log.Println("Script execution completed")
}

func loadConfig(filename string) (Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("Failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("Failed to parse config file: %w", err)
	}

	return config, nil
}

func startScriptingSession(host, port string) (*Session, error) {
	session := &Session{}
	timeout := 60 * time.Second

	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "s3270", "-script")

		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("Failed to get stdin pipe: %w", err)
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("Failed to get stdout pipe: %w", err)
		}

		if err := cmd.Start(); err != nil {
			log.Printf("Attempt %d: Failed to start s3270 in scripting mode: %s", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}

		session.cmd = cmd
		session.stdin = stdin
		session.stdout = stdout

		log.Printf("Attempt %d: s3270 process started", i+1)

		// Add a delay to allow s3270 to initialize
		time.Sleep(5 * time.Second)

		session.lock.Lock()
		sendCmd(session.stdin, "connect "+host+":"+port)
		session.lock.Unlock()

		content, err := getScreenContent(session)
		if err != nil {
			return nil, err
		}
		log.Printf("Attempt %d: s3270 process ended", i+1)
		if strings.Contains(content, "Connected") {
			log.Printf("Attempt %d: Connection established", i+1)
			return session, nil
		}

		// If the connection was not successful, close the session and retry
		session.stdin.Close()
		session.stdout.Close()
		if session.cmd.Process != nil {
			session.cmd.Process.Kill()
		}
		session.cmd.Wait()

		log.Printf("Attempt %d: Connection failed, retrying...", i+1)
	}

	return nil, fmt.Errorf("Failed to establish connection after multiple attempts")
}

func verifyString(x, y int, data, message string, session *Session) {
	screenContent, err := getScreenContent(session)
	if err != nil {
		log.Fatalf("Failed getScreenContent: %s", err)
	}
	lines := strings.Split(screenContent, "\n")
	if len(lines) <= y {
		log.Fatalf("No such line %d in screen content", y)
	}

	if len(lines[y]) < x+len(data) || lines[y][x:x+len(data)] != data {
		log.Fatalf(message)
	}
}

func fillField(stdin io.WriteCloser, x, y int, data string) {
	sendCmd(stdin, fmt.Sprintf("movecursor(%d,%d)", x, y))
	sendCmd(stdin, fmt.Sprintf("string(\"%s\")", data))
}

func sendEnter(stdin io.WriteCloser) {
	sendCmd(stdin, "enter")
}

func clearScreen(stdin io.WriteCloser) {
	sendCmd(stdin, "clear")
}

func sendCmd(stdin io.WriteCloser, command string) {
	log.Printf("Sending command: %s", command)
	_, err := stdin.Write([]byte(command + "\n"))
	if err != nil {
		log.Fatalf("Failed to send command via stdin method: %s", err)
	}

	cmd := exec.Command("x3270if", "exec", "ascii("+command+")")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("Failed to execute command: %s\nOutput: %s", err, out)
	} else {
		log.Printf("Command output:\n%s", out)
	}
}

func createInitialHTML(filename string) error {
	header := `<!DOCTYPE html>
<html>
<head>
    <meta charset='UTF-8'>
    <style>body { font-family: monospace; white-space: pre; }</style>
</head>
<body>
`
	err := ioutil.WriteFile(filename, []byte(header), 0644)
	if err != nil {
		return fmt.Errorf("Failed to create initial HTML: %w", err)
	}
	return nil
}

func buildHTMLFromScreen(screenContent string) string {
	// Instead of a full HTML document, just wrap each screen in a div for clarity.
	return "<div>" + screenContent + "</div><hr>"
}

func appendToFile(filename, content string) error {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("Failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("Failed to write to file: %w", err)
	}

	return nil
}

func saveScreen(filename string, session *Session) {
	screenContent, err := getScreenContent(session)
	if err != nil {
		log.Fatalf("Failed getScreenContent: %s", err)
	}
	htmlContent := buildHTMLFromScreen(screenContent)
	appendToFile(filename, htmlContent)
}

func getScreenContent(session *Session) (string, error) {
	var cmdOutput bytes.Buffer

	session.lock.Lock()
	defer session.lock.Unlock()

	sendCmd(session.stdin, "ascii")

	scanner := bufio.NewScanner(session.stdout)
	for scanner.Scan() {
		cmdOutput.WriteString(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("Reading from stdout failed: %s", err)
	}

	return cmdOutput.String(), nil
}

func closeResources(session *Session) {
	session.stdin.Close()
	session.stdout.Close()
	if session.cmd.Process != nil {
		session.cmd.Process.Kill()
	}
	session.cmd.Wait()
}
