package app

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/spec"
)

func connectionAuthorization(ctx context.Context, system spec.System, keys credref.Resolver) (*connectionauth.Config, error) {
	settings := system.Spec.Connections
	if settings == nil {
		return nil, nil
	}
	encoded, err := keys.Resolve(ctx, settings.EncryptionKey.Ref)
	if err != nil {
		return nil, errors.New("connection encryption key unavailable")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("connection encryption key must be 32 random bytes encoded as base64")
	}
	config := &connectionauth.Config{Key: key, RedirectURL: settings.RedirectURL, Providers: platform.DefaultConnectionProviders()}
	for _, name := range []string{"drive", "slack", "bigquery"} {
		if _, ok := settings.Clients[name]; ok {
			config.Providers = append(config.Providers, platform.NativeConnectionProvider(name))
		}
	}
	for i := range config.Providers {
		provider := &config.Providers[i]
		client, ok := settings.Clients[provider.Name]
		if !ok {
			continue
		}
		provider.ClientID = client.ClientID
		if provider.AuthorizationURL == "" {
			provider.Issuer, provider.Scopes = client.Issuer, client.Scopes
		}
		if client.ClientSecret.Set() {
			provider.ClientSecret, err = keys.Resolve(ctx, client.ClientSecret.Ref)
			if err != nil {
				return nil, errors.New("connection client secret unavailable")
			}
		}
		if client.SigningSecret.Set() {
			provider.SigningSecret, err = keys.Resolve(ctx, client.SigningSecret.Ref)
			if err != nil {
				return nil, errors.New("connection signing secret unavailable")
			}
		}
	}
	return config, nil
}
