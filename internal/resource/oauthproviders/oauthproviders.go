package oauthproviders

import (
	"github.com/ibm-verify/agent-identity-cli/internal/client/oauth2"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

const template = `# Provider definition name. Identifier, unique and immutable.
definition_name: default
# Trusted issuer URL of the provider. Required.
issuer: https://auth.example.com
# Keystore identifier holding the JWKS signing keys/certificates.
jwks_sig_keystore: default-keystore
# Keystore identifier holding the JWKS encryption keys/certificates.
# jwks_enc_keystore: default-enc-keystore
# Token settings for access and refresh tokens.
token_settings:
  signing_algorithm: RS256
  signing_key_store: default-keystore
  signing_key_label: default-signing-key
  access_token_lifetime_seconds: 3600
  refresh_token_lifetime_seconds: 2592000
  grant_types:
    - authorization_code
    - refresh_token
    - urn:ietf:params:oauth:grant-type:token-exchange
`

// Resource returns the definition of oauth-providers, which is all that cmd.NewRoot
// needs. The API has no PATCH, so the patch verb is not offered.
func Resource() *resource.Resource {
	d := &resource.Def[oauth2.OAuthProviderResponse]{
		Name:     "oauth-providers",
		Singular: "oauth-provider",
		Aliases:  []string{"oauth-provider", "oauth_providers", "oauth_provider"},
		Template: template,
		Columns: []resource.Column[oauth2.OAuthProviderResponse]{
			{Header: "ISSUER", Value: func(p *oauth2.OAuthProviderResponse) string { return p.Issuer }},
			{Header: "SIGNING_KEY", Value: func(p *oauth2.OAuthProviderResponse) string {
				if p.TokenSettings == nil {
					return ""
				}
				return p.TokenSettings.SigningKeyLabel
			}},
			{Header: "ALGORITHM", Value: func(p *oauth2.OAuthProviderResponse) string {
				if p.TokenSettings == nil {
					return ""
				}
				return string(p.TokenSettings.SigningAlgorithm)
			}},
		},
		ListFlags: resource.ListSupport{Limit: true, Page: true, Search: true},

		ID:     func(p *oauth2.OAuthProviderResponse) string { return p.DefinitionName },
		NameOf: func(p *oauth2.OAuthProviderResponse) string { return p.DefinitionName },

		// The server assigns the version and timestamps, so input that carries them
		// is accepted and dropped.
		Strip: func(p *oauth2.OAuthProviderResponse) {
			p.CreatedAt, p.UpdatedAt, p.Version = nil, nil, nil
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Delete:  remove,
	}
	return d.Resource()
}
