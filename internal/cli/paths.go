package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// CodexDirs resolves global and project Codex directories.
type CodexDirs struct {
	// Global is the user-level Codex directory (default ~/.codex).
	Global string
	// Project is the project-level .codex directory, or "" when not in a project.
	Project string
}

// ResolveCodexDirs determines the Codex directories. When scope is "project",
// projectDir must be set. CODEX_HOME overrides the global dir.
func ResolveCodexDirs(scope, projectDir string) (*CodexDirs, error) {
	global := os.Getenv("CODEX_HOME")
	if global == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		global = filepath.Join(home, ".codex")
	}
	d := &CodexDirs{Global: global}
	if scope == "project" {
		if projectDir == "" {
			return nil, fmt.Errorf("项目级安装需要 --dir 参数指定项目根目录")
		}
		d.Project = filepath.Join(projectDir, ".codex")
	}
	return d, nil
}

// SkillsDir returns the skill directory for a scope.
func (d *CodexDirs) SkillsDir(scope string) string {
	if scope == "project" {
		return filepath.Join(d.Project, "skills")
	}
	return filepath.Join(d.Global, "skills")
}

// ConfigFile returns the config.toml path for a scope.
func (d *CodexDirs) ConfigFile(scope string) string {
	if scope == "project" {
		return filepath.Join(d.Project, "config.toml")
	}
	return filepath.Join(d.Global, "config.toml")
}

// BackupsDir returns the backup root for a scope.
func (d *CodexDirs) BackupsDir(scope string) string {
	root := d.Global
	if scope == "project" {
		root = d.Project
	}
	return filepath.Join(root, ".aihub-backups")
}
