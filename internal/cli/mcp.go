package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SecretSource resolves secret values when installing MCP profiles.
type SecretSource interface {
	Lookup(key string) (string, bool)
}

// EnvFileSecretSource reads secrets from a dotenv-style file.
type EnvFileSecretSource struct {
	values map[string]string
}

// NewEnvFileSecretSource parses an env file into a SecretSource.
func NewEnvFileSecretSource(envFile string) (SecretSource, error) {
	vals, err := parseEnvFile(envFile)
	if err != nil {
		return nil, err
	}
	return &EnvFileSecretSource{values: vals}, nil
}

// Lookup returns a secret value by key.
func (s *EnvFileSecretSource) Lookup(key string) (string, bool) {
	v, ok := s.values[key]
	return v, ok
}

// InstallProfile writes an MCP profile JSON into the Codex mcp directory.
// Placeholders like ${KEY} in the manifest env are resolved from secrets or
// the process environment.
func InstallProfile(dirs *CodexDirs, scope string, manifest *MCPInstallManifest, secrets SecretSource) error {
	if manifest == nil || manifest.Profile == "" {
		return fmt.Errorf("manifest 缺少 profile")
	}
	env := map[string]string{}
	for k, v := range manifest.Env {
		val := v
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			key := strings.TrimSuffix(strings.TrimPrefix(v, "${"), "}")
			switch {
			case secrets != nil:
				if sv, ok := secrets.Lookup(key); ok {
					val = sv
				} else if ev := os.Getenv(key); ev != "" {
					val = ev
				}
			case os.Getenv(key) != "":
				val = os.Getenv(key)
			}
		}
		env[k] = val
	}
	profile := map[string]any{
		"name":    manifest.Name,
		"command": manifest.Command,
		"args":    manifest.Args,
		"env":     env,
	}
	if err := os.MkdirAll(dirs.Global, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dirs.Global, manifest.Profile+".json"), data, 0o644)
}

// RemoveProfile removes an MCP profile with backup.
func RemoveProfile(dirs *CodexDirs, scope, profile string) error {
	if profile == "" {
		return fmt.Errorf("需要 Profile slug")
	}
	src := filepath.Join(dirs.Global, profile+".json")
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Profile %s 未安装", profile)
		}
		return err
	}
	backup := filepath.Join(backupDir(dirs), "mcp-"+profile+"-"+time.Now().Format("20060102-150405")+".json")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	return os.Rename(src, backup)
}

// ServeStdioMCP runs a stdio MCP server (JSON-RPC 2.0 over stdin/stdout),
// used by Codex via `aihub mcp serve`.
func ServeStdioMCP(cfg *Config, logger *slog.Logger) error {
	enc := json.NewEncoder(os.Stdout)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // ignore malformed frames
		}
		method, _ := msg["method"].(string)
		id, hasID := msg["id"]
		switch method {
		case "initialize":
			sendMCPResult(enc, id, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "aihub", "version": "1.0.0"},
			})
		case "ping":
			if hasID {
				sendMCPResult(enc, id, map[string]any{})
			}
		case "tools/list":
			sendMCPResult(enc, id, map[string]any{
				"tools": []map[string]any{
					{
						"name":        "aihub_status",
						"description": "返回 AIHub 登录状态与服务器信息",
						"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
					},
					{
						"name":        "aihub_search_skills",
						"description": "列出本机已安装的 AIHub Skills",
						"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
					},
				},
			})
		case "tools/call":
			params, _ := msg["params"].(map[string]any)
			name, _ := params["name"].(string)
			text := callMCPTool(name, cfg)
			content := []map[string]any{{"type": "text", "text": text}}
			if hasID {
				sendMCPResult(enc, id, map[string]any{"content": content})
			}
		default:
			if hasID {
				sendMCPError(enc, id, -32601, "Method not found: "+method)
			}
		}
	}
	return sc.Err()
}

func callMCPTool(name string, cfg *Config) string {
	switch name {
	case "aihub_status":
		if cfg != nil && cfg.HasToken() {
			return fmt.Sprintf("已登录 %s（用户 %s，scopes: %s）", cfg.ServerURL, cfg.Username, strings.Join(cfg.Scopes, ","))
		}
		return "未登录"
	case "aihub_search_skills":
		dirs, err := ResolveCodexDirs("global", "")
		if err != nil {
			return "错误: " + err.Error()
		}
		entries, err := os.ReadDir(dirs.SkillsDir("global"))
		if err != nil {
			return "无已安装 Skills"
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		if len(names) == 0 {
			return "无已安装 Skills"
		}
		return strings.Join(names, "\n")
	default:
		return "未知工具: " + name
	}
}

func sendMCPResult(enc *json.Encoder, id any, result any) {
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func sendMCPError(enc *json.Encoder, id any, code int, message string) {
	_ = enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}
