package skillpack

import (
	"archive/zip"
	"bytes"
	"testing"
)

func makeZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content)) //nolint:errcheck
	}
	zw.Close() //nolint:errcheck
	return buf.Bytes()
}

func TestValidateOK(t *testing.T) {
	data := makeZip(t, map[string]string{
		"SKILL.md":        "---\nname: demo\n---\n# Demo\n",
		"references/a.md": "ref",
	})
	meta, err := Validate(data, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "demo" {
		t.Fatalf("name = %q", meta.Name)
	}
	if len(meta.Files) != 2 {
		t.Fatalf("files = %v", meta.Files)
	}
}

func TestValidateMissingSKILL(t *testing.T) {
	data := makeZip(t, map[string]string{"a.txt": "x"})
	if _, err := Validate(data, 1<<20); err == nil {
		t.Fatal("expected error for missing SKILL.md")
	}
}

func TestValidateTraversal(t *testing.T) {
	data := makeZip(t, map[string]string{
		"SKILL.md":      "---\nname: demo\n---\n",
		"../escape.txt": "bad",
	})
	if _, err := Validate(data, 1<<20); err == nil {
		t.Fatal("expected error for traversal path")
	}
}

func TestValidateAbsolutePath(t *testing.T) {
	data := makeZip(t, map[string]string{
		"SKILL.md":    "---\nname: demo\n---\n",
		"/etc/passwd": "bad",
	})
	if _, err := Validate(data, 1<<20); err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestValidateNestedRoot(t *testing.T) {
	data := makeZip(t, map[string]string{
		"my-skill/SKILL.md":       "---\nname: my-skill\n---\n",
		"my-skill/scripts/run.sh": "echo hi",
		"other/ignored.txt":       "x",
	})
	meta, err := Validate(data, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if meta.RootDir != "my-skill" {
		t.Fatalf("root = %q", meta.RootDir)
	}
	if len(meta.Files) != 2 {
		t.Fatalf("files = %v", meta.Files)
	}
}

func TestValidateOversize(t *testing.T) {
	data := makeZip(t, map[string]string{"SKILL.md": string(bytes.Repeat([]byte("a"), 1024))})
	if _, err := Validate(data, 100); err == nil {
		t.Fatal("expected error for oversized file")
	}
}
