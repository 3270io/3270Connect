package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

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

	config := loadConfig("config.json")

	connectToHost(config.Host, config.Port)

	saveScreen(filename)

	for _, step := range config.Steps {
		switch step.Action {
		case "string_found":
			verifyString(step.X, step.Y, step.Data, step.Message)
		case "fill_field":
			fillField(step.X, step.Y, step.Data)
		case "send_enter":
			sendEnter()
		case "clear":
			clearScreen()
		case "wait":
			time.Sleep(time.Duration(step.X) * time.Second) // assuming step.X is used as a time delay here
		}

		saveScreen(filename)
	}

	execCmd("s3270", "disconnect")
}

func loadConfig(filename string) Config {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("Failed to read config file: %s", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatalf("Failed to parse config file: %s", err)
	}

	return config
}

func connectToHost(host, port string) {
	execCmd("s3270", host, port)
}

func verifyString(x, y int, data, message string) {
	screenContent := getScreenContent()
	lines := strings.Split(screenContent, "\n")
	if len(lines) <= y {
		log.Fatalf("No such line %d in screen content", y)
	}

	if len(lines[y]) < x+len(data) || lines[y][x:x+len(data)] != data {
		log.Fatalf(message)
	}
}

func fillField(x, y int, data string) {
	execCmd("s3270", fmt.Sprintf("movecursor(%d,%d)", x, y), fmt.Sprintf("string(\"%s\")", data))
}

func sendEnter() {
	execCmd("s3270", "enter")
}

func clearScreen() {
	execCmd("s3270", "clear")
}

func execCmd(name string, arg ...string) string {
	out, err := exec.Command(name, arg...).CombinedOutput()
	if err != nil {
		log.Fatalf("Command [%s %s] failed with error: %s. Output: %s", name, strings.Join(arg, " "), err, string(out))
	}
	return string(out)
}

func saveScreen(filename string) {
	screenContent := execCmd("s3270", "ascii")
	htmlContent := buildHTMLFromScreen(screenContent)

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open file: %s", err)
	}
	defer f.Close()

	if _, err := f.WriteString(htmlContent); err != nil {
		log.Fatalf("Failed to write to file: %s", err)
	}
}

func buildHTMLFromScreen(screenContent string) string {
	html := "<!DOCTYPE html><html><head><meta charset='UTF-8'><style>body { font-family: monospace; white-space: pre; }</style></head><body>"
	html += screenContent
	html += "</body></html>"

	return html
}

func getScreenContent() string {
	// Fetch the screen content
	output := execCmd("s3270", "ascii")
	return output
}
