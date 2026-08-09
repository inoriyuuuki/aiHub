//go:build integration

// Package tests contains end-to-end integration tests against real
// PostgreSQL and MinIO instances (docker compose dev environment).
package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aihub/aihub/internal/config"
	"github.com/aihub/aihub/internal/server"
)

type env struct {
	baseURL string
	client  *http.Client
}

func startServer(t *testing.T) *env {
	t.Helper()
	dsn := os.Getenv("AIHUB_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AIHUB_TEST_DATABASE_URL not set")
	}
	pwFile := filepath.Join(t.TempDir(), "admin_password")
	if err := os.WriteFile(pwFile, []byte("integration-pass-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		HTTPAddr:          ":0",
		PublicBaseURL:     "http://localhost",
		DatabaseURL:       dsn,
		MinIOEndpoint:     getenv("AIHUB_TEST_MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:    getenv("AIHUB_TEST_MINIO_ACCESS_KEY", "aihub"),
		MinIOSecretKey:    getenv("AIHUB_TEST_MINIO_SECRET_KEY", "aihub-secret"),
		MinIOUseSSL:       false,
		MinIOBucket:       "aihub-test-" + fmt.Sprint(time.Now().UnixNano()),
		AdminUsername:     "admin",
		AdminPasswordFile: pwFile,
		SessionTTL:        time.Hour,
		LoginMaxAttempts:  100,
		LoginWindow:       time.Minute,
		MaxUploadBytes:    50 << 20,
		EnabledModules:    map[string]bool{},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := server.New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	ts := httptest.NewServer(app.Handler(logger))
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return &env{baseURL: ts.URL, client: &http.Client{Jar: jar}}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (e *env) do(t *testing.T, method, path string, body any, token string, want int) (map[string]any, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.baseURL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, resp.StatusCode, want, string(raw))
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, raw
}

func (e *env) login(t *testing.T) {
	t.Helper()
	e.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "integration-pass-123"}, "", 200)
}

