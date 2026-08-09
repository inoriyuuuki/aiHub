package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// handleLogin authenticates the admin user and issues a token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Scopes   []string `json:"scopes,omitempty"`
		TTLHours int      `json:"ttlHours,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.Username != s.cfg.AdminUser || req.Password != s.cfg.AdminPass {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "用户名或密码错误"})
		return
	}
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = []string{"read", "write"}
	}
	rec := s.tokens.Create(req.Username, scopes, req.TTLHours)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     rec.Token,
		"tokenId":   rec.ID,
		"expiresAt": rec.ExpiresAt,
		"scopes":    rec.Scopes,
		"user":      map[string]string{"username": s.cfg.AdminUser},
	})
}

// handleTokens lists or creates tokens.
func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// CLI expects a bare JSON array of token records.
		writeJSON(w, http.StatusOK, s.tokens.List())
	case http.MethodPost:
		var req struct {
			Name     string   `json:"name"`
			Scopes   []string `json:"scopes,omitempty"`
			TTLHours int      `json:"ttlHours,omitempty"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if req.Name == "" {
			req.Name = "api"
		}
		scopes := req.Scopes
		if len(scopes) == 0 {
			scopes = []string{"read"}
		}
		rec := s.tokens.Create(req.Name, scopes, req.TTLHours)
		writeJSON(w, http.StatusCreated, map[string]any{
			"token": rec.Token, "tokenId": rec.ID, "scopes": rec.Scopes, "expiresAt": rec.ExpiresAt,
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleTokenByID revokes a token.
func (s *Server) handleTokenByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/tokens/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid token id"})
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if !s.tokens.Revoke(id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "token not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": id})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handlePublishSkill accepts a multipart upload of a zipped skill.
func (s *Server) handlePublishSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form: " + err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 file 字段"})
		return
	}
	defer file.Close()
	zipData, err := io.ReadAll(io.LimitReader(file, 128<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	slug, err := s.registry.PublishSkill(
		zipData,
		header.Filename,
		r.FormValue("slug"),
		r.FormValue("name"),
		r.FormValue("description"),
		r.FormValue("category"),
		splitTags(r.FormValue("tags")),
		r.FormValue("changelog"),
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	rec, _ := s.registry.Skill(slug)
	writeJSON(w, http.StatusCreated, map[string]any{
		"slug":    slug,
		"version": rec.Manifest.Version.Version,
	})
}

// handleSkillManifest returns an install manifest for a skill.
func (s *Server) handleSkillManifest(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("slug")
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 slug 参数"})
		return
	}
	rec, ok := s.registry.Skill(slug)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill not found: " + slug})
		return
	}
	writeJSON(w, http.StatusOK, rec.Manifest)
}

// handleExpertManifest returns an install manifest for an expert pack.
func (s *Server) handleExpertManifest(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("slug")
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 slug 参数"})
		return
	}
	m, ok := s.registry.Expert(slug)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "expert pack not found: " + slug})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleMCPManifest returns an install manifest for an MCP profile.
func (s *Server) handleMCPManifest(w http.ResponseWriter, r *http.Request) {
	profile := r.URL.Query().Get("profile")
	if profile == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 profile 参数"})
		return
	}
	m, ok := s.registry.Profile(profile)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP profile not found: " + profile})
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func splitTags(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(dst)
}
