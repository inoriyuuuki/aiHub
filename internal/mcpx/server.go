package mcpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the AIHub MCP implementation version.
const Version = "1.0.0"

// BuildServer constructs an MCP server from the tool registry, filtered by
// the caller's token scopes. Read tools are always visible; write/delete tools
// are only visible when the corresponding scope is present.
func BuildServer(reg *Registry, scopes []string, logger *slog.Logger) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "aihub", Version: Version}, &mcp.ServerOptions{
		Instructions: "AIHub 资源管理：搜索提示词、Skill、专家包和 MCP 目录。",
		Logger:       logger,
	})
	for _, t := range reg.All() {
		if !ScopesAllowed(scopes, t.Group, t.Write, t.Delete) {
			continue
		}
		tool := t
		srv.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}, toolHandler(&tool, logger))
	}
	return srv
}

func toolHandler(t *ToolDef, logger *slog.Logger) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (res *mcp.CallToolResult, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				if logger != nil {
					logger.Error("mcp tool panic", "tool", t.Name, "panic", rec)
				}
				res = textResult("工具内部错误", true)
				err = nil
			}
		}()
		args := map[string]any{}
		if err := JSONArgs(req.Params.Arguments, &args); err != nil {
			return textResult("参数解析失败: "+err.Error(), true), nil
		}
		out, err := t.Handler(ctx, args)
		if err != nil {
			Log(logger, t.Name, err)
			return textResult(err.Error(), true), nil
		}
		data, err := json.Marshal(out)
		if err != nil {
			Log(logger, t.Name, err)
			return textResult("结果序列化失败", true), nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	}
}

func textResult(text string, isErr bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: isErr}
}

// Authenticator resolves token scopes for an HTTP MCP request.
type Authenticator interface {
	// AuthenticateMCP returns the effective scopes for the request, or an error.
	AuthenticateMCP(r *http.Request) ([]string, error)
}

// NewStreamableHTTPHandler wires the MCP Streamable HTTP endpoint with
// Bearer-token authorization. Unauthenticated requests get 401.
func NewStreamableHTTPHandler(reg *Registry, auth Authenticator, logger *slog.Logger) http.Handler {
	inner := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		scopes, _ := scopesFromContext(req.Context())
		return BuildServer(reg, scopes, logger)
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		Logger:       logger,
		JSONResponse: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scopes, err := auth.AuthenticateMCP(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer realm="aihub"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32001,"message":"unauthorized"},"id":null}`))
			return
		}
		inner.ServeHTTP(w, r.WithContext(withScopes(r.Context(), scopes)))
	})
}

type scopesKey struct{}

func withScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, scopesKey{}, scopes)
}

func scopesFromContext(ctx context.Context) ([]string, bool) {
	s, ok := ctx.Value(scopesKey{}).([]string)
	return s, ok
}
