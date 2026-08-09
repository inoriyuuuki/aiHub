package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ExpertManifest is the expert pack install manifest.
type ExpertManifest struct {
	Pack struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
	} `json:"pack"`
	Manifest struct {
		Coordinator struct {
			Slug string `json:"slug"`
		} `json:"coordinator"`
		Members []struct {
			Slug    string `json:"slug"`
			Name    string `json:"name"`
			Version int    `json:"version"`
			SHA256  string `json:"sha256"`
		} `json:"members"`
	} `json:"manifest"`
	DownloadURL string `json:"downloadUrl"`
}

// expertMarker records an installed expert pack.
type expertMarker struct {
	PackSlug    string    `json:"packSlug"`
	Version     int       `json:"version"`
	SHA256      string    `json:"sha256"`
	InstalledAt time.Time `json:"installedAt"`
}

// InstallExpertPack installs the coordinator skill and all member skills
// atomically: on any failure previously installed state is restored.
func InstallExpertPack(client *Client, dirs *CodexDirs, scope string, manifest *ExpertManifest) error {
	tmp, err := os.MkdirTemp("", "aihub-expert-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck

	zipPath := filepath.Join(tmp, "pack.zip")
	if err := DownloadToFile(manifest.DownloadURL, zipPath); err != nil {
		return err
	}
	if err := extractZipTo(zipPath, tmp); err != nil {
		return err
	}
	// Layout: <pack>/SKILL.md, <pack>/members/<memberSlug>/...
	packSrc := filepath.Join(tmp, manifest.Pack.Slug)
	skillsDir := dirs.SkillsDir(scope)

	// Snapshot existing managed dirs so we can roll back on failure.
	installed := map[string]string{} // slug -> backup path
	rollback := func() {
		for slug, backup := range installed {
			dest := filepath.Join(skillsDir, slug)
			os.RemoveAll(dest) //nolint:errcheck
			if backup != "" {
				_ = os.Rename(backup, dest)
			}
		}
	}

	installOne := func(slug, src string) error {
		dest := filepath.Join(skillsDir, slug)
		if _, err := os.Stat(dest); err == nil {
			if _, err := os.Stat(filepath.Join(dest, ".aihub-managed.json")); err != nil {
				return fmt.Errorf("目录 %s 已存在且非 AIHub 管理，拒绝覆盖", dest)
			}
			backup := filepath.Join(dirs.BackupsDir(scope), fmt.Sprintf("%s-%d", slug, time.Now().Unix()))
			if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
				return err
			}
			if err := os.Rename(dest, backup); err != nil {
				return err
			}
			installed[slug] = backup
		} else {
			installed[slug] = ""
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return copyDir(src, dest)
	}

	// Coordinator first.
	if err := installOne(manifest.Pack.Slug, packSrc); err != nil {
		rollback()
		return err
	}
	membersSrc := filepath.Join(packSrc, "members")
	for _, m := range manifest.Manifest.Members {
		memberSrc := filepath.Join(membersSrc, m.Slug)
		if _, err := os.Stat(memberSrc); err != nil {
			rollback()
			return fmt.Errorf("专家包缺少成员目录: %s", m.Slug)
		}
		if err := installOne(m.Slug, memberSrc); err != nil {
			rollback()
			return err
		}
	}
	// Write markers.
	sha, _ := sha256File(zipPath)
	coordMarker := expertMarker{PackSlug: manifest.Pack.Slug, Version: manifest.Pack.Version, SHA256: sha, InstalledAt: time.Now().UTC()}
	coordData, _ := json.MarshalIndent(coordMarker, "", "  ")
	if err := os.WriteFile(filepath.Join(skillsDir, manifest.Pack.Slug, ".aihub-managed.json"), coordData, 0o644); err != nil {
		rollback()
		return err
	}
	for _, m := range manifest.Manifest.Members {
		memberMarker := expertMarker{PackSlug: manifest.Pack.Slug, Version: manifest.Pack.Version, SHA256: m.SHA256, InstalledAt: time.Now().UTC()}
		data, _ := json.MarshalIndent(memberMarker, "", "  ")
		if err := os.WriteFile(filepath.Join(skillsDir, m.Slug, ".aihub-managed.json"), data, 0o644); err != nil {
			rollback()
			return err
		}
	}
	return nil
}

// RemoveExpertPack removes the coordinator and member skills of a pack that
// carry AIHub expert markers, backing them up first.
func RemoveExpertPack(dirs *CodexDirs, scope, packSlug string) error {
	skillsDir := dirs.SkillsDir(scope)
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("专家包 %s 未安装", packSlug)
		}
		return err
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		markerPath := filepath.Join(skillsDir, slug, ".aihub-managed.json")
		data, err := os.ReadFile(markerPath)
		if err != nil {
			continue
		}
		var marker expertMarker
		if err := json.Unmarshal(data, &marker); err != nil {
			continue
		}
		if marker.PackSlug != packSlug {
			continue
		}
		backup := filepath.Join(dirs.BackupsDir(scope), fmt.Sprintf("%s-remove-%d", slug, time.Now().Unix()))
		if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(skillsDir, slug), backup); err != nil {
			return err
		}
		removed++
	}
	if removed == 0 {
		return fmt.Errorf("专家包 %s 未安装", packSlug)
	}
	return nil
}
