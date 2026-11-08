package bigquery

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// tokenTransport never lets Google client retries or redirects reuse an old
// token. Credential failures stop before the API is called and stay opaque.
type tokenTransport struct {
	source func(context.Context) (string, error)
	base   http.RoundTripper
}

func (t *tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL
	if u.Scheme != "https" || u.User != nil || u.Host != "bigquery.googleapis.com" && u.Host != "www.googleapis.com" || !strings.HasPrefix(u.Path, "/bigquery/v2/") {
		return nil, errors.New("bigquery: credential destination refused")
	}
	token, err := t.source(r.Context())
	if err != nil || token == "" {
		return nil, errors.New("bigquery: reconnect your Google account")
	}
	request := r.Clone(r.Context())
	request.Header = r.Header.Clone()
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, errors.New("bigquery: request failed")
	}
	return response, nil
}
