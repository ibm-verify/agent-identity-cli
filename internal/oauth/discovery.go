package oauth

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	oauth2 "github.com/ibm-verify/verify-auth-go/oauth2"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

// discover fetches the server metadata and checks it. The issuer must match
// the configured one, and the endpoints the grant needs must be present.
func discover(ctx context.Context, hc *http.Client, issuer string, needAuthorizationEndpoint bool) (*oauth2.Configuration, error) {
	issuer = strings.TrimRight(issuer, "/")

	cfg, err := oauth2.Discover(ctx, issuer, hc)
	if err != nil {
		return nil, i18n.Wrap(err, "auth.discovery_failed", map[string]any{"Issuer": issuer, "Error": err.Error()})
	}

	if reported := strings.TrimRight(cfg.Issuer, "/"); reported != issuer {
		return nil, i18n.Errorf("auth.discovery_issuer_mismatch", map[string]any{"Configured": issuer, "Reported": reported})
	}
	if cfg.TokenEndpoint == "" {
		return nil, i18n.Errorf("auth.discovery_missing_endpoint", map[string]any{"Endpoint": "token_endpoint"})
	}
	if needAuthorizationEndpoint && cfg.AuthorizationEndpoint == "" {
		return nil, i18n.Errorf("auth.discovery_missing_endpoint", map[string]any{"Endpoint": "authorization_endpoint"})
	}

	slog.Debug("discovered server metadata",
		"issuer", issuer,
		"token_endpoint", safeURL(cfg.TokenEndpoint),
		"authorization_endpoint", safeURL(cfg.AuthorizationEndpoint),
		"revocation_endpoint", safeURL(cfg.RevocationEndpoint))
	return cfg, nil
}

// safeURL returns scheme, host and path only, so no query string reaches a log.
func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + u.Path
}
