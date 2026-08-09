package cli

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// hardExcludes can never be re-included via .aihubignore.
var hardExcludes = []string{
	".git/", "node_modules/", "vendor/", "__pycache__/", ".venv/", "venv/",
	".DS_Store", ".env", ".env.*", "*.pem", "*.key", "id_rsa", "id_ed25519",
	".aihubignore", ".aihub-managed.json",
}

const maxSkillFileSize = 10 << 20 // 10 MiB

// ZipDir packages dir into a zip, applying security defaults and .aihubignore.
func ZipDir(dir string) ([]byte, error) {
	ignore, err := loadIgnore(filepath.Join(dir, ".aihubignore"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	err = filepath.Walk(absDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		// Directory pruning: if dir excluded and no re-include, skip.
		if info.IsDir() {
			if relSlash != "" && isExcluded(relSlash+"/", ignore, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcluded(relSlash, ignore, false) {
			return nil
		}
		if info.Size() > maxSkillFileSize {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		fh, err := zw.Create(relSlash)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(fh, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// isExcluded decides whether rel matches the ignore rules.
func isExcluded(rel string, rules []ignoreRule, isDir bool) bool {
	excluded := false
	for _, rule := range rules {
		if rule.negate {
			if rule.matches(rel, isDir) {
				excluded = false
			}
			continue
		}
		if rule.matches(rel, isDir) {
			excluded = true
		}
	}
	// Hard excludes override any re-include.
	for _, h := range hardExcludes {
		if matchPattern(h, rel, isDir) {
			return true
		}
	}
	return excluded
}

type ignoreRule struct {
	pattern string
	negate  bool
	dirOnly bool
}

func loadIgnore(path string) ([]ignoreRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rules := []ignoreRule{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := strings.HasPrefix(line, "!")
		if negate {
			line = strings.TrimPrefix(line, "!")
		}
		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		rules = append(rules, ignoreRule{pattern: line, negate: negate, dirOnly: dirOnly})
	}
	return rules, nil
}

func (r ignoreRule) matches(rel string, isDir bool) bool {
	if r.dirOnly && !isDir {
		// dir-only patterns still match files inside? Standard gitignore: dir/
		// excludes the directory and everything under it. We handle via pruning,
		// so only apply to dirs.
		return false
	}
	return matchPattern(r.pattern, rel, isDir)
}

func matchPattern(pattern, rel string, isDir bool) bool {
	p := strings.TrimPrefix(pattern, "/")
	// Match against both rel and any parent dir prefix.
	candidates := []string{rel}
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		candidates = append(candidates, rel[:i])
	}
	for _, c := range candidates {
		if globMatch(p, c) {
			return true
		}
		// Also match basename patterns (e.g. "*.log").
		base := rel
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			base = rel[i+1:]
		}
		if !strings.Contains(p, "/") && globMatch(p, base) {
			return true
		}
	}
	return false
}

// globMatch supports *, **, ?.
func globMatch(pattern, s string) bool {
	// Convert glob to simple recursive matcher.
	return matchSegments(strings.Split(pattern, "/"), strings.Split(s, "/"))
}

func matchSegments(pats, segs []string) bool {
	for len(pats) > 0 {
		p := pats[0]
		if p == "**" {
			if len(pats) == 1 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pats[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if !matchSegment(p, segs[0]) {
			return false
		}
		pats = pats[1:]
		segs = segs[1:]
	}
	return len(segs) == 0
}

func matchSegment(p, s string) bool {
	var (
		pi, si = 0, 0
		star   = -1
		match  = 0
	)
	for si < len(s) {
		if pi < len(p) && (p[pi] == '?' || p[pi] == s[si]) {
			pi++
			si++
		} else if pi < len(p) && p[pi] == '*' {
			star = pi
			match = si
			pi++
		} else if star != -1 {
			pi = star + 1
			match++
			si = match
		} else {
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

var _ = fmt.Sprintf
