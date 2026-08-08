package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/3270io/3270Connect/connect3270"
)

// The `3270Connect mcp` subcommand: a Model Context Protocol server on stdin
// and stdout, so an AI client can compose a workflow, smoke-test it, run it
// at concurrency, and read the results.

// runMCP is the entry point for `3270Connect mcp`.
//
// It is dispatched before flag.Parse() in main(), which matters twice:
// flag.Parse stops at the first non-flag argument, so `mcp --tools=load`
// would leave these flags unparsed; and everything after it — the banner, the
// "no parameters, forcing dashboard mode" branch, the double-click webview —
// writes to stdout or opens a window.
func runMCP(args []string) {
	// Stdout belongs to the protocol from here on. This binary prints a
	// great deal: a banner, progress bars, status lines. One of them inside
	// a JSON-RPC frame desynchronises the client for the rest of the
	// session. Capturing the real handle and pointing os.Stdout at stderr
	// catches all of it, including the pterm shim, which resolves os.Stdout
	// per call rather than holding a writer.
	realStdout := os.Stdout
	os.Stdout = os.Stderr
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	// A load run's own output would otherwise fight for the terminal.
	connect3270.Headless = true
	connect3270.Verbose = false
	headless = true

	loadEnvFile()

	fs := flag.NewFlagSet("3270Connect mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		listTools = fs.Bool("list-tools", false, "print the enabled tool catalogue as JSON and exit")
		toolsTier = fs.String("tools", "", "tool tier: readonly (default), smoke or load")
	)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, mcpUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	tier := resolveTier(*toolsTier)

	if *listTools {
		if err := writeToolCatalogue(realStdout, tier); err != nil {
			fmt.Fprintf(os.Stderr, "3270Connect mcp: %v\n", err)
			os.Exit(1)
		}
		return
	}

	log.Printf("3270Connect MCP server ready — tools: %s", tier)

	server := buildMCPServer(tier)
	transport := &mcp.IOTransport{
		Reader: os.Stdin,
		// Not StdioTransport: it resolves os.Stdout when it connects, which
		// is now stderr.
		Writer: nopWriteCloser{realStdout},
	}
	if err := server.Run(context.Background(), transport); err != nil {
		fmt.Fprintf(os.Stderr, "3270Connect mcp: %v\n", err)
		os.Exit(1)
	}
}

const mcpUsage = `3270Connect mcp — performance and volume testing from an AI client.

Composes and validates workflows, smoke-tests them against a host, runs them
at concurrency, and reports throughput and latency percentiles.

Tools are gated by tier. The default, readonly, can describe and validate
workflows and read the results of runs already in progress, but cannot
generate load. See MCP_TOOLS below.

Options:
`

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// loadEnvFile applies <binary-dir>/3270Connect.env as defaults.
//
// An AI client launches this process with a command line and no terminal, so
// there is nowhere else to put a setting. It matters most for the RSA token:
// putting a real one in claude_desktop_config.json means it appears in every
// screenshot and support paste of that file.
//
// Real environment variables win, so a deployment can still override the file
// without editing it.
func loadEnvFile() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(exe), "3270Connect.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	applied := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			continue
		}
		if _, alreadySet := os.LookupEnv(key); alreadySet {
			continue
		}
		os.Setenv(key, strings.Trim(strings.TrimSpace(value), `"'`))
		applied++
	}
	if applied > 0 {
		log.Printf("loaded %d setting(s) from %s", applied, path)
	}
}

// writeToolCatalogue prints what the server would expose at a tier — the
// first-run check, needing no host and no run in progress.
func writeToolCatalogue(w io.Writer, tier Tier) error {
	type entry struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Tier        string         `json:"tier"`
		ReadOnly    bool           `json:"readOnly"`
		Destructive bool           `json:"destructive"`
		InputSchema map[string]any `json:"inputSchema"`
	}

	tools := enabledTools(tier)
	out := make([]entry, 0, len(tools))
	for _, t := range tools {
		out = append(out, entry{
			Name:        t.Name,
			Description: t.Description,
			Tier:        t.Tier.String(),
			ReadOnly:    t.ReadOnly(),
			Destructive: t.Destructive,
			InputSchema: t.InputSchema,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
