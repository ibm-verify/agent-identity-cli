package oauth

import (
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	oauth2 "github.com/ibm-verify/verify-auth-go/oauth2"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// EnvClientSecret supplies the client secret of a client_secret_post context
// that has none saved. A secret in the context always wins. The variable is
// never written to disk.
const EnvClientSecret = "IBM_AGENTID_CLIENT_SECRET"

const (
	grantAuthorizationCode = "authorization_code"
	grantClientCredentials = "client_credentials"

	authNone             = "none"
	authClientSecretPost = "client_secret_post"
	authPrivateKeyJWT    = "private_key_jwt"

	defaultLoginTimeout = 5 * time.Minute
)

// revokeTimeout bounds a revocation call so an unreachable server cannot hang
// logout. It is a variable so tests can shorten it.
var revokeTimeout = 10 * time.Second

// Provider gets tokens for one context. The zero values of the optional fields
// are the production defaults; tests replace them.
type Provider struct {
	// Context is the context name, used for the cache file and in messages.
	Context string

	// Env is the resolved context.
	Env *profile.Environment

	// HTTPClient is used for every request. The default is NewHTTPClient(Env.TLS).
	HTTPClient *http.Client

	// Out receives prompts. The default is os.Stderr. Stdout is never written.
	Out io.Writer

	// OpenBrowser opens a URL. The default runs the platform opener.
	OpenBrowser func(url string) error

	// Now is the clock. The default is time.Now.
	Now func() time.Time

	hc *http.Client
}

func (p *Provider) env() *profile.Environment {
	if p.Env == nil {
		return &profile.Environment{}
	}
	return p.Env
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Provider) out() io.Writer {
	if p.Out != nil {
		return p.Out
	}
	return os.Stderr
}

func (p *Provider) opener() func(string) error {
	if p.OpenBrowser != nil {
		return p.OpenBrowser
	}
	return openBrowser
}

func (p *Provider) httpClient() (*http.Client, error) {
	if p.HTTPClient != nil {
		return p.HTTPClient, nil
	}
	if p.hc == nil {
		hc, err := NewHTTPClient(p.env().TLS)
		if err != nil {
			return nil, err
		}
		p.hc = hc
	}
	return p.hc, nil
}

// secret returns the client secret: the one saved in the context, read from its
// file when it is a file reference, and otherwise IBM_AGENTID_CLIENT_SECRET. A
// trailing newline is trimmed. An empty result means there is none.
func (p *Provider) secret() (string, error) {
	if src := p.env().OAuth.ClientSecret; src != "" {
		data, err := src.Read()
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	return os.Getenv(EnvClientSecret), nil
}

func (p *Provider) grant() string {
	if g := p.env().OAuth.GrantType; g != "" {
		return g
	}
	return grantAuthorizationCode
}

func (p *Provider) authType() string {
	if a := p.env().OAuth.AuthType; a != "" {
		return a
	}
	return authNone
}

// validate checks the context's settings and returns its grant type.
func (p *Provider) validate() (string, error) {
	oc := p.env().OAuth

	grant := p.grant()
	if grant != grantAuthorizationCode && grant != grantClientCredentials {
		return "", i18n.Wrap(ErrUnsupportedGrant, "auth.grant_unsupported", map[string]any{"Grant": grant})
	}
	if oc.Issuer == "" {
		return "", p.settingMissing("oauth.issuer")
	}
	if oc.ClientID == "" {
		return "", p.settingMissing("oauth.client_id")
	}

	switch auth := p.authType(); auth {
	case authNone:
		if grant == grantClientCredentials {
			return "", i18n.Wrap(ErrInvalidAuthConfig, "auth.client_auth_required", map[string]any{"Grant": grant})
		}
	case authClientSecretPost:
	case authPrivateKeyJWT:
		if oc.PrivateJWK == "" {
			return "", p.settingMissing("oauth.private_jwk")
		}
	default:
		return "", i18n.Wrap(ErrInvalidAuthConfig, "auth.auth_type_unknown", map[string]any{"AuthType": auth})
	}
	return grant, nil
}

func (p *Provider) settingMissing(setting string) error {
	return i18n.Wrap(ErrInvalidAuthConfig, "auth.setting_missing", map[string]any{"Name": p.Context, "Setting": setting})
}

// clientAuth builds the SDK's client authentication. It returns errNoSecret
// when a client_secret_post context has no secret.
func (p *Provider) clientAuth(cfg *oauth2.Configuration) (oauth2.ClientAuth, error) {
	oc := p.env().OAuth

	switch p.authType() {
	case authClientSecretPost:
		secret, err := p.secret()
		if err != nil {
			return nil, err
		}
		if secret == "" {
			return nil, errNoSecret
		}
		return &oauth2.ClientSecretPost{ClientID: oc.ClientID, ClientSecret: secret}, nil

	case authPrivateKeyJWT:
		key, err := loadKey(oc.PrivateJWK)
		if err != nil {
			return nil, err
		}
		return &oauth2.PrivateKeyJWT{Configuration: cfg, ClientID: oc.ClientID, PrivateKeyJWK: key}, nil

	default:
		return &oauth2.ClientSecretPost{ClientID: oc.ClientID}, nil
	}
}

// sdkClient builds the SDK client for a request. redirect may be empty.
func (p *Provider) sdkClient(cfg *oauth2.Configuration, auth oauth2.ClientAuth, redirect string) (*oauth2.Client, error) {
	hc, err := p.httpClient()
	if err != nil {
		return nil, err
	}
	opts := []oauth2.ClientOption{oauth2.WithHTTPClient(hc)}
	if scopes := p.env().OAuth.Scopes; len(scopes) > 0 {
		opts = append(opts, oauth2.WithScopes(scopes...))
	}
	if redirect != "" {
		opts = append(opts, oauth2.WithRedirectURL(redirect))
	}
	return oauth2.NewClient(auth, cfg, opts...)
}

// loginRequired is ErrLoginRequired with the message that fits the context.
func (p *Provider) loginRequired() error {
	data := map[string]any{"Name": p.Context}
	if p.authType() == authClientSecretPost {
		return i18n.Wrap(ErrLoginRequired, "auth.login_required_secret", data)
	}
	return i18n.Wrap(ErrLoginRequired, "auth.login_required", data)
}

// secretMissing is the error for a client_secret_post context with no secret.
func (p *Provider) secretMissing() error {
	return i18n.Wrap(ErrInvalidAuthConfig, "auth.secret_missing", map[string]any{"Name": p.Context})
}

// tokenFromResponse stamps an SDK response for the context.
func (p *Provider) tokenFromResponse(tr *oauth2.TokenResponse, keepRefresh string) *profile.Token {
	refresh := tr.RefreshToken
	if refresh == "" {
		refresh = keepRefresh
	}
	return profile.NewToken(p.env(), tr.AccessToken, refresh, tr.TokenType, tr.ExpiresIn, p.now())
}

// save caches the token. A failure is returned with the token still in hand,
// so a caller that only wants to print it is not left empty-handed.
func (p *Provider) save(tok *profile.Token) (*profile.Token, error) {
	if err := profile.SaveToken(p.Context, tok); err != nil {
		return tok, i18n.Wrap(err, "auth.cache_write_failed", map[string]any{"Error": err.Error()})
	}
	return tok, nil
}
