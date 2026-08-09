package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/aihub/aihub/internal/mcpx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolInfo mirrors the server's tool registry listing.
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Write       bool           `json:"write"`
	Delete      bool           `json:"delete"`
	Group       string         `json:"group"`
}

// FetchTools downloads the server tool registry.
func (c *Client) FetchTools() ([]ToolInfo, error) {
	var tools []ToolInfo
	if err := c.DoJSON("GET", "/api/v1/mcp/tools", nil, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

// ServeStdioMCP starts a local stdio MCP server proxying the AIHub REST API.
// Write/delete tools are included only when the token has the corresponding
// scopes.
func ServeStdioMCP(cfg *Config, logger *slog.Logger) error {
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	tools, err := client.FetchTools()
	if err != nil {
		return fmt.Errorf("获取工具清单失败: %w", err)
	}
	reg := mcpx.NewRegistry()
	scopeSet := map[string]bool{}
	for _, s := range cfg.Scopes {
		scopeSet[s] = true
	}
	for _, t := range tools {
		if !mcpx.ScopesAllowed(cfg.Scopes, t.Group, t.Write, t.Delete) {
			continue
		}
		tool := t
		reg.Add(mcpx.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Group:       t.Group,
			Write:       t.Write,
			Delete:      t.Delete,
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return dispatchREST(ctx, client, tool, args)
			},
		})
	}
	_ = scopeSet
	srv := mcpx.BuildServer(reg, cfg.Scopes, logger)
	return srv.Run(context.Background(), &mcp.StdioTransport{})
}

