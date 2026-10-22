package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/urmzd/fiber/pkg/address"
)

// A /fix grant signed before the rename must still verify after it, so the
// grant's protocol domain is kept.
func TestTheMutationDomainIsKept(t *testing.T) {
	secret := []byte("s")
	c := &Connector{cfg: Config{MutationSecret: secret}}
	target := address.Address{Workspace: "acme", Path: []string{"o", "r", "1"}, Params: map[string]string{"head_sha": "abc"}}
	fields, _ := json.Marshal([]string{"warp.github.mutation.v1", "acme", "o/r/1", "abc", "", "", "", "", ""})
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(fields)
	if !hmac.Equal(c.mutationMAC(target), mac.Sum(nil)) {
		t.Fatal("the mutation grant is no longer signed under warp.github.mutation.v1")
	}
}
