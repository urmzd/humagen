package app

import (
	"context"
	"errors"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/githubinstall"
	"github.com/urmzd/humagen/pkg/spec"
)

// githubInstallation resolves only the operator's declared credentials. Client
// records retain the references, and the callback cannot supply or replace them.
func githubInstallation(ctx context.Context, system spec.System, keys credref.Resolver) (*githubinstall.Config, error) {
	settings := system.Spec.Connections
	if settings == nil || settings.GitHub == nil {
		return nil, nil
	}
	g := settings.GitHub
	if !g.ClientSecret.Set() || !g.PrivateKey.Set() || !g.WebhookSecret.Set() {
		return nil, errors.New("GitHub installation credentials must use deployment references")
	}
	config := &githubinstall.Config{AppID: g.AppID, AppSlug: g.AppSlug, ClientID: g.ClientID, RedirectURL: settings.RedirectURL, PrivateKeyRef: g.PrivateKey.Ref, SigningSecretRef: g.WebhookSecret.Ref}
	var err error
	if config.ClientSecret, err = keys.Resolve(ctx, g.ClientSecret.Ref); err != nil {
		return nil, errors.New("GitHub client secret unavailable")
	}
	if config.PrivateKey, err = keys.Resolve(ctx, g.PrivateKey.Ref); err != nil {
		return nil, errors.New("GitHub private key unavailable")
	}
	if config.SigningSecret, err = keys.Resolve(ctx, g.WebhookSecret.Ref); err != nil {
		return nil, errors.New("GitHub webhook secret unavailable")
	}
	return config, nil
}
