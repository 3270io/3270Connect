package main

import (
	"bufio"
	"bytes"
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
	defer closeResources(session)

	if err := createInitialHTML(filename); err != nil {
		log.Fatalf("Failed to create initial HTML: %s", err)
	}

	saveScreen(filename, session)

	for _, step := range config.Steps {
		switch step.Action {
		case "string_found":
			verifyString(step.X, step.Y, step.Data, step.Message, session)
		case "fill_field":
			fillField(session.stdin, step.X, step.Y, step.Data)
		case "send_enter":
			sendEnter(session.stdin)
		case "clear":
			clearScreen(session.stdin)
			saveScreen(filename, session)
		case "wait":
			time.Sleep(time.Duration(step.X) * time.Second)
		}
		saveScreen(filename, session)
	}

	appendToFile(filename, "</body></html>")
	sendCmd(session.stdin, "disconnect")
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

	for i := 0; i < maxRetries; i++ {
		// ... (rest of the code remains the same)

		session.lock.Lock()
		sendCmd(session.stdin, "connect "+host+":"+port)
		session.lock.Unlock()

		content, err := getScreenContent(session)
		if err != nil {
			return nil, err
		}

		if strings.Contains(content, "Connected") {
			return session, nil
		}
	}

	return nil, fmt.Errorf("Failed to establish connection after multiple attempts")
}

func verifyString(x, y int, data, message string, session *Session) {
	screenContent := getScreenContent(session)
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
	_, err := stdin.Write([]byte(command + "\n"))
	if err != nil {
		log.Fatalf("Failed to send command: %s", err)
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
	screenContent := getScreenContent(session)
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
