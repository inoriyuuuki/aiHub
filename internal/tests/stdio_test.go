//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCLIStdioMCP builds the aihub CLI, logs in, and connects to `aihub mcp
// serve` over stdio using the official MCP client, verifying tools/list and a
// read tool call end to end.
func TestCLIStdioMCP(t *testing.T) {
	e := startServer(t)
	e.login(t)

	// Create a read+write token via the session.
	tok, _ := e.do(t, "POST", "/api/v1/tokens", map[string]any{"name": "stdio", "scopes": []any{"read", "mcp"}}, "", 201)
	rawToken := tok["data"].(map[string]any)["token"].(string)

	// Write CLI config.
	configDir := t.TempDir()
	cfg := map[string]any{
		"serverUrl": e.baseURL,
		"username":  "admin",
		"token":     rawToken,
		"scopes":    []string{"read", "mcp"},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), cfgData, 0o600); err != nil {
		t.Fatal(err)
	}

	// Build the CLI binary.
	binPath := filepath.Join(t.TempDir(), "aihub")
	build := exec.Command("go", "build", "-o", binPath, "github.com/aihub/aihub/cmd/aihub")
	build.Env = append(os.Environ(), "GOCACHE="+os.Getenv("GOCACHE"), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build aihub: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath, "mcp", "serve")
	cmd.Env = append(os.Environ(), "AIHUB_CONFIG_DIR="+configDir)
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect stdio: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	if !names["prompts.read"] {
		t.Fatal("prompts.read tool missing")
	}
	if names["prompts.write"] {
		t.Fatal("read-only token must not expose write tools")
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "prompts.read",
		Arguments: map[string]any{"keyword": "问候"},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if text == "" {
		t.Fatal("empty call result")
	}
}
