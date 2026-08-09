package cli

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ZipDir compresses a directory tree into an in-memory zip archive,
// excluding .git metadata.
func ZipDir(dir string) ([]byte, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, os.ErrInvalid
	}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			return nil
		}
		if fi.IsDir() {
			_, err := zw.Create(rel + "/")
			return err
		}
		fh, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		fh.Name = rel
		fh.Method = zip.Deflate
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
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
