// Package oauth gets access tokens for a context: it reuses the cache,
// refreshes silently, mints tokens for non-interactive grants, and runs the
// interactive authorization code + PKCE login with a loopback redirect. The
// protocol work is done by github.com/ibm-verify/verify-auth-go/oauth2.
package oauth

import "errors"

// Sentinel errors for errors.Is. The errors returned by this package wrap them
// with localized messages.
var (
	// ErrLoginRequired means no token can be produced without the user running login.
	ErrLoginRequired = errors.New("login required")

	// ErrUnsupportedGrant means the context's grant type is not supported.
	ErrUnsupportedGrant = errors.New("unsupported grant type")

	// ErrInvalidAuthConfig means the context's authentication settings are unusable.
	ErrInvalidAuthConfig = errors.New("invalid authentication settings")
)

// errNoSecret is returned internally when a client_secret_post context has no
// secret. Callers map it: Token to ErrLoginRequired, Login to
// ErrInvalidAuthConfig, and Revoke skips the call.
var errNoSecret = errors.New("no client secret available")
