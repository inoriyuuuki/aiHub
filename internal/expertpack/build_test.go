package expertpack

import (
	"testing"
)

func sampleSpec() Spec {
	return Spec{
		PackSlug: "frontend-expert", Name: "前端专家", Description: "前端开发",
		Domain: "frontend", Responsibility: "负责前端", Usage: "需要时调用",
		Members: []Member{
			{Slug: "react-dev", Name: "React", Version: 2, SHA256: "aaaa", Description: "React 开发", Files: []MemberFile{{Path: "SKILL.md", Data: []byte("---\nname: react-dev\n---\n")}}},
			{Slug: "css-master", Name: "CSS", Version: 1, SHA256: "bbbb", Description: "CSS 专家", Files: []MemberFile{{Path: "SKILL.md", Data: []byte("---\nname: css-master\n---\n")}}},
		},
	}
}

func TestBuildDeterministic(t *testing.T) {
	r1, err := Build(sampleSpec())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Build(sampleSpec())
	if err != nil {
		t.Fatal(err)
	}
	if r1.Manifest.Pack.SHA256 != r2.Manifest.Pack.SHA256 {
		t.Fatalf("build not deterministic: %s != %s", r1.Manifest.Pack.SHA256, r2.Manifest.Pack.SHA256)
	}
	if string(r1.Archive) != string(r2.Archive) {
		t.Fatal("archives differ")
	}
	if len(r1.Manifest.Members) != 2 {
		t.Fatalf("members = %d", len(r1.Manifest.Members))
	}
	if r1.Manifest.InstallOrder[0] != "css-master" {
		t.Fatalf("install order should be sorted: %v", r1.Manifest.InstallOrder)
	}
}

func TestBuildRejectsEmptyPackSlug(t *testing.T) {
	spec := sampleSpec()
	spec.PackSlug = ""
	if _, err := Build(spec); err == nil {
		t.Fatal("expected error for empty pack slug")
	}
}

func TestManifestRoundTrip(t *testing.T) {
	r, err := Build(sampleSpec())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := EncodeManifest(r.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseManifest(enc)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Pack.SHA256 != r.Manifest.Pack.SHA256 {
		t.Fatal("manifest round trip mismatch")
	}
}
