package projects

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const keyHistorySecret = "k:history-cursor"

// HistoryEntry shadows the internal global sequence. Consumers must never
// serialize Change alone as an externally visible project-history record.
type HistoryEntry struct {
	Change
	Seq string `json:"seq"`
}

func newHistorySecret() (string, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key[:]), nil
}

// Existing history receives one durable private key before the store opens.
// New tenants write their key in the same batch as their first change.
func (s *Store) ensureHistorySecrets() error {
	keys := map[string]string{}
	batch := s.kept.Batch()
	for tenant, t := range s.tenants {
		if len(t.history) > 0 && t.cursorSecret == "" {
			key, err := newHistorySecret()
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(key)
			batch.Put(tenant, keyHistorySecret, raw)
			keys[tenant] = key
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if err := batch.Commit(); err != nil {
		return err
	}
	for tenant, key := range keys {
		s.tenants[tenant].cursorSecret = key
	}
	return nil
}

func historyID(secret, tenant, project string, seq int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(tenant + "\x00" + project + "\x00" + strconv.FormatInt(seq, 10)))
	return "h_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:24])
}

// OpaqueHistory resolves cursors only against this project's bounded retained
// history. A token for another project cannot reveal a global sequence or gap.
func (s *Store) OpaqueHistory(tenantName, name string, limit int, before string) ([]HistoryEntry, string, error) {
	if before != "" && (len(before) != 34 || !strings.HasPrefix(before, "h_")) {
		return nil, "", fmt.Errorf("%w: invalid history cursor", ErrInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []HistoryEntry{}
	t := s.tenants[tenantName]
	if t == nil {
		if before != "" {
			return nil, "", fmt.Errorf("%w: history cursor is not retained", ErrInvalid)
		}
		return out, "", nil
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	limit = min(limit, MaxPage)
	var sequence int64
	if before != "" {
		for _, c := range t.history {
			if c.Project == name && hmac.Equal([]byte(historyID(t.cursorSecret, tenantName, name, c.Seq)), []byte(before)) {
				sequence = c.Seq
				break
			}
		}
		if sequence == 0 {
			return nil, "", fmt.Errorf("%w: history cursor is not retained; refresh the history", ErrInvalid)
		}
	}
	for i := len(t.history) - 1; i >= 0; i-- {
		c := t.history[i]
		if c.Project != name || sequence > 0 && c.Seq >= sequence {
			continue
		}
		if len(out) == limit {
			return out, out[len(out)-1].Seq, nil
		}
		out = append(out, HistoryEntry{Change: c, Seq: historyID(t.cursorSecret, tenantName, name, c.Seq)})
	}
	return out, "", nil
}

func validHistorySecret(secret string) bool {
	key, err := base64.RawURLEncoding.DecodeString(secret)
	return err == nil && len(key) == 32
}
