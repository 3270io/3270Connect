package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/3270io/3270Connect/internal/agent"
)

// catalogue is the skills, instructions and extensions available, loaded
// once.
var (
	catalogueOnce sync.Once
	catalogue     *agent.Catalogue
	loadSession   *agent.LoadSession
)

func skillCatalogue() (*agent.Catalogue, *agent.LoadSession) {
	catalogueOnce.Do(func() {
		catalogue = agent.Load(agent.DirsFor(executableDir()), version)
		loadSession = agent.NewLoadSession()
		if problems := catalogue.Problems(); len(problems) > 0 {
			logMCP("agent catalogue: %d entry/entries could not be loaded", len(problems))
		}
	})
	return catalogue, loadSession
}

// skillTools exposes the playbooks. They are ordinary tools rather than
// something the prompt carries, so the always-on cost is a name and one line
// per skill and the body arrives when it is wanted.
func skillTools() []MCPTool {
	return []MCPTool{
		{
			Name: "list_skills",
			Description: "List the available skills: the procedures this tool knows for load testing, finding a host's " +
				"concurrency limit, soaking, interpreting results and writing workflows. Returns names, one-line " +
				"descriptions and where each came from. Metadata only — call load_skill for the procedure.",
			Tier:        TierRead,
			InputSchema: objectSchema(nil),
			Handle: func(context.Context, map[string]any) (string, error) {
				cat, _ := skillCatalogue()
				skills := cat.Skills()
				out := make([]map[string]any, 0, len(skills))
				for _, s := range skills {
					entry := map[string]any{"name": s.Name, "description": s.Description, "source": s.SourceLabel}
					if len(s.Invocation) > 0 {
						entry["invocation"] = s.Invocation
					}
					if prev, ok := cat.Shadowed(s.Name); ok {
						entry["replaces"] = prev.String()
					}
					out = append(out, entry)
				}
				return toJSON(map[string]any{"count": len(out), "skills": out})
			},
		},
		{
			Name: "load_skill",
			Description: "Load one skill's full procedure, plus the names of the shared instruction fragments it relies on. " +
				"Call it before starting the work a skill covers rather than after improvising it. Loading the same skill " +
				"twice in a conversation returns a short reminder instead of the body.",
			Tier: TierRead,
			InputSchema: objectSchema(map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name from list_skills, e.g. \"ramp-up-load-test\". An invocation alias works too."},
			}, "name"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				cat, session := skillCatalogue()
				name := stringArg(args, "name")
				skill, ok := cat.Skill(name)
				if !ok {
					return "", fmt.Errorf("no skill named %q; call list_skills to see what is available", name)
				}
				if !session.FirstLoad("skill", skill.Name) {
					return fmt.Sprintf("[skill:%s] Already loaded in this conversation — you have it. Do not request it again.", skill.Name), nil
				}
				return toJSON(map[string]any{
					"name": skill.Name, "description": skill.Description, "source": skill.SourceLabel,
					"body": skill.Body(), "instruction_refs": skill.Instructions,
				})
			},
		},
		{
			Name:        "list_instructions",
			Description: "List the shared policy fragments skills cite — load-test safety, reading latency, workflow authoring, and anything this installation added.",
			Tier:        TierRead,
			InputSchema: objectSchema(nil),
			Handle: func(context.Context, map[string]any) (string, error) {
				cat, _ := skillCatalogue()
				instructions := cat.Instructions()
				out := make([]map[string]any, 0, len(instructions))
				for _, i := range instructions {
					out = append(out, map[string]any{"name": i.Name, "title": i.Title, "source": i.SourceLabel})
				}
				return toJSON(map[string]any{"count": len(out), "instructions": out})
			},
		},
		{
			Name:        "load_instruction",
			Description: "Read one shared policy fragment, using a name from a skill's instruction_refs. The .instructions.md suffix is optional.",
			Tier:        TierRead,
			InputSchema: objectSchema(map[string]any{
				"name": map[string]any{"type": "string", "description": "Fragment name, e.g. \"reading-latency\"."},
			}, "name"),
			Handle: func(_ context.Context, args map[string]any) (string, error) {
				cat, session := skillCatalogue()
				name := stringArg(args, "name")
				inst, ok := cat.Instruction(name)
				if !ok {
					return "", fmt.Errorf("no instruction named %q; call list_instructions to see what is available", name)
				}
				if !session.FirstLoad("instruction", inst.Name) {
					return fmt.Sprintf("[instruction:%s] Already loaded in this conversation — you have it.", inst.Name), nil
				}
				return toJSON(map[string]any{
					"name": inst.Name, "title": inst.Title, "source": inst.SourceLabel, "body": inst.Body(),
				})
			},
		},
		{
			Name:        "list_extensions",
			Description: "List the extension packs installed beside 3270Connect, what each contributes and whether it is enabled — including any that failed to load and why. The answer to a skill missing from list_skills.",
			Tier:        TierRead,
			InputSchema: objectSchema(nil),
			Handle: func(context.Context, map[string]any) (string, error) {
				cat, _ := skillCatalogue()
				exts := cat.Extensions()
				out := make([]map[string]any, 0, len(exts))
				for _, e := range exts {
					entry := map[string]any{
						"name": e.Manifest.Name, "version": e.Manifest.Version,
						"enabled": !e.Disabled, "loadable": e.Problem == "",
						"skills":       len(e.Manifest.Contributes.Skills),
						"instructions": len(e.Manifest.Contributes.Instructions),
						"workflows":    len(e.Manifest.Contributes.Workflows),
					}
					if e.Problem != "" {
						entry["problem"] = e.Problem
					}
					out = append(out, entry)
				}
				return toJSON(map[string]any{
					"count": len(out), "extensions": out, "problems": cat.Problems(),
				})
			},
		},
	}
}

