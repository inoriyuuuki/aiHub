package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to the AIHub HTTP server.
type Client struct {
	ServerURL string
	Token     string
	HTTP      *http.Client
}

// NewClient builds a client from config; it errors when not logged in.
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil || !cfg.HasToken() {
		return nil, fmt.Errorf("未登录，请先运行 aihub login")
	}
	return &Client{
		ServerURL: strings.TrimRight(cfg.ServerURL, "/"),
		Token:     cfg.Token,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// DoJSON performs an HTTP request with an optional JSON body and decodes the
// JSON response into out.
func (c *Client) DoJSON(method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.ServerURL+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("%s %s: %s", method, path, msg)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Login authenticates against the server and returns a populated config.
func (c *Client) Login(server, username, password string, ttlHours int) (*Config, error) {
	base := strings.TrimRight(server, "/")
	req := map[string]any{"username": username, "password": password}
	if ttlHours > 0 {
		req["ttlHours"] = ttlHours
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("登录失败: %s", msg)
	}
	var out struct {
		Token     string     `json:"token"`
		TokenID   int64      `json:"tokenId"`
		ExpiresAt *time.Time `json:"expiresAt"`
		Scopes    []string   `json:"scopes"`
		User      struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.Token == "" {
		return nil, fmt.Errorf("登录响应缺少 token")
	}
	cfg := &Config{
		ServerURL:    base,
		Username:     username,
		Scopes:       out.Scopes,
		Token:        out.Token,
		TokenID:      out.TokenID,
		TokenExpires: out.ExpiresAt,
	}
	if out.User.Username != "" {
		cfg.Username = out.User.Username
	}
	return cfg, nil
}

// UploadSkill uploads a zipped skill with metadata using multipart/form-data.
func (c *Client) UploadSkill(zipPath string, meta map[string]string) (map[string]any, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("file", filepath.Base(zipPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, err
	}
	for k, v := range meta {
		if v != "" {
			_ = w.WriteField(k, v)
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.ServerURL+"/api/v1/skills/publish", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("发布失败: %s", msg)
	}
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *Client) http() *http.Client {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 60 * time.Second}
	}
	return c.HTTP
}
