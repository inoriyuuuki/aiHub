// Package skillpack validates and inspects AIHub Skill packages (zip archives
// containing a SKILL.md, compatible with Codex skill discovery).
package skillpack

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Meta describes a validated skill package.
type Meta struct {
	// Name from SKILL.md frontmatter.
	Name string
	// Description from SKILL.md frontmatter or first heading.
	Description string
	// RootDir is the top-level directory containing SKILL.md ("" when at root).
	RootDir string
	// Files is the safe, slash-separated list of files relative to RootDir.
	Files []string
	// TotalSize is the total uncompressed size in bytes.
	TotalSize int64
}

// Validate inspects a zip archive for a legal skill package.
//
// It rejects path traversal, absolute paths, symlinks, missing SKILL.md,
// and oversized content.
func Validate(data []byte, maxBytes int64) (*Meta, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("不是合法的 zip 压缩包: %w", err)
	}
	var total int64
	var skillEntry *zip.File
	var root string
	seenDirs := map[string]bool{}
	files := []string{}

	for _, f := range zr.File {
		if f.UncompressedSize64 > uint64(maxBytes) {
			return nil, fmt.Errorf("压缩包内文件 %q 超过大小限制", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > maxBytes {
			return nil, fmt.Errorf("压缩包总大小超过限制")
		}
		name := cleanZipName(f.Name)
		if !isSafeZipPath(f.Name) {
			return nil, fmt.Errorf("压缩包包含不安全路径: %q", f.Name)
		}
		if isSymlink(f) {
			return nil, fmt.Errorf("压缩包包含符号链接: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			seenDirs[strings.TrimSuffix(name, "/")] = true
			continue
		}
		files = append(files, name)
		if path.Base(name) == "SKILL.md" {
			if skillEntry != nil {
				return nil, fmt.Errorf("压缩包包含多个 SKILL.md")
			}
			skillEntry = f
			root = path.Dir(name)
			if root == "." {
				root = ""
			}
		}
	}
	if skillEntry == nil {
		return nil, fmt.Errorf("压缩包缺少 SKILL.md")
	}
	// Only files under root are part of the skill.
	valid := []string{}
	for _, name := range files {
		if root == "" {
			valid = append(valid, name)
		} else if name == root+"/SKILL.md" || strings.HasPrefix(name, root+"/") {
			valid = append(valid, strings.TrimPrefix(name, root+"/"))
		}
	}
	sort.Strings(valid)

	meta := &Meta{RootDir: root, Files: valid, TotalSize: total}
	rc, err := skillEntry.Open()
	if err != nil {
		return nil, fmt.Errorf("读取 SKILL.md 失败: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	rc.Close()
	if err != nil {
		return nil, fmt.Errorf("读取 SKILL.md 失败: %w", err)
	}
	meta.Name, meta.Description = parseFrontmatter(content)
	if meta.Name == "" {
		// Derive name from the root directory if frontmatter is missing.
		if root != "" {
			meta.Name = path.Base(root)
		}
	}
	if meta.Name == "" {
		return nil, fmt.Errorf("SKILL.md 缺少 name")
	}
	return meta, nil
}

// cleanZipName normalizes a zip entry name to slash-separated relative path.
func cleanZipName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	return strings.TrimPrefix(name, "./")
}

// isSafeZipPath rejects traversal, absolute and weird paths.
func isSafeZipPath(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "/") {
		return false
	}
	cleaned := path.Clean(cleanZipName(name))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	// Reject any remaining traversal attempt or drive letters.
	if strings.Contains(name, "..") && (strings.Contains(name, "../") || strings.Contains(name, "..\\")) {
		return false
	}
	if len(name) >= 2 && name[1] == ':' {
		return false
	}
	return true
}

func isSymlink(f *zip.File) bool {
	return f.Mode()&0xF000 == 0xA000 // S_IFLNK
}

// parseFrontmatter extracts name and description from a SKILL.md frontmatter.
func parseFrontmatter(content []byte) (name, desc string) {
	text := string(content)
	if !strings.HasPrefix(text, "---") {
		// Fall back to the first heading.
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "# ")), ""
			}
		}
		return "", ""
	}
	rest := strings.TrimPrefix(text, "---")
	end := strings.Index(rest, "---")
	if end < 0 {
		return "", ""
	}
	fm := rest[:end]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), `"'`)
		}
		if strings.HasPrefix(line, "description:") {
			desc = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), `"'`)
		}
	}
	return name, desc
}
