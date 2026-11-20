package input

import (
	"cmp"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// loginTemplate is a skeleton to edit, not data to use as is: the example
// files do not exist.
const loginTemplate = `# Context name. The --context flag wins. Defaults to "default".
context: default
# Base URL of the Agent Identity server.
server: https://agentid.example.com
tls:
  # CA bundle: "@file" or inline PEM.
  ca_cert: "@./ca.pem"
  # Skip TLS verification. For local development only.
  insecure_skip_verify: false
oauth:
  # OIDC issuer URL.
  issuer: https://idp.example.com
  # OAuth client identifier.
  client_id: my-client-id
  # authorization_code | client_credentials
  grant_type: authorization_code
  # none | private_key_jwt | client_secret_post
  auth_type: none
  # Private JWK for private_key_jwt: "@file" or inline JSON.
  private_jwk: "@./jwk_private.json"
  # Client secret: "@file" or inline value.
  client_secret: "@./secret.txt"
  # Loopback redirect URI. Defaults to an ephemeral port on 127.0.0.1.
  redirect_uri: http://127.0.0.1
  # Requested scopes.
  scopes: [openid, offline_access]
`

// LoginTemplate returns the login skeleton as "yaml" (the default, with
// comments) or "json".
func LoginTemplate(format string) ([]byte, error) {
	switch format {
	case "", "yaml":
		return []byte(loginTemplate), nil
	case "json":
		var l Login
		if err := yaml.Unmarshal([]byte(loginTemplate), &l); err != nil {
			return nil, err
		}
		b, err := json.MarshalIndent(l, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}
	return nil, i18n.Wrap(ErrInvalidInput, "input.unknown_format", map[string]any{"Format": format})
}

// Login is the login payload: the context name plus the environment to save.
type Login struct {
	Context             string `yaml:"context,omitempty" json:"context,omitempty"`
	profile.Environment `yaml:",inline"`
}

// InlineSecrets lists the properties that hold an inline secret, so the
// command layer can warn about them.
func (l *Login) InlineSecrets() []string {
	var props []string
	if l.OAuth.PrivateJWK != "" && !l.OAuth.PrivateJWK.IsFile() {
		props = append(props, "oauth.private_jwk")
	}
	if l.OAuth.ClientSecret != "" && !l.OAuth.ClientSecret.IsFile() {
		props = append(props, "oauth.client_secret")
	}
	return props
}

// ParseLogin parses the login payload. Context is contextFlag, then the
// payload's context, then "default". It checks required values and that
// referenced files exist, and makes their paths absolute. It does not read
// their contents.
func ParseLogin(d *Data, contextFlag string) (*Login, error) {
	var l Login
	if err := decode(d.Bytes, &l); err != nil {
		return nil, err
	}

	if u, err := url.Parse(l.Server); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, i18n.Wrap(ErrInvalidInput, "input.invalid_server", nil)
	}
	if strings.TrimSpace(l.OAuth.Issuer) == "" {
		return nil, i18n.Wrap(ErrInvalidInput, "input.required", map[string]any{"Property": "oauth.issuer"})
	}
	if strings.TrimSpace(l.OAuth.ClientID) == "" {
		return nil, i18n.Wrap(ErrInvalidInput, "input.required", map[string]any{"Property": "oauth.client_id"})
	}

	l.Context = cmp.Or(contextFlag, l.Context, profile.DefaultContext)
	if err := profile.ValidateName(l.Context); err != nil {
		return nil, err
	}

	if err := resolve("tls.ca_cert", &l.TLS.CACert, d.BaseDir); err != nil {
		return nil, err
	}
	if err := resolve("oauth.private_jwk", &l.OAuth.PrivateJWK, d.BaseDir); err != nil {
		return nil, err
	}
	if err := resolve("oauth.client_secret", &l.OAuth.ClientSecret, d.BaseDir); err != nil {
		return nil, err
	}
	return &l, nil
}

// resolve makes a file reference absolute and checks that the file exists. An
// inline value is left as written.
func resolve[S ~string](prop string, v *S, baseDir string) error {
	r, err := profile.Source(*v).Resolve(baseDir)
	if err != nil {
		return err
	}
	if r.IsFile() {
		slog.Debug("file reference resolved", "property", prop, "path", r.Path())
	}
	*v = S(r)
	return nil
}
