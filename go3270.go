package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Workflow struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Login    string `json:"login"`
	Password string `json:"password"`
	Steps    []Step `json:"steps"`
}

type Step struct {
	Action  string `json:"action"`
	Message string `json:"message,omitempty"`
	X       int    `json:"x,omitempty"`
	Y       int    `json:"y,omitempty"`
	Data    string `json:"data,omitempty"`
}

func captureScreen(stdin io.Writer, stdout *bytes.Buffer) string {
	_, err := stdin.Write([]byte("Ascii()\n"))
	if err != nil {
		log.Fatalf("Error writing to stdin: %v", err)
	}
	time.Sleep(1 * time.Second) // Giving some time for x3270 to respond

	screenContent := stdout.String()
	fmt.Println("======= SCREEN CONTENT =======")
	fmt.Println(screenContent)
	fmt.Println("==============================")
	return screenContent
}

func getConnectionState(stdin io.Writer, stdout *bytes.Buffer) (bool, string) {
	_, err := stdin.Write([]byte("Query(ConnectionState)\n"))
	if err != nil {
		log.Fatalf("Error writing to stdin: %v", err)
	}

	// Give the command some time to process and for the output to be available
	time.Sleep(1 * time.Second)

	output := stdout.String()

	// Check if the output contains "ok"
	return strings.Contains(output, "ok"), output
}

func main() {
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

	var workflow Workflow
	if err := json.Unmarshal(data, &workflow); err != nil {
		log.Fatal(err)
	}

	cmd := exec.Command("x3270", "-model", "2", "-script", fmt.Sprintf("%s:%s", workflow.Host, workflow.Port))

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	defer stdin.Close()

	time.Sleep(2 * time.Second)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	defer cmd.Process.Kill() // Ensure x3270 process is killed

	connected, state := getConnectionState(stdin, &out)
	out.Reset() // Reset the buffer after reading
	if !connected {
		log.Fatalf("x3270 is not connected. ConnectionState returned: %s", state)
	}

	htmlFile, err := os.Create("screengrabs.html")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		htmlFile.Sync()
		htmlFile.Close()
	}()

	htmlFile.WriteString("<html><body>")

	//stdin.Write([]byte("Connect(" + workflow.Host + ":" + workflow.Port + ")\n"))
	//time.Sleep(2 * time.Second) // Wait after connecting
	//fmt.Println(out.String())   // Print feedback

	for _, step := range workflow.Steps {
		time.Sleep(2 * time.Second)

		if step.Message != "" {
			fmt.Println(step.Message) // Display the step message
		}

		switch step.Action {
		case "string_found":
			screenContent := captureScreen(stdin, &out) // Capture the current screen
			positionIndex := (step.Y-1)*80 + step.X     // Convert x,y to index in the content, assuming 80 chars per line.
			substring := screenContent[positionIndex : positionIndex+len(step.Data)]

			found := substring == step.Data

			fmt.Printf("Looking for string: '%s' at position (%d, %d). Found: %v\n", step.Data, step.X, step.Y, found)

		case "fill_field":
			cmdStr := fmt.Sprintf("MoveCursor(%d,%d)\n", step.X, step.Y)
			fmt.Println("Command:", cmdStr) // Display the constructed command
			stdin.Write([]byte(cmdStr))
			fmt.Println(out.String())   // Print feedback
			time.Sleep(3 * time.Second) // Increase the delay after moving the cursor

			cmdStr = fmt.Sprintf("String(%s)\n", step.Data)
			fmt.Println("Command:", cmdStr) // Display the constructed command
			stdin.Write([]byte(cmdStr))
			fmt.Println(out.String()) // Print feedback

		case "send_enter":
			_, err := stdin.Write([]byte("Enter\n"))
			if err != nil {
				log.Fatalf("Error writing to stdin: %v", err)
			}
		case "wait":
			fmt.Printf("Waiting for %d seconds...\n", step.X)
		case "screengrab":
			fmt.Println(step.Message)
			screenContent := captureScreen(stdin, &out)
			htmlContent := fmt.Sprintf("<pre>%s</pre><hr/>", html.EscapeString(strings.TrimSpace(screenContent)))
			htmlFile.WriteString(htmlContent)
		}

		screenContent := captureScreen(stdin, &out)
		htmlContent := fmt.Sprintf("<pre>%s</pre><hr/>", html.EscapeString(strings.TrimSpace(screenContent)))
		htmlFile.WriteString(htmlContent)
		out.Reset() // Reset the buffer after reading
	}

	htmlFile.WriteString("</body></html>")
}
