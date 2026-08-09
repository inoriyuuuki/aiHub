package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dataDir := t.TempDir()
	srv, err := New(&Config{
		Addr:      ":0",
		DataDir:   dataDir,
		AdminUser: "admin",
		AdminPass: "secret",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return srv, ts
}

func login(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret"})
	resp, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if out.Token == "" {
		t.Fatal("login returned empty token")
	}
	return out.Token
}

func TestLoginAndTokens(t *testing.T) {
	_, ts := newTestServer(t)
	token := login(t, ts)

	// list tokens with auth
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tokens status %d", resp.StatusCode)
	}

	// unauthenticated request is rejected
	resp2, err := http.Get(ts.URL + "/api/v1/tokens")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp2.StatusCode)
	}

	// wrong password rejected
	bad, _ := json.Marshal(map[string]string{"username": "admin", "password": "nope"})
	resp3, err := http.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad password, got %d", resp3.StatusCode)
	}
}

func TestPublishAndManifest(t *testing.T) {
	_, ts := newTestServer(t)
	token := login(t, ts)

	// Build a small skill zip in memory.
	zipData, err := makeTestZip(t, map[string]string{"SKILL.md": "# Hello\n\nDemo skill.\n"})
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "demo.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(zipData); err != nil {
		t.Fatal(err)
	}
	_ = mw.WriteField("slug", "demo")
	_ = mw.WriteField("name", "Demo Skill")
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/skills/publish", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("publish status %d: %s", resp.StatusCode, b)
	}

	// Fetch install manifest.
	req2, _ := http.NewRequest("GET", ts.URL+"/api/v1/skills/install-manifest?slug=demo", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("manifest status %d", resp2.StatusCode)
	}
	var manifest struct {
		Slug    string `json:"slug"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Slug != "demo" || manifest.Content == "" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
}

func TestFrontendServesIndex(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("frontend status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || ct[:5] != "text/" {
		t.Fatalf("unexpected content type %q", ct)
	}
}

func TestHealth(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status %d", resp.StatusCode)
	}
}

func makeTestZip(t *testing.T, entries map[string]string) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
