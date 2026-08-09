package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// CodexDirs holds resolved Codex configuration directories.
type CodexDirs struct {
	// Global is the directory holding global MCP profiles.
	Global string
	// Project is the resolved project root; empty for global scope.
	Project string
	base    string
}

// ResolveCodexDirs returns the Codex directories for a scope
// ("global" or "project").
func ResolveCodexDirs(scope, projectDir string) (*CodexDirs, error) {
	switch scope {
	case "global":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base := filepath.Join(home, ".codex")
		return &CodexDirs{
			base:    base,
			Global:  filepath.Join(base, "mcp"),
			Project: "",
		}, nil
	case "project":
		root, err := findProjectRoot(projectDir)
		if err != nil {
			// Return a non-nil struct so callers that ignore the error
			// can still check fields (e.g. Project == "").
			return &CodexDirs{}, err
		}
		base := filepath.Join(root, ".codex")
		return &CodexDirs{
			base:    base,
			Global:  filepath.Join(base, "mcp"),
			Project: root,
		}, nil
	default:
		return &CodexDirs{}, fmt.Errorf("未知 scope %q（可用 global|project）", scope)
	}
}

// SkillsDir returns the skills directory for a scope.
func (d *CodexDirs) SkillsDir(scope string) string {
	return filepath.Join(d.base, "skills")
}

// findProjectRoot walks upward from start looking for a project marker
// (.codex or go.mod).
func findProjectRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".codex")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("在 %s 或其父目录中找不到项目根（.codex 或 go.mod）", start)
		}
		dir = parent
	}
}
