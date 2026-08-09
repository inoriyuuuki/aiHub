package prompts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// allowedMIMEs whitelist upload content types by kind.
var allowedMIMEs = map[string]map[string]bool{
	"image": {
		"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/svg+xml": true,
	},
	"file": {
		"text/plain": true, "text/markdown": true, "application/json": true, "application/pdf": true,
		"application/zip": true, "application/octet-stream": true, "text/csv": true,
	},
	"attachment": {
		"text/plain": true, "text/markdown": true, "application/json": true, "application/pdf": true,
		"application/zip": true, "application/octet-stream": true, "text/csv": true, "image/png": true,
		"image/jpeg": true, "image/gif": true, "image/webp": true,
	},
}

// presignRequest asks for an upload URL.
type presignRequest struct {
	Kind     string `json:"kind"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	MIME     string `json:"mime"`
	RefType  string `json:"refType"` // prompt
	RefID    int64  `json:"refId"`
}

type presignResponse struct {
	ObjectKey string `json:"objectKey"`
	UploadURL string `json:"uploadUrl"`
	ExpiresIn int    `json:"expiresIn"`
}

// handlePresign creates an object key and a presigned PUT URL.
func (s *Service) handlePresign(w http.ResponseWriter, r *http.Request) {
	var req presignRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if req.Kind == "" {
		req.Kind = "file"
	}
	if req.Kind != "image" && req.Kind != "file" && req.Kind != "attachment" && req.Kind != "effect-file" {
		httpx.WriteError(w, httpx.ErrUnprocessable("kind 必须是 image/file/attachment/effect-file"))
		return
	}
	if req.RefType != "prompt" || req.RefID <= 0 {
		httpx.WriteError(w, httpx.ErrUnprocessable("refType 必须是 prompt 且 refId 有效"))
		return
	}
	var promptExists bool
	if err := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM prompts WHERE id=$1)`, req.RefID).Scan(&promptExists); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if !promptExists {
		httpx.WriteError(w, httpx.ErrUnprocessable("提示词不存在"))
		return
	}
	if req.Size <= 0 || req.Size > s.cfg.MaxUploadBytes {
		httpx.WriteError(w, httpx.ErrUnprocessable(fmt.Sprintf("文件大小必须在 1 到 %d 字节之间", s.cfg.MaxUploadBytes)))
		return
	}
	if req.SHA256 == "" || len(req.SHA256) != 64 {
		httpx.WriteError(w, httpx.ErrUnprocessable("必须提供文件的 SHA-256"))
		return
	}
	if _, err := hex.DecodeString(req.SHA256); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable("SHA-256 格式无效"))
		return
	}
	if req.Filename == "" || strings.ContainsAny(req.Filename, "/\\") {
		httpx.WriteError(w, httpx.ErrUnprocessable("文件名无效"))
		return
	}
	mime := strings.ToLower(req.MIME)
	if mime == "" {
		mime = "application/octet-stream"
	}
	// Object key: refType/{refId}/{hash16}-{filename}
	key := fmt.Sprintf("%s/%d/%s-%s", req.RefType, req.RefID, req.SHA256[:16], req.Filename)
	url, err := s.store.PresignPut(r.Context(), key, mime, 15*time.Minute)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, presignResponse{ObjectKey: key, UploadURL: url, ExpiresIn: 900})
}

