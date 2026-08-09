package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// InstallSkill installs a skill manifest into the Codex skills directory.
func InstallSkill(_ *Client, dirs *CodexDirs, scope string, manifest *SkillManifest) error {
	if manifest == nil || manifest.Slug == "" {
		return fmt.Errorf("manifest 缺少 slug")
	}
	target := filepath.Join(dirs.SkillsDir(scope), manifest.Slug)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	content := manifest.Content
	if content == "" {
		content = fmt.Sprintf("# %s\n\n%s\n", manifest.Name, manifest.Description)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(content), 0o644); err != nil {
		return err
	}
	meta, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(target, "manifest.json"), meta, 0o644); err != nil {
		return err
	}
	for _, f := range manifest.Files {
		if f.Path == "" || f.Path == "SKILL.md" || f.Path == "manifest.json" {
			continue
		}
		p := filepath.Join(target, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// backupDir is where removed packs are kept before deletion.
func backupDir(dirs *CodexDirs) string {
	return filepath.Join(dirs.base, ".backup")
}

// RemoveSkill moves a skill out of the skills directory into backup.
func RemoveSkill(dirs *CodexDirs, scope, slug string) error {
	if slug == "" {
		return fmt.Errorf("需要 skill slug")
	}
	src := filepath.Join(dirs.SkillsDir(scope), slug)
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("skill %s 未安装", slug)
		}
		return err
	}
	backup := filepath.Join(backupDir(dirs), "skill-"+slug+"-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	return os.Rename(src, backup)
}

// RestoreSkill restores the most recent backup of a skill.
func RestoreSkill(dirs *CodexDirs, scope, slug string) error {
	if slug == "" {
		return fmt.Errorf("需要 skill slug")
	}
	matches, _ := filepath.Glob(filepath.Join(backupDir(dirs), "skill-"+slug+"-*"))
	if len(matches) == 0 {
		return fmt.Errorf("没有 %s 的备份可恢复", slug)
	}
	latest := matches[len(matches)-1] // timestamp suffix sorts lexicographically
	target := filepath.Join(dirs.SkillsDir(scope), slug)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s 已存在，先移除再恢复", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.Rename(latest, target)
}
