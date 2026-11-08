package connectionauth

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestConnectionProbesVerifyProviderIdentityAndSanitizeRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, body, team string
		healthy          bool
	}{{"slack", `{"ok":true,"team_id":"T12345"}`, "T12345", true}, {"slack", `{"ok":true,"team_id":"T99999"}`, "T12345", false}, {"slack", `{"ok":false,"error":"secret-token"}`, "T12345", false}, {"drive", `{"kind":"drive#about"}`, "", true}, {"drive", `{"error":"secret-token"}`, "", false}, {"github", `{"total_count":0,"repositories":[]}`, "", true}} {
		t.Run(tc.name+tc.body, func(t *testing.T) {
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer secret-token" {
					t.Fatal("missing credential")
				}
				return response(tc.body), nil
			})}
			err := ProbeNative(context.Background(), tc.name, "secret-token", tc.team, client)
			if (err == nil) != tc.healthy {
				t.Fatal("incorrect health", err)
			}
			if err != nil && !errors.Is(err, ErrReconnect) {
				t.Fatal("refusal leaked provider output")
			}
		})
	}
	requests := 0
	client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		requests++
		r := response("")
		r.StatusCode = 302
		r.Header.Set("Location", "https://attacker.test/token")
		return r, nil
	})}
	if err := ProbeNative(context.Background(), "drive", "secret-token", "", client); !errors.Is(err, ErrReconnect) || requests != 1 {
		t.Fatal("followed credential redirect", requests, err)
	}
}