// handleConfirm verifies the uploaded object and records the asset.
func (s *Service) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ObjectKey string `json:"objectKey"`
		Kind      string `json:"kind"`
		Filename  string `json:"filename"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
		MIME      string `json:"mime"`
		RefType   string `json:"refType"`
		RefID     int64  `json:"refId"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if req.Kind == "" {
		req.Kind = "file"
	}
	if req.RefType != "prompt" || req.RefID <= 0 {
		httpx.WriteError(w, httpx.ErrUnprocessable("refType 必须是 prompt 且 refId 有效"))
		return
	}
	// The object key must match the server-issued format so clients cannot
	// attach arbitrary objects.
	expected := fmt.Sprintf("%s/%d/%s-%s", req.RefType, req.RefID, strings.ToLower(req.SHA256)[:16], req.Filename)
	if req.ObjectKey != expected {
		httpx.WriteError(w, httpx.ErrUnprocessable("objectKey 与预签名请求不一致"))
		return
	}
	info, err := s.store.Stat(r.Context(), req.ObjectKey)
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable("对象不存在或无法访问: "+err.Error()))
		return
	}
	// Verify size.
	if info.Size != req.Size {
		s.cleanupObject(r.Context(), req.ObjectKey)
		httpx.WriteError(w, httpx.ErrUnprocessable(fmt.Sprintf("大小校验失败: 期望 %d 实际 %d", req.Size, info.Size)))
		return
	}
	// Verify SHA-256.
	ok, err := s.verifySHA256(r.Context(), req.ObjectKey, req.SHA256)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if !ok {
		s.cleanupObject(r.Context(), req.ObjectKey)
		httpx.WriteError(w, httpx.ErrUnprocessable("SHA-256 校验失败"))
		return
	}
	// Verify MIME whitelist + magic bytes so client-claimed types cannot smuggle
	// HTML/SVG/scripts into the object store.
	mime := strings.ToLower(req.MIME)
	if mime == "" {
		mime = "application/octet-stream"
	}
	allowed := allowedMIMEs[req.Kind]
	if allowed != nil && !allowed[mime] {
		s.cleanupObject(r.Context(), req.ObjectKey)
		httpx.WriteError(w, httpx.ErrUnprocessable("不允许的 MIME 类型: "+mime))
		return
	}
	detected, derr := s.sniffContentType(r.Context(), req.ObjectKey)
	if derr != nil {
		httpx.WriteError(w, derr)
		return
	}
	if !mimeCompatible(mime, detected) {
		s.cleanupObject(r.Context(), req.ObjectKey)
		httpx.WriteError(w, httpx.ErrUnprocessable("文件内容与声明的 MIME 类型不一致"))
		return
	}
	var id int64
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO assets (object_key, size, sha256, mime, filename, kind, ref_type, ref_id, ref_version_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULL) RETURNING id`,
		req.ObjectKey, req.Size, req.SHA256, mime, req.Filename, req.Kind, req.RefType, req.RefID).Scan(&id)
	if db.IsUniqueViolation(err) {
		// Already confirmed; return existing.
		var existing int64
		_ = s.db.QueryRow(r.Context(), `SELECT id FROM assets WHERE object_key=$1`, req.ObjectKey).Scan(&existing)
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": existing, "objectKey": req.ObjectKey})
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "objectKey": req.ObjectKey})
}

// handleAssetURL returns a presigned GET URL for an asset.
func (s *Service) handleAssetURL(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var key string
	if err := s.db.QueryRow(r.Context(), `SELECT object_key FROM assets WHERE id=$1`, id).Scan(&key); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("附件不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	url, err := s.store.PresignGet(r.Context(), key, 15*time.Minute)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"url": url, "objectKey": key})
}

// cleanupObject removes an object that failed validation (best effort).
func (s *Service) cleanupObject(ctx context.Context, key string) {
	_ = s.store.Delete(ctx, key)
}

// sniffContentType reads the first 512 bytes to detect the real content type.
func (s *Service) sniffContentType(ctx context.Context, key string) (string, error) {
	rd, _, err := s.store.Get(ctx, key)
	if err != nil {
		return "", httpx.WrapError(err)
	}
	defer rd.Close()
	buf := make([]byte, 512)
	n, _ := io.ReadFull(rd, buf)
	return http.DetectContentType(buf[:n]), nil
}

// mimeCompatible reports whether a detected type is acceptable for a claimed
// MIME. octet-stream is a wildcard fallback; otherwise types must share a
// major category.
func mimeCompatible(claimed, detected string) bool {
	claimed = strings.ToLower(claimed)
	detected = strings.ToLower(detected)
	if claimed == "application/octet-stream" || detected == "application/octet-stream" {
		return true
	}
	if claimed == detected {
		return true
	}
	c1 := strings.SplitN(claimed, "/", 2)[0]
	d1 := strings.SplitN(detected, "/", 2)[0]
	return c1 == d1
}

// verifySHA256 streams the object and compares its digest.
func (s *Service) verifySHA256(ctx context.Context, key, want string) (bool, error) {
	rd, _, err := s.store.Get(ctx, key)
	if err != nil {
		return false, httpx.WrapError(err)
	}
	defer rd.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rd); err != nil {
		return false, httpx.WrapError(err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == strings.ToLower(want), nil
}

var _ = json.Valid
var _ = filepath.Clean
