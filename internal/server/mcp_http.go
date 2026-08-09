package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// handleMCPHTTP implements the Streamable HTTP MCP endpoint (/mcp).
func (s *Server) handleMCPHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON-RPC body"})
		return
	}
	method, _ := msg["method"].(string)
	id, hasID := msg["id"]

	switch method {
	case "initialize":
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "aihub-server", "version": "1.0.0"},
			},
		})
	case "ping":
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
	case "tools/list":
		tools := []map[string]any{
			{
				"name":        "server_info",
				"description": "返回 AIHub 服务器信息与注册的 Skills",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			},
			{
				"name":        "list_skills",
				"description": "列出 Hub 上已发布的 Skills",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"tools": tools}})
	case "tools/call":
		params, _ := msg["params"].(map[string]any)
		name, _ := params["name"].(string)
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"result": map[string]any{"content": []map[string]any{{"type": "text", "text": s.callHTTPTool(name)}}},
		})
	default:
		if !hasID {
			return // notification, no response
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32601, "message": "Method not found: " + method},
		})
	}
}

func (s *Server) callHTTPTool(name string) string {
	switch name {
	case "server_info":
		return "AIHub server v1.0.0, data dir: " + s.cfg.DataDir
	case "list_skills":
		recs := s.registry.ListSkills()
		if len(recs) == 0 {
			return "暂无已发布 Skills（可先通过 aihub skill publish 发布）"
		}
		names := make([]string, 0, len(recs))
		for _, rec := range recs {
			names = append(names, rec.Manifest.Slug+" (v"+strconv.Itoa(rec.Manifest.Version.Version)+")")
		}
		return strings.Join(names, "\n")
	default:
		return "未知工具: " + name
	}
}