func TestFullFlow(t *testing.T) {
	e := startServer(t)
	e.login(t)

	// Health
	e.do(t, "GET", "/api/v1/health", nil, "", 200)

	// Modules
	_, raw := e.do(t, "GET", "/api/v1/modules", nil, "", 200)
	if !strings.Contains(string(raw), "prompts") || !strings.Contains(string(raw), "skills") {
		t.Fatalf("modules missing entries: %s", raw)
	}

	// Project
	proj, _ := e.do(t, "POST", "/api/v1/projects", map[string]any{"name": "Demo", "slug": "demo", "scope": "project"}, "", 201)
	projID := int64(proj["data"].(map[string]any)["id"].(float64))

	// Category + schema
	cat, _ := e.do(t, "POST", "/api/v1/prompt-categories", map[string]any{"name": "客服", "slug": "support"}, "", 201)
	catID := int64(cat["data"].(map[string]any)["id"].(float64))
	schema := map[string]any{
		"type":              "object",
		"x-aihub-variables": []any{map[string]any{"name": "topic"}},
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "x-aihub-ui": "text", "title": "标题"},
			"messages": map[string]any{
				"type": "array", "x-aihub-ui": "repeatable-group",
				"items": map[string]any{"type": "object", "properties": map[string]any{
					"role":    map[string]any{"type": "string", "enum": []any{"user", "assistant"}},
					"content": map[string]any{"type": "string"},
				}},
			},
		},
		"required": []any{"title"},
	}
	e.do(t, "POST", fmt.Sprintf("/api/v1/prompt-categories/%d/schemas", catID), map[string]any{"schema": schema}, "", 201)

	// Prompt draft
	p, _ := e.do(t, "POST", "/api/v1/prompts", map[string]any{
		"projectId": projID, "categoryId": catID, "slug": "greeting", "title": "问候",
		"content": map[string]any{"title": "关于 {{topic}} 的问候"},
	}, "", 201)
	pID := int64(p["data"].(map[string]any)["id"].(float64))

	// Publish v1
	e.do(t, "POST", fmt.Sprintf("/api/v1/prompts/%d/publish", pID), map[string]any{"summary": "v1"}, "", 201)
	// Update draft + publish v2
	e.do(t, "PATCH", fmt.Sprintf("/api/v1/prompts/%d", pID), map[string]any{
		"projectId": projID, "categoryId": catID, "slug": "greeting", "title": "问候",
		"content": map[string]any{"title": "新版关于 {{topic}} 的问候"},
	}, "", 200)
	e.do(t, "POST", fmt.Sprintf("/api/v1/prompts/%d/publish", pID), map[string]any{"summary": "v2"}, "", 201)
	versions, _ := e.do(t, "GET", fmt.Sprintf("/api/v1/prompts/%d/versions", pID), nil, "", 200)
	if len(versions["data"].([]any)) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions["data"].([]any)))
	}
	// Diff v2 vs v1
	d, _ := e.do(t, "GET", fmt.Sprintf("/api/v1/prompts/%d/versions/2/diff?base=1", pID), nil, "", 200)
	if !strings.Contains(d["data"].(map[string]any)["diff"].(string), "新版") {
		t.Fatalf("diff missing change: %v", d["data"])
	}
	// Rollback to v1 -> v3
	rb, _ := e.do(t, "POST", fmt.Sprintf("/api/v1/prompts/%d/rollback", pID), map[string]any{"version": 1}, "", 200)
	cur := rb["data"].(map[string]any)["currentVersion"].(map[string]any)
	if cur["version"].(float64) != 3 {
		t.Fatalf("rollback should create v3, got %v", cur["version"])
	}
	// By-slug resolution (project-first)
	bySlug, _ := e.do(t, "GET", "/api/v1/prompts/resolve?slug=greeting&project=demo", nil, "", 200)
	if bySlug["data"].(map[string]any)["id"].(float64) != float64(pID) {
		t.Fatal("by-slug failed")
	}
	// Render
	rend, _ := e.do(t, "POST", fmt.Sprintf("/api/v1/prompts/%d/render", pID), map[string]any{"values": map[string]any{"topic": "价格"}}, "", 200)
	if !strings.Contains(fmt.Sprint(rend["data"]), "价格") {
		t.Fatalf("render output missing value: %v", rend["data"])
	}

	// Asset flow: presign -> put -> confirm
	picData := []byte{1, 2, 3, 4}
	picSum := sha256.Sum256(picData)
	picHash := hex.EncodeToString(picSum[:])
	asset, _ := e.do(t, "POST", "/api/v1/assets/presign", map[string]any{
		"kind": "image", "filename": "pic.png", "size": 4, "sha256": picHash,
		"mime": "image/png", "refType": "prompt", "refId": pID,
	}, "", 200)
	uploadURL := asset["data"].(map[string]any)["uploadUrl"].(string)
	putReq, _ := http.NewRequest("PUT", uploadURL, bytes.NewReader(picData))
	putResp, err := e.client.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode >= 400 {
		t.Fatalf("PUT to presigned url failed: %d", putResp.StatusCode)
	}
	conf, _ := e.do(t, "POST", "/api/v1/assets/confirm", map[string]any{
		"objectKey": asset["data"].(map[string]any)["objectKey"], "kind": "image",
		"filename": "pic.png", "size": 4, "sha256": picHash,
		"mime": "image/png", "refType": "prompt", "refId": pID,
	}, "", 201)
	assetID := int64(conf["data"].(map[string]any)["id"].(float64))
	e.do(t, "GET", fmt.Sprintf("/api/v1/assets/%d/url", assetID), nil, "", 200)
	// Invalid sha must be rejected
	e.do(t, "POST", "/api/v1/assets/confirm", map[string]any{
		"objectKey": asset["data"].(map[string]any)["objectKey"], "kind": "image",
		"filename": "pic.png", "size": 4, "sha256": strings.Repeat("cd", 32),
		"mime": "image/png", "refType": "prompt", "refId": pID,
	}, "", 422)

	// ---- Skills ----
	skill := uploadSkill(t, e, validSkillZip(t), map[string]string{"slug": "demo-skill", "name": "Demo Skill"})
	skillID := int64(skill["data"].(map[string]any)["id"].(float64))
	e.do(t, "GET", "/api/v1/skills/install-manifest?slug=demo-skill", nil, "", 200)
	// Invalid skill zip rejected
	e.do(t, "POST", "/api/v1/skills/upload", nil, "", 400) // no multipart -> bad request
	// Multipart with traversal zip -> 422
	uploadSkillExpect(t, e, traversalZip(t), map[string]string{"slug": "evil"}, 422)

	// ---- Expert pack ----
	pack, _ := e.do(t, "POST", "/api/v1/expert-packs", map[string]any{
		"name": "前端专家", "slug": "frontend-expert", "domain": "frontend",
		"responsibility": "负责前端", "usage": "需要时调用",
	}, "", 201)
	packID := int64(pack["data"].(map[string]any)["id"].(float64))
	// current version id of demo skill
	sv := skill["data"].(map[string]any)["currentVersion"].(map[string]any)
	svID := int64(sv["id"].(float64))
	e.do(t, "POST", fmt.Sprintf("/api/v1/expert-packs/%d/members", packID), map[string]any{"skillId": skillID, "skillVersionId": svID}, "", 200)
	b1, _ := e.do(t, "POST", fmt.Sprintf("/api/v1/expert-packs/%d/build", packID), map[string]any{}, "", 201)
	sha1 := b1["data"].(map[string]any)["currentVersion"].(map[string]any)["sha256"].(string)
	e.do(t, "POST", fmt.Sprintf("/api/v1/expert-packs/%d/build", packID), map[string]any{}, "", 201)
	vs, _ := e.do(t, "GET", fmt.Sprintf("/api/v1/expert-packs/%d/versions", packID), nil, "", 200)
	versionsArr := vs["data"].([]any)
	sha2 := versionsArr[0].(map[string]any)["sha256"].(string)
	if sha1 != sha2 {
		t.Fatalf("expert pack build not deterministic: %s != %s", sha1, sha2)
	}
	e.do(t, "GET", "/api/v1/expert-packs/install-manifest?slug=frontend-expert", nil, "", 200)

	// ---- MCP catalog ----
	def, _ := e.do(t, "POST", "/api/v1/mcp/definitions", map[string]any{"name": "Web Search", "slug": "web-search", "transport": "stdio"}, "", 201)
	defID := int64(def["data"].(map[string]any)["id"].(float64))
	e.do(t, "POST", fmt.Sprintf("/api/v1/mcp/definitions/%d/versions", defID), map[string]any{
		"config":  map[string]any{"command": "npx", "args": []any{"-y", "web-search"}},
		"envVars": []any{map[string]any{"name": "API_KEY", "description": "密钥", "sensitive": true, "required": true}},
		"tools":   []any{map[string]any{"name": "web_search", "description": "搜索", "defaultEnabled": true}},
	}, "", 201)
	prof, _ := e.do(t, "POST", "/api/v1/mcp/profiles", map[string]any{"name": "默认", "slug": "default", "scope": "global"}, "", 201)
	profID := int64(prof["data"].(map[string]any)["id"].(float64))
	defVer := def["data"].(map[string]any)["currentVersion"]
	_ = defVer
	defVersions, _ := e.do(t, "GET", fmt.Sprintf("/api/v1/mcp/definitions/%d/versions", defID), nil, "", 200)
	dv := defVersions["data"].([]any)[0].(map[string]any)
	dvID := int64(dv["id"].(float64))
	e.do(t, "POST", fmt.Sprintf("/api/v1/mcp/profiles/%d/items", profID), map[string]any{
		"definitionId": defID, "definitionVersionId": dvID, "enabledTools": []any{"web_search"},
	}, "", 201)
	manifest, _ := e.do(t, "GET", "/api/v1/mcp/install-manifest?profile=default", nil, "", 200)
	if !strings.Contains(fmt.Sprint(manifest["data"]), "web_search") {
		t.Fatalf("install manifest missing tools: %v", manifest["data"])
	}

	// ---- Tokens & scopes ----
	tok, _ := e.do(t, "POST", "/api/v1/tokens", map[string]any{"name": "read-only", "scopes": []any{"read"}}, "", 201)
	readToken := tok["data"].(map[string]any)["token"].(string)
	tok2, _ := e.do(t, "POST", "/api/v1/tokens", map[string]any{"name": "full", "scopes": []any{"read", "write", "mcp"}}, "", 201)
	writeToken := tok2["data"].(map[string]any)["token"].(string)
	// read token can read but not write
	e.do(t, "GET", "/api/v1/prompts?pageSize=1", nil, readToken, 200)
	e.do(t, "POST", "/api/v1/prompts", map[string]any{"categoryId": catID, "slug": "nope", "title": "x"}, readToken, 403)
	// MCP over HTTP with read token: no write tools
	toolsRead := mcpListTools(t, e, readToken)
	if containsTool(toolsRead, "prompts.write") {
		t.Fatal("read token must not see write tools")
	}
	if !containsTool(toolsRead, "prompts.read") {
		t.Fatal("read token must see read tools")
	}
	// MCP with write token: write tools present
	toolsWrite := mcpListTools(t, e, writeToken)
	if !containsTool(toolsWrite, "prompts.write") {
		t.Fatal("write token must see write tools")
	}
	// MCP call a read tool
	callRes := mcpCallTool(t, e, readToken, "prompts.read", map[string]any{"keyword": "问候"})
	if !strings.Contains(callRes, "问候") {
		t.Fatalf("mcp read call failed: %s", callRes)
	}
	// Revoke read token -> immediate failure
	rev, _ := e.do(t, "DELETE", "/api/v1/tokens/"+fmt.Sprint(int64(tok["data"].(map[string]any)["id"].(float64))), nil, "", 200)
	_ = rev
	e.do(t, "GET", "/api/v1/prompts?pageSize=1", nil, readToken, 401)

	// ---- CLI installer units ----
	testCLIInstaller(t)
}

