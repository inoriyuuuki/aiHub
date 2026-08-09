// Command aihub is the local AIHub CLI: publishes/installs skills and expert
// packs, manages Codex MCP profiles, and serves a local stdio MCP server.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aihub/aihub/internal/cli"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := run(os.Args[1:], logger); err != nil {
		fmt.Fprintln(os.Stderr, "aihub:", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "login":
		return cmdLogin(rest)
	case "logout":
		cfg, err := cli.LoadConfig()
		if err != nil {
			return err
		}
		if cfg.HasToken() && cfg.TokenID > 0 {
			// Revoke the token server-side before clearing local state.
			if client, cerr := cli.NewClient(cfg); cerr == nil {
				var out any
				_ = client.DoJSON("DELETE", "/api/v1/tokens/"+strconv.FormatInt(cfg.TokenID, 10), nil, &out)
			}
		}
		cfg.Logout()
		return cfg.Save()
	case "status":
		return cmdStatus(rest)
	case "token":
		return cmdToken(rest)
	case "skill":
		return cmdSkill(rest)
	case "expert":
		return cmdExpert(rest)
	case "mcp":
		return cmdMCP(rest, logger)
	case "help", "-h", "--help":
		usage()
		return nil
	case "version":
		fmt.Println("aihub 1.0.0")
		return nil
	}
	return fmt.Errorf("未知命令 %q（运行 aihub help 查看帮助）", cmd)
}

func usage() {
	fmt.Print(`AIHub CLI - 管理提示词、Skill、专家包与 MCP 配置

用法:
  aihub login [--server URL] [--username NAME] [--password PASS] [--ttl-hours N]
  aihub logout
  aihub status
  aihub token list|create|revoke <id>
  aihub skill publish <dir> [--slug S] [--name N] [--description D] [--category C] [--tags A,B] [--changelog M]
  aihub skill install <slug> [--scope global|project] [--dir PROJECT_DIR] [--project PROJECT_SLUG]
  aihub skill update <slug> [--scope ...] [--dir ...]
  aihub skill remove <slug> [--scope ...] [--dir ...]
  aihub skill restore <slug> [--scope ...] [--dir ...]
  aihub expert install <slug> [--scope ...] [--dir ...]
  aihub expert remove <slug> [--scope ...] [--dir ...]
  aihub mcp install-profile <profile> [--scope ...] [--dir ...] [--env-file FILE]
  aihub mcp remove-profile <profile> [--scope ...] [--dir ...]
  aihub mcp serve [--write]
  aihub sync
  aihub version
`)
}

func cmdLogin(args []string) error {
	fl := parseFlags(args)
	server := fl.get("server", os.Getenv("AIHUB_SERVER"))
	if server == "" {
		server = "http://localhost:8080"
	}
	username := fl.get("username", os.Getenv("AIHUB_USERNAME"))
	password := fl.get("password", os.Getenv("AIHUB_PASSWORD"))
	ttl, _ := strconv.Atoi(fl.get("ttl-hours", "0"))
	if username == "" || password == "" {
		return fmt.Errorf("需要 --username 和 --password（或环境变量 AIHUB_USERNAME / AIHUB_PASSWORD）")
	}
	c := &cli.Client{}
	cfg, err := c.Login(server, username, password, ttl)
	if err != nil {
		return err
	}
	fmt.Printf("已登录 %s（用户 %s，scopes: %s）\n", cfg.ServerURL, cfg.Username, strings.Join(cfg.Scopes, ","))
	return nil
}

func cmdStatus(args []string) error {
	cfg, err := cli.LoadConfig()
	if err != nil {
		return err
	}
	if !cfg.HasToken() {
		fmt.Println("未登录")
		return nil
	}
	fmt.Printf("服务器: %s\n用户: %s\nScopes: %s\n", cfg.ServerURL, cfg.Username, strings.Join(cfg.Scopes, ","))
	if cfg.TokenExpires != nil {
		fmt.Printf("Token 过期: %s\n", cfg.TokenExpires.Format("2006-01-02 15:04:05"))
	}
	// Report installed skills.
	dirs, _ := cli.ResolveCodexDirs("global", "")
	reportInstalled("全局 Skills", dirs.SkillsDir("global"))
	reportInstalled("全局 MCP", dirs.Global)
	if pd, _ := cli.ResolveCodexDirs("project", "."); pd.Project != "" {
		reportInstalled("项目 Skills", pd.SkillsDir("project"))
	}
	return nil
}

