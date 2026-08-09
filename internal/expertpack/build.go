// Package expertpack builds deterministic expert packs: a coordinator skill
// plus locked member skills packaged into a reproducible zip archive.
package expertpack

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MemberFile is one file inside a member skill.
type MemberFile struct {
	// Path is relative to the member skill root, e.g. "SKILL.md".
	Path string
	Data []byte
}

// Member is a locked skill version included in the pack.
type Member struct {
	Slug        string
	Name        string
	Version     int
	SHA256      string
	Description string
	Files       []MemberFile
}

// Spec describes the pack to build.
type Spec struct {
	PackSlug       string
	Name           string
	Description    string
	Domain         string
	Responsibility string
	Usage          string
	Members        []Member
}

// Manifest is the deterministic, locked install manifest.
type Manifest struct {
	Pack         PackInfo        `json:"pack"`
	Coordinator  CoordinatorInfo `json:"coordinator"`
	Members      []MemberInfo    `json:"members"`
	InstallOrder []string        `json:"installOrder"`
}

// PackInfo describes the built archive.
type PackInfo struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// CoordinatorInfo describes the generated coordinator skill.
type CoordinatorInfo struct {
	Slug   string `json:"slug"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// MemberInfo locks a member skill version.
type MemberInfo struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Version      int    `json:"version"`
	SHA256       string `json:"sha256"`
	Description  string `json:"description"`
	InstallOrder int    `json:"installOrder"`
}

// Result is the deterministic build output.
type Result struct {
	Archive     []byte
	Manifest    Manifest
	Coordinator string // coordinator SKILL.md content
}

// Build produces a deterministic zip and manifest. Members are sorted by slug
// and zip entries are emitted in sorted order with fixed timestamps, so equal
// inputs always produce identical bytes and hashes.
func Build(spec Spec) (*Result, error) {
	if !safeSlug(spec.PackSlug) {
		return nil, fmt.Errorf("pack slug is invalid")
	}
	members := append([]Member(nil), spec.Members...)
	for _, m := range members {
		if !safeSlug(m.Slug) {
			return nil, fmt.Errorf("member slug %q is invalid", m.Slug)
		}
	}
	sort.SliceStable(members, func(i, j int) bool { return members[i].Slug < members[j].Slug })

	coordinator := renderCoordinator(spec, members)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Emit entries in sorted order with a fixed timestamp.
	type entry struct {
		name string
		data []byte
	}
	entries := []entry{}

	coordPath := path.Join(spec.PackSlug, "SKILL.md")
	entries = append(entries, entry{coordPath, []byte(coordinator)})
	for _, m := range members {
		files := append([]MemberFile(nil), m.Files...)
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		for _, f := range files {
			if f.Path == "" {
				continue
			}
			entries = append(entries, entry{path.Join(spec.PackSlug, "members", m.Slug, f.Path), f.Data})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.name] {
			continue
		}
		seen[e.name] = true
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.SetModTime(zipTime)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	archive := buf.Bytes()
	archHash := sha256.Sum256(archive)

	coordHash := sha256.Sum256([]byte(coordinator))
	manifest := Manifest{
		Pack: PackInfo{
			Slug: spec.PackSlug, Name: spec.Name, Version: 0, // version assigned by caller
			SHA256: hex.EncodeToString(archHash[:]), Size: int64(len(archive)),
		},
		Coordinator: CoordinatorInfo{
			Slug: spec.PackSlug, Path: coordPath, SHA256: hex.EncodeToString(coordHash[:]),
		},
	}
	for i, m := range members {
		manifest.Members = append(manifest.Members, MemberInfo{
			Slug: m.Slug, Name: m.Name, Version: m.Version, SHA256: m.SHA256,
			Description: m.Description, InstallOrder: i + 1,
		})
		manifest.InstallOrder = append(manifest.InstallOrder, m.Slug)
	}
	return &Result{Archive: archive, Manifest: manifest, Coordinator: coordinator}, nil
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func safeSlug(s string) bool { return slugPattern.MatchString(s) }

// fixed zip timestamp (2000-01-01 UTC) for reproducible builds.
var zipTime = mustTime()

func mustTime() time.Time { return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) }

// EncodeManifest serializes a manifest deterministically.
func EncodeManifest(m Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ParseManifest decodes a manifest.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// renderCoordinator produces the coordinator SKILL.md with stable ordering.
func renderCoordinator(spec Spec, members []Member) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + spec.Name + "\n")
	b.WriteString("description: " + oneLine(spec.Description) + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + spec.Name + "\n\n")
	if spec.Domain != "" {
		b.WriteString("**领域**: " + spec.Domain + "\n\n")
	}
	if spec.Responsibility != "" {
		b.WriteString("**职责**: " + spec.Responsibility + "\n\n")
	}
	if spec.Usage != "" {
		b.WriteString("**使用说明**: " + spec.Usage + "\n\n")
	}
	b.WriteString("## 成员技能\n\n")
	b.WriteString("| 技能 | 版本 | 说明 |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, m := range members {
		desc := oneLine(m.Description)
		b.WriteString(fmt.Sprintf("| %s | %d | %s |\n", m.Slug, m.Version, desc))
	}
	b.WriteString("\n## 选择规则\n\n")
	b.WriteString("根据任务内容判断所属领域，优先调用与该领域匹配的成员技能；")
	b.WriteString("如果任务跨越多个领域，按顺序组合调用多个成员技能。\n")
	return b.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return strings.TrimSpace(s)
}