func containsTool(tools []map[string]any, name string) bool {
	for _, t := range tools {
		if t["name"] == name {
			return true
		}
	}
	return false
}

func mcpListTools(t *testing.T, e *env, token string) []map[string]any {
	t.Helper()
	resp := mcpRaw(t, e, token, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{},
	})
	if resp["error"] != nil {
		t.Fatalf("tools/list error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	out := []map[string]any{}
	for _, x := range tools {
		out = append(out, x.(map[string]any))
	}
	return out
}

func mcpCallTool(t *testing.T, e *env, token, name string, args map[string]any) string {
	t.Helper()
	resp := mcpRaw(t, e, token, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if resp["error"] != nil {
		t.Fatalf("tools/call error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("no content in call result")
	}
	text := content[0].(map[string]any)["text"].(string)
	return text
}

func mcpRaw(t *testing.T, e *env, token string, payload map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", e.baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("mcp http status %d: %s", resp.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("mcp response not json: %s", raw)
	}
	return out
}

func validSkillZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("SKILL.md")
	f.Write([]byte("---\nname: demo-skill\ndescription: 演示\n---\n# Demo\n")) //nolint:errcheck
	f2, _ := zw.Create("scripts/run.sh")
	f2.Write([]byte("echo hi\n")) //nolint:errcheck
	zw.Close()                    //nolint:errcheck
	return buf.Bytes()
}

func traversalZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("SKILL.md")
	f.Write([]byte("---\nname: evil\n---\n")) //nolint:errcheck
	f2, _ := zw.Create("../evil.txt")
	f2.Write([]byte("bad")) //nolint:errcheck
	zw.Close()              //nolint:errcheck
	return buf.Bytes()
}

func uploadSkill(t *testing.T, e *env, zipData []byte, fields map[string]string) map[string]any {
	t.Helper()
	return uploadSkillExpect(t, e, zipData, fields, 201)
}

func uploadSkillExpect(t *testing.T, e *env, zipData []byte, fields map[string]string, want int) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile("file", "skill.zip")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(zipData) //nolint:errcheck
	mw.Close()        //nolint:errcheck
	req, _ := http.NewRequest("POST", e.baseURL+"/api/v1/skills/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("skill upload: status %d want %d: %s", resp.StatusCode, want, raw)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
