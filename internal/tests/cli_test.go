//go:build integration

package tests

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aihub/aihub/internal/cli"
)

// testCLIInstaller exercises the Codex adapter end to end against temp dirs:
// skill install/remove/restore and MCP profile install/remove.
func testCLIInstaller(t *testing.T) {
	zipData := zipBytes(map[string]string{
		"SKILL.md":       "---\nname: demo-skill\n---\n# Demo\n",
		"scripts/run.sh": "echo hi\n",
	})
	zipsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData) //nolint:errcheck
	}))
	defer zipsrv.Close()

	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dirs, err := cli.ResolveCodexDirs("global", "")
	if err != nil {
		t.Fatal(err)
	}

	var manifest cli.SkillManifest
	manifest.Skill.Slug = "demo-skill"
	manifest.Skill.Name = "Demo Skill"
	manifest.Version.Version = 1
	manifest.Version.RootDir = ""
	manifest.Source = "global"
	manifest.DownloadURL = zipsrv.URL + "/skill.zip"

	client := &cli.Client{BaseURL: "http://unused", Token: "x", HTTP: zipsrv.Client()}
	if err := cli.InstallSkill(client, dirs, "global", &manifest); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(home, "skills", "demo-skill")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatal("SKILL.md not installed")
	}
	if _, err := os.Stat(filepath.Join(skillDir, ".aihub-managed.json")); err != nil {
		t.Fatal("marker not written")
	}
	// Update should work (managed).
	if err := cli.InstallSkill(client, dirs, "global", &manifest); err != nil {
		t.Fatal(err)
	}
	// Remove + restore.
	if err := cli.RemoveSkill(dirs, "global", "demo-skill"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatal("skill dir should be removed")
	}
	if err := cli.RestoreSkill(dirs, "global", "demo-skill"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skillDir); err != nil {
		t.Fatal("skill dir should be restored")
	}

	// ---- MCP profile ----
	var mcpManifest cli.MCPInstallManifest
	mcpManifest.Profile.Slug = "default"
	mcpManifest.Profile.Name = "默认"
	mcpManifest.ManagedKey = "aihub"
	mcpManifest.MCPServers = []cli.MCPInstallServer{
		{
			Name:         "web-search",
			Type:         "stdio",
			Command:      "npx",
			Args:         []string{"-y", "web-search"},
			Env:          []map[string]any{{"name": "API_KEY", "sensitive": true, "required": true}},
			EnabledTools: []string{"web_search"},
		},
	}
	secrets := &fakeSecrets{values: map[string]string{"API_KEY": "secret123"}}
	if err := cli.InstallProfile(dirs, "global", &mcpManifest, secrets); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "config.toml")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(cfgData)
	if !strings.Contains(cfg, "[mcp_servers.aihub-default-web-search]") {
		t.Fatalf("managed section missing: %s", cfg)
	}
	if !strings.Contains(cfg, "API_KEY = \"secret123\"") {
		t.Fatalf("secret not injected: %s", cfg)
	}
	if !strings.Contains(cfg, "enabled_tools = [\"web_search\"]") {
		t.Fatalf("enabled tools missing: %s", cfg)
	}
	// Marker written.
	if _, err := os.Stat(filepath.Join(home, ".aihub-mcp-managed.json")); err != nil {
		t.Fatal("mcp marker missing")
	}
	// Add an unmanaged section, then re-install -> preserved.
	extra := "\n[model]\nprovider = \"openai\"\n"
	if err := os.WriteFile(cfgPath, append(cfgData, []byte(extra)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cli.InstallProfile(dirs, "global", &mcpManifest, secrets); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfg2), "provider = \"openai\"") {
		t.Fatal("unmanaged section was overwritten")
	}
	// Remove profile.
	if err := cli.RemoveProfile(dirs, "global", "default"); err != nil {
		t.Fatal(err)
	}
	cfg3, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(cfg3), "aihub-default-web-search") {
		t.Fatal("managed section not removed")
	}
	if !strings.Contains(string(cfg3), "provider = \"openai\"") {
		t.Fatal("unmanaged section lost after removal")
	}
}

type fakeSecrets struct {
	values map[string]string
}

func (f *fakeSecrets) Prompt(name, description string) (string, error) {
	return f.values[name], nil
}

func zipBytes(entries map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, _ := zw.Create(name)
		w.Write([]byte(content)) //nolint:errcheck
	}
	zw.Close() //nolint:errcheck
	return buf.Bytes()
}
