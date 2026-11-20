package oauth

import (
	"context"
	"errors"
	"log/slog"

	auth "github.com/ibm-verify/verify-auth-go"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// Token returns a usable token without any user interaction. It reuses a valid
// cached token, refreshes an expired one, and mints a new one for
// client_credentials. When none of that is possible it returns ErrLoginRequired.
//
// If the new token cannot be cached, it is returned together with the error.
func (p *Provider) Token(ctx context.Context) (*profile.Token, error) {
	grant, err := p.validate()
	if err != nil {
		return nil, err
	}

	cached, status, err := profile.LookupToken(p.Context, p.env(), p.now())
	if err != nil {
		return nil, err
	}
	if status == profile.TokenValid {
		return cached, nil
	}

	if status == profile.TokenExpired && cached.RefreshToken != "" {
		tok, err := p.refresh(ctx, cached)
		switch {
		case err == nil:
			return p.save(tok)
		case errors.Is(err, errNoSecret):
			slog.Info("cannot refresh: no client secret available", "context", p.Context)
		case isInvalidGrant(err):
			slog.Info("refresh token rejected by the server", "context", p.Context)
		default:
			return nil, err
		}
	}

	if grant != grantClientCredentials {
		return nil, p.loginRequired()
	}

	tok, err := p.mint(ctx)
	if errors.Is(err, errNoSecret) {
		return nil, p.loginRequired()
	}
	if err != nil {
		return nil, err
	}
	return p.save(tok)
}

// refresh exchanges the cached refresh token. The previous refresh token is
// kept when the server does not rotate it.
func (p *Provider) refresh(ctx context.Context, cached *profile.Token) (*profile.Token, error) {
	hc, err := p.httpClient()
	if err != nil {
		return nil, err
	}
	cfg, err := discover(ctx, hc, p.env().OAuth.Issuer, false)
	if err != nil {
		return nil, err
	}
	ca, err := p.clientAuth(cfg)
	if err != nil {
		return nil, err
	}
	client, err := p.sdkClient(cfg, ca, "")
	if err != nil {
		return nil, err
	}

	tr, err := client.TokenWithRefreshToken(ctx, cached.RefreshToken, nil)
	if err != nil {
		return nil, err
	}
	slog.Info("token refreshed", "context", p.Context)
	return p.tokenFromResponse(tr, cached.RefreshToken), nil
}

// mint runs the client_credentials grant. It returns errNoSecret when a
// client_secret_post context has no secret.
func (p *Provider) mint(ctx context.Context) (*profile.Token, error) {
	hc, err := p.httpClient()
	if err != nil {
		return nil, err
	}
	cfg, err := discover(ctx, hc, p.env().OAuth.Issuer, false)
	if err != nil {
		return nil, err
	}
	ca, err := p.clientAuth(cfg)
	if err != nil {
		return nil, err
	}
	client, err := p.sdkClient(cfg, ca, "")
	if err != nil {
		return nil, err
	}

	tr, err := client.TokenWithAPIClient(ctx, nil)
	if err != nil {
		return nil, err
	}
	return p.tokenFromResponse(tr, ""), nil
}

// isInvalidGrant reports whether the authorization server rejected the grant,
// as opposed to the request failing for another reason.
func isInvalidGrant(err error) bool {
	var ae *auth.Error
	return errors.As(err, &ae) && ae.Code == "invalid_grant"
}
