package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// InstallExpertPack installs the coordination pack and records its members.
func InstallExpertPack(_ *Client, dirs *CodexDirs, scope string, manifest *ExpertManifest) error {
	if manifest == nil || manifest.Slug == "" {
		return fmt.Errorf("manifest 缺少 slug")
	}
	target := filepath.Join(dirs.SkillsDir(scope), manifest.Slug)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("# %s\n\n协调专家包，包含 %d 个成员:\n", manifest.Name, len(manifest.Manifest.Members))
	for _, m := range manifest.Manifest.Members {
		body += fmt.Sprintf("- %s (%s)\n", m.Name, m.Slug)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(body), 0o644); err != nil {
		return err
	}
	meta, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(target, "manifest.json"), meta, 0o644)
}

// RemoveExpertPack removes an expert pack with backup.
func RemoveExpertPack(dirs *CodexDirs, scope, slug string) error {
	if slug == "" {
		return fmt.Errorf("需要专家包 slug")
	}
	src := filepath.Join(dirs.SkillsDir(scope), slug)
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("专家包 %s 未安装", slug)
		}
		return err
	}
	backup := filepath.Join(backupDir(dirs), "expert-"+slug+"-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	return os.Rename(src, backup)
}
