package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to the AIHub REST API using a bearer token.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient builds a client from config.
func NewClient(cfg *Config) (*Client, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("未配置服务器地址，请先运行 aihub login")
	}
	if !cfg.HasToken() {
		return nil, fmt.Errorf("未登录或 Token 已过期，请先运行 aihub login")
	}
	return &Client{
		BaseURL: strings.TrimRight(cfg.ServerURL, "/"),
		Token:   cfg.Token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// apiError is the server error envelope.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// do performs a JSON request and decodes the data envelope.
func (c *Client) DoJSON(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *apiError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("服务器响应无效 (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		if env.Error != nil {
			return fmt.Errorf("%s", env.Error.Message)
		}
		return fmt.Errorf("请求失败 (HTTP %d)", resp.StatusCode)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

// doRaw performs a request returning raw bytes.
func (c *Client) doRaw(method, path string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var env struct {
			Error *apiError `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		if env.Error != nil {
			return nil, fmt.Errorf("%s", env.Error.Message)
		}
		return nil, fmt.Errorf("请求失败 (HTTP %d)", resp.StatusCode)
	}
	return raw, nil
}

// Login authenticates with username/password, creates an API token via the
// session, and stores it in the CLI config.
func (c *Client) Login(serverURL, username, password string, ttlHours int) (*Config, error) {
	base := strings.TrimRight(serverURL, "/")
	httpc := &http.Client{Timeout: 30 * time.Second, Jar: newCookieJar()}
	loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := httpc.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		return nil, err
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("登录失败 (HTTP %d)", resp.StatusCode)
	}
	// Create a scoped token.
	scopes := []string{"read", "write", "delete", "mcp"}
	tokenReq := map[string]any{"name": "cli", "scopes": scopes}
	if ttlHours > 0 {
		tokenReq["ttlHours"] = ttlHours
	}
	tb, _ := json.Marshal(tokenReq)
	req, _ := http.NewRequest("POST", base+"/api/v1/tokens", bytes.NewReader(tb))
	req.Header.Set("Content-Type", "application/json")
	tresp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer tresp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(tresp.Body, 1<<20))
	if tresp.StatusCode >= 400 {
		return nil, fmt.Errorf("创建 Token 失败 (HTTP %d): %s", tresp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var tokenResp struct {
		Data struct {
			Token  string   `json:"token"`
			Scopes []string `json:"scopes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return nil, fmt.Errorf("解析 Token 响应失败")
	}
	cfg := &Config{ServerURL: base, Username: username, Token: tokenResp.Data.Token, Scopes: tokenResp.Data.Scopes, TokenCreated: time.Now()}
	if ttlHours > 0 {
		t := time.Now().Add(time.Duration(ttlHours) * time.Hour)
		cfg.TokenExpires = &t
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// UploadSkill uploads a skill zip (multipart) and returns the created skill.
func (c *Client) UploadSkill(zipPath string, meta map[string]string) (map[string]any, error) {
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range meta {
		if v == "" {
			continue
		}
		_ = mw.WriteField(k, v)
	}
	fw, err := mw.CreateFormFile("file", filepath.Base(zipPath))
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	_ = mw.Close()
	req, err := http.NewRequest("POST", c.BaseURL+"/api/v1/skills/upload", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		var env struct {
			Error *apiError `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		if env.Error != nil {
			return nil, fmt.Errorf("%s", env.Error.Message)
		}
		return nil, fmt.Errorf("上传失败 (HTTP %d)", resp.StatusCode)
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// DownloadToFile fetches a URL (presigned) and writes it to dest.
func DownloadToFile(rawURL, dest string) error {
	resp, err := http.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("下载失败 (HTTP %d)", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func newCookieJar() http.CookieJar {
	return &memJar{cookies: map[string][]*http.Cookie{}}
}

type memJar struct {
	cookies map[string][]*http.Cookie
}

func (j *memJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.cookies[u.Host] = cookies
}
func (j *memJar) Cookies(u *url.URL) []*http.Cookie {
	return j.cookies[u.Host]
}