// dispatchREST maps an MCP tool call to REST API calls.
func dispatchREST(ctx context.Context, client *Client, tool ToolInfo, args map[string]any) (any, error) {
	switch tool.Name {
	case "projects.read":
		q := url.Values{}
		if v, ok := args["keyword"].(string); ok && v != "" {
			q.Set("keyword", v)
		}
		if v, ok := args["archived"].(bool); ok {
			q.Set("archived", strconv.FormatBool(v))
		}
		q.Set("page", strconv.Itoa(intArg(args, "page", 1)))
		q.Set("pageSize", strconv.Itoa(intArg(args, "pageSize", 20)))
		var out any
		return out, client.DoJSON("GET", "/api/v1/projects?"+q.Encode(), nil, &out)

	case "prompts.read":
		if slug, ok := args["slug"].(string); ok && slug != "" {
			var out any
			return out, client.DoJSON("GET", "/api/v1/prompts/resolve?slug="+url.PathEscape(slug)+"&"+queryProject(args), nil, &out)
		}
		q := url.Values{}
		if v, ok := args["keyword"].(string); ok && v != "" {
			q.Set("keyword", v)
		}
		if v, ok := args["category"].(string); ok && v != "" {
			q.Set("category", v)
		}
		if v, ok := args["tag"].(string); ok && v != "" {
			q.Set("tag", v)
		}
		if v, ok := args["status"].(string); ok && v != "" {
			q.Set("status", v)
		}
		q.Set("page", strconv.Itoa(intArg(args, "page", 1)))
		q.Set("pageSize", strconv.Itoa(intArg(args, "pageSize", 20)))
		var out any
		return out, client.DoJSON("GET", "/api/v1/prompts?"+q.Encode(), nil, &out)

	case "prompts.render":
		slug, _ := args["slug"].(string)
		id, err := client.resolvePromptID(slug, stringArg(args, "project"))
		if err != nil {
			return nil, err
		}
		body := map[string]any{"values": args["values"]}
		if v, ok := args["version"].(float64); ok {
			body["version"] = int(v)
		}
		var out any
		return out, client.DoJSON("POST", "/api/v1/prompts/"+strconv.FormatInt(id, 10)+"/render", body, &out)

	case "prompts.write":
		action, _ := args["action"].(string)
		slug, _ := args["slug"].(string)
		switch action {
		case "create":
			var out any
			return out, client.DoJSON("POST", "/api/v1/prompts", map[string]any{
				"slug": slug, "title": args["title"], "categoryId": 0, "content": args["content"],
			}, &out)
		case "update":
			id, err := client.resolvePromptID(slug, "")
			if err != nil {
				return nil, err
			}
			var out any
			return out, client.DoJSON("PATCH", "/api/v1/prompts/"+strconv.FormatInt(id, 10), map[string]any{
				"slug": slug, "title": args["title"], "categoryId": 0, "content": args["content"],
			}, &out)
		case "publish":
			id, err := client.resolvePromptID(slug, "")
			if err != nil {
				return nil, err
			}
			var out any
			return out, client.DoJSON("POST", "/api/v1/prompts/"+strconv.FormatInt(id, 10)+"/publish", map[string]any{"summary": args["summary"]}, &out)
		}
		return nil, fmt.Errorf("未知 prompts.write action")

	case "prompts.delete":
		slug, _ := args["slug"].(string)
		id, err := client.resolvePromptID(slug, "")
		if err != nil {
			return nil, err
		}
		var out any
		return out, client.DoJSON("DELETE", "/api/v1/prompts/"+strconv.FormatInt(id, 10), nil, &out)

	case "skills.read":
		if manifest, _ := args["manifest"].(bool); manifest {
			var out any
			return out, client.DoJSON("GET", "/api/v1/skills/install-manifest?slug="+url.PathEscape(stringArg(args, "slug"))+"&project="+url.QueryEscape(stringArg(args, "project")), nil, &out)
		}
		if slug, ok := args["slug"].(string); ok && slug != "" {
			var out any
			return out, client.DoJSON("GET", "/api/v1/skills/resolve?slug="+url.PathEscape(slug), nil, &out)
		}
		q := url.Values{}
		if v, ok := args["keyword"].(string); ok && v != "" {
			q.Set("keyword", v)
		}
		q.Set("page", strconv.Itoa(intArg(args, "page", 1)))
		q.Set("pageSize", strconv.Itoa(intArg(args, "pageSize", 20)))
		var out any
		return out, client.DoJSON("GET", "/api/v1/skills?"+q.Encode(), nil, &out)

	case "skills.write":
		// Publishing binary skill packages via MCP stdio is not supported;
		// users should use `aihub skill publish` or the Web uploader.
		return nil, fmt.Errorf("CLI MCP 下 skills.write 请使用 aihub skill publish 或 Web 上传")

	case "skills.delete":
		slug, _ := args["slug"].(string)
		var out struct {
			ID int64 `json:"id"`
		}
		if err := client.DoJSON("GET", "/api/v1/skills/resolve?slug="+url.PathEscape(slug), nil, &out); err != nil {
			return nil, err
		}
		var res any
		return res, client.DoJSON("DELETE", "/api/v1/skills/"+strconv.FormatInt(out.ID, 10), nil, &res)

	case "experts.read":
		if manifest, _ := args["manifest"].(bool); manifest {
			var out any
			return out, client.DoJSON("GET", "/api/v1/expert-packs/install-manifest?slug="+url.PathEscape(stringArg(args, "slug")), nil, &out)
		}
		q := url.Values{}
		if v, ok := args["keyword"].(string); ok && v != "" {
			q.Set("keyword", v)
		}
		q.Set("page", strconv.Itoa(intArg(args, "page", 1)))
		q.Set("pageSize", strconv.Itoa(intArg(args, "pageSize", 20)))
		var out any
		return out, client.DoJSON("GET", "/api/v1/expert-packs?"+q.Encode(), nil, &out)

	case "experts.write", "experts.delete", "mcp_catalog.read", "mcp_catalog.write", "mcp_catalog.delete":
		return nil, fmt.Errorf("工具 %s 请在 Web 界面或专用 CLI 命令中使用", tool.Name)
	}
	return nil, fmt.Errorf("未知工具: %s", tool.Name)
}

func (c *Client) resolvePromptID(slug, project string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	q := ""
	if project != "" {
		q = "&project=" + url.QueryEscape(project)
	}
	if err := c.DoJSON("GET", "/api/v1/prompts/resolve?slug="+url.PathEscape(slug)+q, nil, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

func queryProject(args map[string]any) string {
	if v, ok := args["project"].(string); ok && v != "" {
		return "project=" + url.QueryEscape(v)
	}
	return ""
}

func intArg(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

var _ = json.Marshal
var _ = strings.TrimSpace
