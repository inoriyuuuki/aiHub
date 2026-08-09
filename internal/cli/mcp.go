package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MCPInstallManifest is the Codex install manifest from the server.
type MCPInstallManifest struct {
	Profile struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		Description string `json:"description"`
	} `json:"profile"`
	ManagedKey    string             `json:"managedKey"`
	MCPServers    []MCPInstallServer `json:"mcpServers"`
	EnabledTools  []string           `json:"enabledTools"`
	DisabledTools []string           `json:"disabledTools"`
}

// MCPInstallServer is one MCP server entry to write to Codex config.
type MCPInstallServer struct {
	Name          string           `json:"name"`
	Type          string           `json:"type"`
	Command       string           `json:"command,omitempty"`
	Args          []string         `json:"args,omitempty"`
	URL           string           `json:"url,omitempty"`
	Workdir       string           `json:"workdir,omitempty"`
	Env           []map[string]any `json:"env,omitempty"`
	EnabledTools  []string         `json:"enabledTools,omitempty"`
	DisabledTools []string         `json:"disabledTools,omitempty"`
}

// SecretSource allows prompting or reading env values without storing secrets.
type SecretSource interface {
	// Prompt asks the user for a value.
	Prompt(name, description string) (string, error)
}

// StdinSecretSource prompts on stdin.
type StdinSecretSource struct{}

func (StdinSecretSource) Prompt(name, description string) (string, error) {
	if description != "" {
		fmt.Printf("%s（%s）: ", name, description)
	} else {
		fmt.Printf("%s: ", name)
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// InstallProfile writes all MCP servers of a profile into Codex config,
// preserving unmanaged sections and refusing to overwrite same-name
// non-managed sections.
func InstallProfile(dirs *CodexDirs, scope string, manifest *MCPInstallManifest, secrets SecretSource) error {
	configFile := dirs.ConfigFile(scope)
	codexDir := filepath.Dir(configFile)
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return err
	}
	managed, err := readManagedSections(codexDir)
	if err != nil {
		return err
	}
	managedSet := map[string]bool{}
	for _, n := range managed {
		managedSet[n] = true
	}

	// Build new sections.
	sections := map[string]string{}
	var newNames []string
	for _, srv := range manifest.MCPServers {
		name := fmt.Sprintf("aihub-%s-%s", manifest.Profile.Slug, srv.Name)
		if managedSet[name] {
			// Re-install/update: allowed, we manage it.
		} else if existingSection(configFile, name) {
			return fmt.Errorf("配置段 [mcp_servers.%s] 已存在且未被 AIHub 管理，拒绝覆盖", name)
		}
		body, err := renderServerTOML(srv, secrets)
		if err != nil {
			return err
		}
		sections[name] = body
		newNames = append(newNames, name)
	}

	existing, err := os.ReadFile(configFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	merged, err := mergeManagedTOML(existing, sections)
	if err != nil {
		return err
	}
	// Backup before writing.
	if len(existing) > 0 {
		backupDir := dirs.BackupsDir(scope)
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return err
		}
		backup := filepath.Join(backupDir, fmt.Sprintf("config-%d.toml", time.Now().Unix()))
		if err := os.WriteFile(backup, existing, 0o644); err != nil {
			return err
		}
	}
	// Atomic write.
	tmp := configFile + ".tmp"
	if err := os.WriteFile(tmp, merged, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, configFile); err != nil {
		return err
	}
	// Update marker.
	all := append([]string{}, managed...)
	for _, n := range newNames {
		if !managedSet[n] {
			all = append(all, n)
		}
	}
	return writeManagedSections(codexDir, all)
}

// RemoveProfile removes all sections of a profile from Codex config.
func RemoveProfile(dirs *CodexDirs, scope, profileSlug string) error {
	configFile := dirs.ConfigFile(scope)
	codexDir := filepath.Dir(configFile)
	managed, err := readManagedSections(codexDir)
	if err != nil {
		return err
	}
	prefix := "aihub-" + profileSlug + "-"
	removed := map[string]bool{}
	remaining := []string{}
	for _, n := range managed {
		if strings.HasPrefix(n, prefix) {
			removed[n] = true
		} else {
			remaining = append(remaining, n)
		}
	}
	if len(removed) == 0 {
		return fmt.Errorf("Profile %s 未安装", profileSlug)
	}
	existing, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}
	lines := strings.Split(string(existing), "\n")
	out := []string{}
	skip := false
	for _, line := range lines {
		name, _, ok := tomlTableName(line)
		if ok {
			skip = removed[name]
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	if err := os.WriteFile(configFile, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return err
	}
	return writeManagedSections(codexDir, remaining)
}

// renderServerTOML renders the body of a [mcp_servers.name] section.
func renderServerTOML(srv MCPInstallServer, secrets SecretSource) (string, error) {
	var b strings.Builder
	if srv.Command != "" {
		b.WriteString(fmt.Sprintf("command = %q\n", srv.Command))
		if len(srv.Args) > 0 {
			parts := make([]string, 0, len(srv.Args))
			for _, a := range srv.Args {
				parts = append(parts, fmt.Sprintf("%q", a))
			}
			b.WriteString("args = [" + strings.Join(parts, ", ") + "]\n")
		}
	}
	if srv.URL != "" {
		b.WriteString(fmt.Sprintf("url = %q\n", srv.URL))
	}
	if srv.Workdir != "" {
		b.WriteString(fmt.Sprintf("workdir = %q\n", srv.Workdir))
	}
	if len(srv.Env) > 0 {
		entries := []string{}
		for _, ev := range srv.Env {
			name, _ := ev["name"].(string)
			if name == "" {
				continue
			}
			desc, _ := ev["description"].(string)
			sensitive, _ := ev["sensitive"].(bool)
			required, _ := ev["required"].(bool)
			def, _ := ev["default"].(string)
			value := def
			if sensitive || required {
				if secrets == nil {
					secrets = StdinSecretSource{}
				}
				v, err := secrets.Prompt(name, desc)
				if err != nil {
					return "", err
				}
				value = v
			}
			entries = append(entries, fmt.Sprintf("%s = %q", tomlKey(name), value))
		}
		if len(entries) > 0 {
			b.WriteString("env = { " + strings.Join(entries, ", ") + " }\n")
		}
	}
	if len(srv.EnabledTools) > 0 {
		parts := make([]string, 0, len(srv.EnabledTools))
		for _, t := range srv.EnabledTools {
			parts = append(parts, fmt.Sprintf("%q", t))
		}
		b.WriteString("enabled_tools = [" + strings.Join(parts, ", ") + "]\n")
	}
	if len(srv.DisabledTools) > 0 {
		parts := make([]string, 0, len(srv.DisabledTools))
		for _, t := range srv.DisabledTools {
			parts = append(parts, fmt.Sprintf("%q", t))
		}
		b.WriteString("disabled_tools = [" + strings.Join(parts, ", ") + "]\n")
	}
	return b.String(), nil
}

func tomlKey(name string) string {
	if name == "" {
		return "KEY"
	}
	return name
}

// existingSection reports whether configFile contains [mcp_servers.name].
func existingSection(configFile, name string) bool {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		serverName, _, ok := tomlTableName(line)
		if ok && serverName == name {
			return true
		}
	}
	return false
}
