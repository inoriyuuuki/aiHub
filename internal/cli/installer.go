package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// managedMarker is written into each skill directory we manage.
type managedMarker struct {
	Slug        string    `json:"slug"`
	Version     int       `json:"version"`
	SHA256      string    `json:"sha256"`
	Source      string    `json:"source"`
	InstalledAt time.Time `json:"installedAt"`
}

// SkillManifest is the install manifest returned by the server.
type SkillManifest struct {
	Skill struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Status string `json:"status"`
	} `json:"skill"`
	Version struct {
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
		RootDir string `json:"rootDir"`
	} `json:"version"`
	Source      string `json:"source"`
	DownloadURL string `json:"downloadUrl"`
}

// InstallSkill downloads and installs a skill, atomically replacing any
// previous managed version (backing it up first).
func InstallSkill(client *Client, dirs *CodexDirs, scope string, manifest *SkillManifest) error {
	dest := filepath.Join(dirs.SkillsDir(scope), manifest.Skill.Slug)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	// Refuse to overwrite an unmanaged directory.
	if _, err := os.Stat(dest); err == nil {
		if _, err := os.Stat(filepath.Join(dest, ".aihub-managed.json")); err != nil {
			return fmt.Errorf("目标目录 %s 已存在且不是 AIHub 管理的，拒绝覆盖", dest)
		}
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".aihub-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck

	zipPath := filepath.Join(tmp, "skill.zip")
	if err := DownloadToFile(manifest.DownloadURL, zipPath); err != nil {
		return err
	}
	if err := extractZipTo(zipPath, tmp); err != nil {
		return err
	}
	// Compute the package sha for the marker before removing the zip.
	sha, err := sha256File(zipPath)
	if err != nil {
		return err
	}
	_ = os.Remove(zipPath) // the zip is not part of the installed skill
	// Normalize: content may live under a root dir.
	root := filepath.Join(tmp, manifest.Version.RootDir)
	if _, err := os.Stat(root); err != nil {
		root = tmp
	}
	marker := managedMarker{
		Slug: manifest.Skill.Slug, Version: manifest.Version.Version,
		SHA256: sha, Source: manifest.Source, InstalledAt: time.Now().UTC(),
	}
	markerData, _ := json.MarshalIndent(marker, "", "  ")

	// Backup any existing managed directory.
	if _, err := os.Stat(dest); err == nil {
		backup := filepath.Join(dirs.BackupsDir(scope), fmt.Sprintf("%s-%d", manifest.Skill.Slug, time.Now().Unix()))
		if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return err
		}
		if err := os.Rename(dest, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(filepath.Join(tmp, "."), dest); err != nil {
		// If rename of directory fails (cross-device), fall back to copy.
		if err := copyDir(tmp, dest); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dest, ".aihub-managed.json"), markerData, 0o644); err != nil {
		return err
	}
	_ = root
	return nil
}

// RemoveSkill backs up and removes a managed skill directory.
func RemoveSkill(dirs *CodexDirs, scope, slug string) error {
	dest := filepath.Join(dirs.SkillsDir(scope), slug)
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("Skill %s 未安装", slug)
	}
	if _, err := os.Stat(filepath.Join(dest, ".aihub-managed.json")); err != nil {
		return fmt.Errorf("目录 %s 不是 AIHub 管理的，拒绝删除", dest)
	}
	backup := filepath.Join(dirs.BackupsDir(scope), fmt.Sprintf("%s-remove-%d", slug, time.Now().Unix()))
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	if err := os.Rename(dest, backup); err != nil {
		return err
	}
	return nil
}

// RestoreSkill restores the most recent backup of a skill.
func RestoreSkill(dirs *CodexDirs, scope, slug string) error {
	backupRoot := dirs.BackupsDir(scope)
	matches, err := filepath.Glob(filepath.Join(backupRoot, slug+"-*"))
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf("没有可恢复的备份")
	}
	latest := matches[len(matches)-1]
	dest := filepath.Join(dirs.SkillsDir(scope), slug)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("目标目录 %s 已存在", dest)
	}
	return os.Rename(latest, dest)
}

// extractZipTo safely extracts a zip into dir, rejecting traversal.
func extractZipTo(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		clean := path.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return fmt.Errorf("压缩包包含不安全路径: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(clean))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("压缩包路径越界: %s", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(rc, 64<<20)); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

func sha256File(p string) (string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

var _ = bytes.MinRead
