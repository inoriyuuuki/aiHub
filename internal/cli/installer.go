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
	if err := checkSlug(manifest.Skill.Slug); err != nil {
		return err
	}
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
	if manifest.Version.SHA256 != "" && sha != manifest.Version.SHA256 {
		return fmt.Errorf("Skill 压缩包 SHA-256 校验失败：期望 %s 实际 %s", manifest.Version.SHA256, sha)
	}
	_ = os.Remove(zipPath) // the zip is not part of the installed skill
	// Normalize: content may live under a root dir.
	src := tmp
	if manifest.Version.RootDir != "" {
		candidate := filepath.Join(tmp, filepath.FromSlash(manifest.Version.RootDir))
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			src = candidate
		}
	}
	marker := managedMarker{
		Slug: manifest.Skill.Slug, Version: manifest.Version.Version,
		SHA256: sha, Source: manifest.Source, InstalledAt: time.Now().UTC(),
	}
	markerData, _ := json.MarshalIndent(marker, "", "  ")
	if err := os.WriteFile(filepath.Join(src, ".aihub-managed.json"), markerData, 0o644); err != nil {
		return err
	}

	// Backup any existing managed directory.
	if _, err := os.Stat(dest); err == nil {
		backup := filepath.Join(dirs.BackupsDir(scope), fmt.Sprintf("%s-%d-%d", manifest.Skill.Slug, time.Now().UnixNano(), time.Now().Unix()))
		if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return err
		}
		if err := os.Rename(dest, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(src, dest); err != nil {
		// If rename of directory fails (cross-device), fall back to copy.
		if err := copyDir(src, dest); err != nil {
			return err
		}
	}
	return nil
}

// RemoveSkill backs up and removes a managed skill directory.
func RemoveSkill(dirs *CodexDirs, scope, slug string) error {
	if err := checkSlug(slug); err != nil {
		return err
	}
	dest := filepath.Join(dirs.SkillsDir(scope), slug)
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("Skill %s 未安装", slug)
	}
	if _, err := os.Stat(filepath.Join(dest, ".aihub-managed.json")); err != nil {
		return fmt.Errorf("目录 %s 不是 AIHub 管理的，拒绝删除", dest)
	}
	backup := filepath.Join(dirs.BackupsDir(scope), fmt.Sprintf("%s-remove-%d-%d", slug, time.Now().UnixNano(), time.Now().Unix()))
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
	if err := checkSlug(slug); err != nil {
		return err
	}
	backupRoot := dirs.BackupsDir(scope)
	matches, err := filepath.Glob(filepath.Join(backupRoot, slug+"-*"))
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return fmt.Errorf("没有可恢复的备份")
	}
	// Prefer install backups over "-remove-" backups; newest first.
	type cand struct {
		path   string
		mod    time.Time
		remove bool
	}
	cands := []cand{}
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		cands = append(cands, cand{m, fi.ModTime(), strings.Contains(filepath.Base(m), "-remove-")})
	}
	if len(cands) == 0 {
		return fmt.Errorf("没有可恢复的备份")
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if !c.remove && best.remove {
			best = c
		} else if c.remove == best.remove && c.mod.After(best.mod) {
			best = c
		}
	}
	dest := filepath.Join(dirs.SkillsDir(scope), slug)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("目标目录 %s 已存在", dest)
	}
	return os.Rename(best.path, dest)
}

// extractZipTo safely extracts a zip into dir, rejecting traversal, symlinks
// and oversized content, and preserving executable bits.
func extractZipTo(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer zr.Close()
	var total int64
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		clean := path.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return fmt.Errorf("压缩包包含不安全路径: %s", f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("压缩包包含符号链接: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > 64<<20 || total+int64(f.UncompressedSize64) > 512<<20 {
			return fmt.Errorf("压缩包内容超过大小限制: %s", f.Name)
		}
		total += int64(f.UncompressedSize64)
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
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
