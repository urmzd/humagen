package projects

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/state"
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

// ensureHistorySecret gives a tenant whose history was written before
// cursors were opaque its durable private key, once: two processes doing it
// at once keep the key whose write lands first.
func (s *Store) ensureHistorySecret(tenantName string) (*snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := state.Retry(func() error {
		tx, err := s.begin(tenantName)
		if err != nil {
			return err
		}
		if tx.t.cursorSecret != "" || len(tx.t.history) == 0 {
			return nil
		}
		key, err := newHistorySecret()
		if err != nil {
			return err
		}
		tx.t.cursorSecret = key
		tx.put(keyHistorySecret, key)
		return tx.commit()
	})
	if err != nil {
		return nil, err
	}
	return s.read(tenantName)
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
	snap, err := s.read(tenantName)
	if err != nil {
		return nil, "", err
	}
	if len(snap.t.history) > 0 && snap.t.cursorSecret == "" {
		if snap, err = s.ensureHistorySecret(tenantName); err != nil {
			return nil, "", err
		}
	}
	out := []HistoryEntry{}
	t := snap.t
	if len(t.history) == 0 {
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