func reportInstalled(label, dir string) {
	names, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(names) == 0 {
		fmt.Printf("%s: 无\n", label)
		return
	}
	items := []string{}
	for _, n := range names {
		if strings.HasPrefix(filepath.Base(n), ".") {
			continue
		}
		items = append(items, filepath.Base(n))
	}
	fmt.Printf("%s: %s\n", label, strings.Join(items, ", "))
}

func cmdToken(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: aihub token list|create|revoke <id>")
	}
	cfg, err := cli.LoadConfig()
	if err != nil {
		return err
	}
	client, err := cli.NewClient(cfg)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		var out []map[string]any
		if err := client.DoJSON("GET", "/api/v1/tokens", nil, &out); err != nil {
			return err
		}
		for _, t := range out {
			id, _ := t["id"].(float64)
			name, _ := t["name"].(string)
			fmt.Printf("%d\t%s\n", int64(id), name)
		}
		return nil
	case "create":
		fl := parseFlags(args[1:])
		name := fl.get("name", "cli-extra")
		scopes := strings.Split(fl.get("scopes", "read"), ",")
		ttl, _ := strconv.Atoi(fl.get("ttl-hours", "0"))
		req := map[string]any{"name": name, "scopes": scopes}
		if ttl > 0 {
			req["ttlHours"] = ttl
		}
		var out map[string]any
		if err := client.DoJSON("POST", "/api/v1/tokens", req, &out); err != nil {
			return err
		}
		tok, _ := out["token"].(string)
		fmt.Println(tok)
		return nil
	case "revoke":
		if len(args) < 2 {
			return fmt.Errorf("需要 token id")
		}
		var out any
		return client.DoJSON("DELETE", "/api/v1/tokens/"+args[1], nil, &out)
	}
	return fmt.Errorf("未知 token 子命令")
}

func cmdSkill(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: aihub skill publish|install|update|remove|restore")
	}
	sub := args[0]
	fl := parseFlags(args[1:])
	cfg, err := cli.LoadConfig()
	if err != nil {
		return err
	}
	client, err := cli.NewClient(cfg)
	if err != nil {
		return err
	}
	scope := fl.get("scope", "global")
	projectDir := fl.get("dir", ".")
	switch sub {
	case "publish":
		if len(args) < 2 {
			return fmt.Errorf("需要 Skill 目录")
		}
		zipData, err := cli.ZipDir(args[1])
		if err != nil {
			return err
		}
		tmp := filepath.Join(os.TempDir(), "aihub-skill-"+slugFromDir(args[1])+".zip")
		if err := os.WriteFile(tmp, zipData, 0o600); err != nil {
			return err
		}
		defer os.Remove(tmp) //nolint:errcheck
		meta := map[string]string{
			"slug":        fl.get("slug", ""),
			"name":        fl.get("name", ""),
			"description": fl.get("description", ""),
			"category":    fl.get("category", ""),
			"tags":        fl.get("tags", ""),
			"changelog":   fl.get("changelog", ""),
		}
		res, err := client.UploadSkill(tmp, meta)
		if err != nil {
			return err
		}
		slug, _ := res["slug"].(string)
		fmt.Printf("已发布 Skill %s\n", slug)
		return nil
	case "install", "update":
		if len(args) < 2 {
			return fmt.Errorf("需要 Skill slug")
		}
		slug := args[1]
		project := fl.get("project", "")
		var manifest cli.SkillManifest
		path := "/api/v1/skills/install-manifest?slug=" + slug
		if project != "" {
			path += "&project=" + project
		}
		if err := client.DoJSON("GET", path, nil, &manifest); err != nil {
			return err
		}
		dirs, err := cli.ResolveCodexDirs(scope, projectDir)
		if err != nil {
			return err
		}
		if err := cli.InstallSkill(client, dirs, scope, &manifest); err != nil {
			return err
		}
		fmt.Printf("已安装 Skill %s (版本 %d, %s)\n", slug, manifest.Version.Version, manifest.Source)
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("需要 Skill slug")
		}
		dirs, err := cli.ResolveCodexDirs(scope, projectDir)
		if err != nil {
			return err
		}
		if err := cli.RemoveSkill(dirs, scope, args[1]); err != nil {
			return err
		}
		fmt.Printf("已移除 Skill %s（已备份，可用 aihub skill restore 恢复）\n", args[1])
		return nil
	case "restore":
		if len(args) < 2 {
			return fmt.Errorf("需要 Skill slug")
		}
		dirs, err := cli.ResolveCodexDirs(scope, projectDir)
		if err != nil {
			return err
		}
		if err := cli.RestoreSkill(dirs, scope, args[1]); err != nil {
			return err
		}
		fmt.Printf("已恢复 Skill %s\n", args[1])
		return nil
	}
	return fmt.Errorf("未知 skill 子命令")
}

