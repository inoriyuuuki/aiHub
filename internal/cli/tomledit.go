package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tomlTableName parses "[mcp_servers.foo.bar]" -> ("foo", "bar", true).
// Returns ok=false for non-table lines.
func tomlTableName(line string) (serverName string, subKey string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return "", "", false
	}
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	parts := strings.Split(inner, ".")
	if len(parts) < 2 || parts[0] != "mcp_servers" {
		return "", "", false
	}
	if len(parts) == 2 {
		return parts[1], "", true
	}
	return parts[1], strings.Join(parts[2:], "."), true
}

// mergeManagedTOML inserts managed MCP server sections while preserving all
// other content verbatim. managedSections maps section name (e.g.
// "aihub-profile-def") to the TOML body (without the header). It refuses to
// overwrite a section with the same name that is not in managedNames.
func mergeManagedTOML(existing []byte, managedSections map[string]string) ([]byte, error) {
	lines := strings.Split(string(existing), "\n")
	out := []string{}
	inManaged := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isTable := strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")
		serverName, _, ok := tomlTableName(line)
		isManaged := ok && managedSections[serverName] != ""
		if isTable {
			inManaged = isManaged
		}
		if inManaged {
			// Our managed sections are always re-appended from managedSections,
			// so existing occurrences are skipped (this also handles updates).
			continue
		}
		// Detect conflicts: a section exists with our naming prefix but is not
		// in our managed set -> refuse to overwrite.
		if ok && !isManaged && isAIHubName(serverName) {
			return nil, fmt.Errorf("配置段 [mcp_servers.%s] 已存在但并非由 AIHub 管理，拒绝覆盖", serverName)
		}
		out = append(out, line)
	}
	// Append managed sections (authoritative version).
	for name, body := range managedSections {
		out = append(out, fmt.Sprintf("[mcp_servers.%s]", name))
		for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
			out = append(out, l)
		}
		out = append(out, "")
	}
	return []byte(strings.Join(out, "\n")), nil
}

// isAIHubName reports whether a server section name uses the aihub- prefix.
func isAIHubName(name string) bool {
	return strings.HasPrefix(name, "aihub-")
}

// managedSectionNames returns the section names recorded in a marker file.
func readManagedSections(dir string) ([]string, error) {
	markerPath := filepath.Join(dir, ".aihub-mcp-managed.json")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	if err := jsonUnmarshal(data, &names); err != nil {
		return nil, err
	}
	return names, nil
}

func writeManagedSections(dir string, names []string) error {
	data, err := jsonMarshal(names)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".aihub-mcp-managed.json"), data, 0o644)
}
