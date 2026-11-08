package githubinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/connectionauth"
	githubapi "github.com/urmzd/humagen/pkg/integrations/github"
)

type account struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type installation struct {
	ID          int64      `json:"id"`
	AppID       int64      `json:"app_id"`
	AppSlug     string     `json:"app_slug"`
	Account     account    `json:"account"`
	SuspendedAt *time.Time `json:"suspended_at"`
}

func (m *Manager) verify(ctx context.Context, code, id string) (Installation, error) {
	form := url.Values{"client_id": {m.cfg.ClientID}, "client_secret": {m.cfg.ClientSecret}, "code": {code}, "redirect_uri": {m.cfg.RedirectURL}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.GitHubURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return Installation{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if err := m.json(req, &token); err != nil {
		return Installation{}, err
	}
	if token.Error != "" || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "bearer") || strings.ContainsAny(token.AccessToken, "\r\n") {
		return Installation{}, connectionauth.ErrInvalid
	}
	var user account
	if err := m.get(ctx, "/user", token.AccessToken, &user); err != nil {
		return Installation{}, err
	}
	if user.ID <= 0 || user.Login == "" {
		return Installation{}, connectionauth.ErrInvalid
	}
	var accessible installation
	// Bound the query independently of provider totals and untrusted Link URLs.
	for page := 1; page <= 10; page++ {
		var list struct {
			Installations []installation `json:"installations"`
		}
		if err := m.get(ctx, "/user/installations?per_page=100&page="+strconv.Itoa(page), token.AccessToken, &list); err != nil {
			return Installation{}, err
		}
		for _, item := range list.Installations {
			if strconv.FormatInt(item.ID, 10) == id {
				accessible = item
				break
			}
		}
		if accessible.ID != 0 || len(list.Installations) < 100 {
			break
		}
	}
	if accessible.ID <= 0 || accessible.AppID <= 0 || strconv.FormatInt(accessible.AppID, 10) != m.cfg.AppID || accessible.Account.ID <= 0 || accessible.SuspendedAt != nil {
		return Installation{}, connectionauth.ErrInvalid
	}
	jwt, err := githubapi.SignAppJWT(m.cfg.AppID, m.key, m.now())
	if err != nil {
		return Installation{}, err
	}
	var installed installation
	if err := m.get(ctx, "/app/installations/"+id, jwt, &installed); err != nil {
		return Installation{}, err
	}
	if installed.ID != accessible.ID || installed.AppID != accessible.AppID || installed.AppSlug != m.cfg.AppSlug || installed.Account.ID != accessible.Account.ID || !strings.EqualFold(installed.Account.Login, accessible.Account.Login) || installed.SuspendedAt != nil || !slug.MatchString(strings.ToLower(installed.Account.Login)) {
		return Installation{}, connectionauth.ErrInvalid
	}
	return Installation{ID: id, AppID: m.cfg.AppID, AppSlug: m.cfg.AppSlug, AccountID: strconv.FormatInt(installed.Account.ID, 10), AccountLogin: installed.Account.Login, UserID: strconv.FormatInt(user.ID, 10)}, nil
}

func (m *Manager) get(ctx context.Context, path, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.cfg.APIURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return m.json(req, out)
}

func (m *Manager) json(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: GitHub could not be reached", connectionauth.ErrReconnect)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: GitHub returned status %d", connectionauth.ErrInvalid, resp.StatusCode)
	}
	const maxResponse = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse || json.Unmarshal(raw, out) != nil {
		return fmt.Errorf("%w: GitHub returned an invalid response", connectionauth.ErrInvalid)
	}
	return nil
}
