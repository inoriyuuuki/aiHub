package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// TokenRecord is a server-side API token.
type TokenRecord struct {
	ID        int64     `json:"id"`
	Token     string    `json:"token"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// TokenStore persists API tokens to disk (hashed at rest).
type TokenStore struct {
	mu     sync.Mutex
	path   string
	nextID int64
	byID   map[int64]*TokenRecord
	byHash map[string]*TokenRecord
}

// LoadTokenStore loads the token store from the data dir (or starts empty).
func LoadTokenStore(dataDir string) (*TokenStore, error) {
	ts := &TokenStore{
		path:   filepath.Join(dataDir, "tokens.json"),
		byID:   map[int64]*TokenRecord{},
		byHash: map[string]*TokenRecord{},
	}
	data, err := os.ReadFile(ts.path)
	if err != nil {
		if os.IsNotExist(err) {
			return ts, nil
		}
		return nil, err
	}
	var recs []*TokenRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, err
	}
	for _, rec := range recs {
		if rec.ID >= ts.nextID {
			ts.nextID = rec.ID + 1
		}
		ts.byID[rec.ID] = rec
		ts.byHash[hashToken(rec.Token)] = rec
	}
	return ts, nil
}

// Create issues a new token (raw value returned only here).
func (ts *TokenStore) Create(name string, scopes []string, ttlHours int) *TokenRecord {
	raw := randomToken()
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	if ttlHours > 0 {
		expires = now.Add(time.Duration(ttlHours) * time.Hour)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.nextID++
	rec := &TokenRecord{
		ID:        ts.nextID,
		Token:     raw,
		Name:      name,
		Scopes:    scopes,
		CreatedAt: now,
		ExpiresAt: expires,
	}
	ts.byID[rec.ID] = rec
	ts.byHash[hashToken(raw)] = rec
	ts.persistLocked()
	return rec
}

// Verify returns the record for a presented token, or nil.
func (ts *TokenStore) Verify(raw string) *TokenRecord {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	rec := ts.byHash[hashToken(raw)]
	if rec == nil {
		return nil
	}
	if time.Now().After(rec.ExpiresAt) {
		delete(ts.byID, rec.ID)
		delete(ts.byHash, hashToken(raw))
		ts.persistLocked()
		return nil
	}
	return rec
}

// List returns all active tokens (raw values masked).
func (ts *TokenStore) List() []*TokenRecord {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]*TokenRecord, 0, len(ts.byID))
	for _, rec := range ts.byID {
		if time.Now().After(rec.ExpiresAt) {
			continue
		}
		clone := *rec
		clone.Token = "••••" + hex.EncodeToString([]byte(rec.Token))[:8]
		out = append(out, &clone)
	}
	return out
}

// Revoke deletes a token by id.
func (ts *TokenStore) Revoke(id int64) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	rec, ok := ts.byID[id]
	if !ok {
		return false
	}
	delete(ts.byID, id)
	delete(ts.byHash, hashToken(rec.Token))
	ts.persistLocked()
	return true
}

func (ts *TokenStore) persistLocked() {
	recs := make([]*TokenRecord, 0, len(ts.byID))
	for _, rec := range ts.byID {
		recs = append(recs, rec)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(ts.path, data, 0o600)
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "token-" + time.Now().Format("20060102150405")
	}
	return hex.EncodeToString(b)
}
