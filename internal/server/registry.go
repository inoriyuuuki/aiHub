package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aihub/aihub/internal/cli"
)

var errSlug = errors.New("invalid slug")

// SkillRecord is a published skill stored on the hub.
type SkillRecord struct {
	Manifest cli.SkillManifest `json:"manifest"`
	ZipPath  string            `json:"zipPath"`
	Created  time.Time         `json:"created"`
}

// Registry serves published skills, expert packs and MCP profiles.
type Registry struct {
	mu       sync.RWMutex
	dataDir  string
	skills   map[string]*SkillRecord
	experts  map[string]*cli.ExpertManifest
	profiles map[string]*cli.MCPInstallManifest
}

// LoadRegistry loads published content from the data dir.
func LoadRegistry(dataDir string) (*Registry, error) {
	r := &Registry{
		dataDir:  dataDir,
		skills:   map[string]*SkillRecord{},
		experts:  map[string]*cli.ExpertManifest{},
		profiles: map[string]*cli.MCPInstallManifest{},
	}
	if err := r.loadSkills(filepath.Join(dataDir, "skills")); err != nil {
		return nil, err
	}
	if err := r.loadExperts(filepath.Join(dataDir, "expert-packs")); err != nil {
		return nil, err
	}
	if err := r.loadProfiles(filepath.Join(dataDir, "mcp")); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) loadSkills(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		var rec SkillRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		r.skills[e.Name()] = &rec
	}
	return nil
}

func (r *Registry) loadExperts(dir string) error {
	return loadManifestDir(dir, &r.experts)
}

func (r *Registry) loadProfiles(dir string) error {
	return loadManifestDir(dir, &r.profiles)
}

func loadManifestDir[T any](dir string, dst *map[string]*T) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m T
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		(*dst)[name] = &m
	}
	return nil
}

// Skill returns a published skill by slug.
func (r *Registry) Skill(slug string) (*SkillRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.skills[slug]
	return rec, ok
}

// Expert returns a published expert pack by slug.
func (r *Registry) Expert(slug string) (*cli.ExpertManifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.experts[slug]
	return m, ok
}

// Profile returns a published MCP profile by name.
func (r *Registry) Profile(name string) (*cli.MCPInstallManifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.profiles[name]
	return m, ok
}

// PublishSkill stores an uploaded skill zip and its manifest.
func (r *Registry) PublishSkill(zipData []byte, filename, slug, name, desc, category string, tags []string, changelog string) (string, error) {
	if slug == "" {
		slug = strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	}
	slug = sanitizeSlug(slug)
	if slug == "" {
		return "", errSlug
	}
	dir := filepath.Join(r.dataDir, "skills", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, "skill.zip")
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		return "", err
	}
	version := 1
	if rec, ok := r.Skill(slug); ok && rec.Manifest.Version.Version > 0 {
		version = rec.Manifest.Version.Version + 1
	}
	rec := &SkillRecord{
		Manifest: cli.SkillManifest{
			Slug:        slug,
			Name:        firstNonEmpty(name, slug),
			Description: desc,
			Category:    category,
			Tags:        tags,
			Version:     cli.VersionInfo{Version: version, Changelog: changelog},
			Source:      "hub",
			Content:     readSkillMD(zipData),
		},
		ZipPath: zipPath,
		Created: time.Now(),
	}
	manifestData, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestData, 0o644); err != nil {
		return "", err
	}
	r.mu.Lock()
	r.skills[slug] = rec
	r.mu.Unlock()
	return slug, nil
}

// ListSkills returns all published skills sorted by slug.
func (r *Registry) ListSkills() []*SkillRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*SkillRecord, 0, len(r.skills))
	for _, rec := range r.skills {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Slug < out[j].Manifest.Slug })
	return out
}

// readSkillMD extracts the root SKILL.md body from a skill archive.
func readSkillMD(zipData []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		if filepath.ToSlash(f.Name) != "SKILL.md" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err == nil {
			return string(b)
		}
	}
	return ""
}

func sanitizeSlug(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		}
	}
	return b.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
