package oauth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// Revoke asks the authorization server to revoke the context's cached token. It
// is best effort and meant to run before the cache file is deleted on logout.
//
// The refresh token is revoked when there is one, otherwise the access token.
// Nothing happens, and no error is returned, when there is no usable cached
// token, the server has no revocation endpoint, or the client credentials are
// not available. Any other failure is returned for the caller to report.
func (p *Provider) Revoke(ctx context.Context) error {
	if _, err := p.validate(); err != nil {
		return err
	}

	cached, _, err := profile.LookupToken(p.Context, p.env(), p.now())
	if err != nil {
		return err
	}
	if cached == nil {
		return nil
	}

	token, hint := cached.AccessToken, "access_token"
	if cached.RefreshToken != "" {
		token, hint = cached.RefreshToken, "refresh_token"
	}

	ctx, cancel := context.WithTimeout(ctx, revokeTimeout)
	defer cancel()

	hc, err := p.httpClient()
	if err != nil {
		return err
	}
	cfg, err := discover(ctx, hc, p.env().OAuth.Issuer, false)
	if err != nil {
		return err
	}
	if cfg.RevocationEndpoint == "" {
		slog.Info("token not revoked: the server has no revocation endpoint", "context", p.Context)
		return nil
	}

	ca, err := p.clientAuth(cfg)
	if errors.Is(err, errNoSecret) {
		slog.Info("token not revoked: no client secret available", "context", p.Context)
		return nil
	}
	if err != nil {
		return err
	}
	client, err := p.sdkClient(cfg, ca, "")
	if err != nil {
		return err
	}

	if err := client.RevokeToken(ctx, token, hint); err != nil {
		return err
	}
	slog.Info("token revoked", "context", p.Context, "token_type", hint)
	return nil
}