// buildMCPServer assembles the server from the tool catalogue.
func buildMCPServer(tier Tier) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "3270Connect", Version: version}, nil)

	for _, tool := range enabledTools(tier) {
		tool := tool
		destructive := tool.Destructive
		openWorld := tool.OpenWorld
		server.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint:    tool.ReadOnly(),
				DestructiveHint: &destructive,
				OpenWorldHint:   &openWorld,
			},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := map[string]any{}
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return mcpError(fmt.Sprintf("arguments are not valid JSON: %v", err)), nil
				}
			}
			out, err := tool.Handle(ctx, args)
			if err != nil {
				// A refusal or a host failure is something the model can act
				// on — a capped concurrency, an unreachable host, an invalid
				// workflow. Returning it as text keeps the reason intact.
				return mcpError(err.Error()), nil
			}
			return mcpText(out), nil
		})
	}

	addPrompt(server, tier)
	return server
}

// addPrompt exposes the operating instructions, including which tier is in
// force — without that a model keeps reaching for tools that are not there
// and reads their absence as a fault.
func addPrompt(server *mcp.Server, tier Tier) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "load_test_3270",
		Description: "How to use 3270Connect for performance and volume testing.",
	}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "Operating instructions for 3270Connect",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: promptText(tier)},
			}},
		}, nil
	})
}

func promptText(tier Tier) string {
	cat, _ := skillCatalogue()

	var b strings.Builder
	b.WriteString("You are driving 3270Connect, which replays scripted 3270 terminal workflows against a mainframe host and measures them.\n\n")
	b.WriteString("A workflow is a JSON document: a host, and ordered steps. The same document runs once as a smoke test or at concurrency as a load test — the difference is how it is started, not what is in it.\n\n")

	b.WriteString("## Skills\n\n")
	b.WriteString("Procedures live in skills, not in this prompt. Call list_skills, then load_skill for the one you need, and load_instruction for the fragments it cites.\n\n")
	for _, s := range cat.Skills() {
		fmt.Fprintf(&b, "- **%s** — %s", s.Name, s.Description)
		if s.Source.Kind != agent.SourceBuiltin {
			fmt.Fprintf(&b, " _(%s)_", s.SourceLabel)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n## Before generating load\n\n")
	b.WriteString("A load test is the one thing here a host cannot ignore. Confirm the hostname with the user before the first concurrent run, validate the workflow, and run it once before running it a hundred times. Ramp up rather than starting at the target.\n\n")

	fmt.Fprintf(&b, "## Tier\n\nThe active tool tier is %q.", tier)
	switch tier {
	case TierRead:
		b.WriteString(" You can describe and validate workflows and read runs already in progress, but cannot open a session or generate load. If the user asks for either, say so — MCP_TOOLS=smoke enables single runs, MCP_TOOLS=load enables concurrency.\n")
	case TierSmoke:
		b.WriteString(" You can run a workflow once against a host, but not at concurrency. Set MCP_TOOLS=load for that.\n")
	default:
		b.WriteString(fmt.Sprintf(" Every tool is available. Concurrency is capped at %d and runtime at %d seconds; both are configurable.\n",
			maxConcurrent(), maxRuntimeSec()))
	}
	return b.String()
}

func mcpText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func mcpError(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

// executableDir is where loose skills, instructions and extensions live —
// beside the binary, the same convention as the .env file. Falls back to the
// working directory when the executable path cannot be resolved.
func executableDir() string {
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}