func cmdExpert(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: aihub expert install|remove")
	}
	sub := args[0]
	fl := parseFlags(args[1:])
	cfg, err := cli.LoadConfig()
	if err != nil {
		return err
	}
	client, err := cli.NewClient(cfg)
	if err != nil {
		return err
	}
	scope := fl.get("scope", "global")
	projectDir := fl.get("dir", ".")
	switch sub {
	case "install", "update":
		if len(args) < 2 {
			return fmt.Errorf("需要专家包 slug")
		}
		var manifest cli.ExpertManifest
		if err := client.DoJSON("GET", "/api/v1/expert-packs/install-manifest?slug="+args[1], nil, &manifest); err != nil {
			return err
		}
		dirs, err := cli.ResolveCodexDirs(scope, projectDir)
		if err != nil {
			return err
		}
		if err := cli.InstallExpertPack(client, dirs, scope, &manifest); err != nil {
			return err
		}
		fmt.Printf("已安装专家包 %s（协调 Skill + %d 个成员）\n", args[1], len(manifest.Manifest.Members))
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("需要专家包 slug")
		}
		dirs, err := cli.ResolveCodexDirs(scope, projectDir)
		if err != nil {
			return err
		}
		if err := cli.RemoveExpertPack(dirs, scope, args[1]); err != nil {
			return err
		}
		fmt.Printf("已移除专家包 %s\n", args[1])
		return nil
	}
	return fmt.Errorf("未知 expert 子命令")
}

func cmdMCP(args []string, logger *slog.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: aihub mcp install-profile|remove-profile|serve")
	}
	sub := args[0]
	fl := parseFlags(args[1:])
	cfg, err := cli.LoadConfig()
	if err != nil {
		return err
	}
	client, err := cli.NewClient(cfg)
	if err != nil {
		return err
	}
	scope := fl.get("scope", "global")
	projectDir := fl.get("dir", ".")
	switch sub {
	case "install-profile":
		if len(args) < 2 {
			return fmt.Errorf("需要 Profile slug")
		}
		var manifest cli.MCPInstallManifest
		if err := client.DoJSON("GET", "/api/v1/mcp/install-manifest?profile="+args[1], nil, &manifest); err != nil {
			return err
		}
		dirs, err := cli.ResolveCodexDirs(scope, projectDir)
		if err != nil {
			return err
		}
		var secrets cli.SecretSource
		if envFile := fl.get("env-file", ""); envFile != "" {
			secrets, err = cli.NewEnvFileSecretSource(envFile)
			if err != nil {
				return err
			}
		}
		if err := cli.InstallProfile(dirs, scope, &manifest, secrets); err != nil {
			return err
		}
		fmt.Printf("已安装 MCP Profile %s 到 %s 配置\n", args[1], scope)
		return nil
	case "remove-profile":
		if len(args) < 2 {
			return fmt.Errorf("需要 Profile slug")
		}
		dirs, err := cli.ResolveCodexDirs(scope, projectDir)
		if err != nil {
			return err
		}
		if err := cli.RemoveProfile(dirs, scope, args[1]); err != nil {
			return err
		}
		fmt.Printf("已移除 MCP Profile %s\n", args[1])
		return nil
	case "serve":
		return cli.ServeStdioMCP(cfg, logger)
	}
	return fmt.Errorf("未知 mcp 子命令")
}

func slugFromDir(dir string) string {
	return filepath.Base(filepath.Clean(dir))
}

// flagSet is a tiny flag parser.
type flagSet map[string]string

func parseFlags(args []string) flagSet {
	fl := flagSet{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			key := strings.TrimPrefix(a, "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				fl[key] = args[i+1]
				i++
			} else {
				fl[key] = "true"
			}
		}
	}
	return fl
}

func (f flagSet) get(key, def string) string {
	if v, ok := f[key]; ok && v != "" {
		return v
	}
	return def
}

var _ = json.Marshal
