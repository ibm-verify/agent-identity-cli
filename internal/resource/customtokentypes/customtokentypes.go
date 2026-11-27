package customtokentypes

import (
	"github.com/ibm-verify/agent-identity-cli/internal/client/oauth2"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

const template = `# Value clients send as subject_token_type / actor_token_type.
# Unique and immutable. Must not collide with a built-in token type. Required.
name: urn:example:token-type:partner
# Discriminator of the token type. Immutable. Only the JWT type is supported.
type: urn:ietf:params:oauth:token-type:jwt
# Trusted issuer. Must match the iss claim of incoming tokens. Required.
issuer: https://idp.example.com
# How incoming tokens are validated. Required.
# Exactly one of jwksUri (https only) and jwks (inline, public keys only).
validate:
  jwksUri: https://idp.example.com/jwks
  # jwks:
  #   keys:
  #     - kid: key-1
  #       kty: RSA
  #       n: "..."
  #       e: AQAB
  # At least one signing algorithm: RS256, RS384, RS512, PS256, PS384, PS512,
  # ES256, ES384 or ES512.
  allowedSigningAlgs:
    - RS256
  # Whitelist of key IDs. Empty means any key of the JWKS is accepted.
  # allowedKids: []
  # Check the jti claim for replay.
  validateJti: false
  # Clock skew tolerance in seconds.
  timeSkew: 30
  # Wider validity window for iat and exp in seconds. 0 means the default (1 hour).
  toleranceWindow: 3600
# Links the token subject to a local identity. Active only if searchBy is set.
# identityMapping:
#   subjectId: sub
#   searchBy: username
#   entityType: agent
#   userRegistryConnection: verify_saas_user
# Name of a mapping rule, instead of or as a fallback to identityMapping.
# identityMappingRule: my-mapping-rule
`

// Resource returns the definition of custom-token-types, which is all that
// cmd.NewRoot needs. The API has no PATCH, so the patch verb is not offered.
// The ID is the server-derived base64url of the name; the name is the
// identifier users know.
func Resource() *resource.Resource {
	d := &resource.Def[oauth2.CustomTokenType]{
		Name:     "custom-token-types",
		Singular: "custom-token-type",
		Aliases:  []string{"custom-token-type", "custom_token_types", "custom_token_type"},
		Template: template,
		Columns: []resource.Column[oauth2.CustomTokenType]{
			{Header: "TYPE", Value: func(t *oauth2.CustomTokenType) string { return string(t.Type) }},
			{Header: "ISSUER", Value: func(t *oauth2.CustomTokenType) string { return t.Issuer }},
		},
		ListFlags: resource.ListSupport{Limit: true, Page: true, Search: true},

		ID:     func(t *oauth2.CustomTokenType) string { return resource.Str(t.Id) },
		NameOf: func(t *oauth2.CustomTokenType) string { return t.Name },

		// The server derives the ID and assigns the timestamps, so input that
		// carries them (the output of get) is accepted and they are dropped.
		Strip: func(t *oauth2.CustomTokenType) {
			t.Id, t.CreatedAt, t.UpdatedAt = nil, nil, nil
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Delete:  remove,
	}
	return d.Resource()
}
