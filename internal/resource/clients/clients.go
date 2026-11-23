package clients

import (
	"github.com/ibm-verify/agent-identity-cli/internal/client/oauth2mgmt"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

const template = `# Name of the client, shown to the end user.
client_name: My Application
# Whether the client is enabled.
enabled: true
# Grant types the client may use: urn:ietf:params:oauth:grant-type:token-exchange
# or urn:openid:params:grant-type:ciba.
grant_types:
  - urn:ietf:params:oauth:grant-type:token-exchange
  - urn:openid:params:grant-type:ciba
# How the client authenticates at the token endpoint: client_secret_basic,
# client_secret_post, private_key_jwt, tls_client_auth or default.
token_endpoint_auth_method: client_secret_basic
# Scopes the client may request, as a space-delimited string.
scope: openid profile
# Free-form labels for grouping and filtering.
tags: [production]
`

// strs converts a list of string-typed enum values for display.
func strs[S ~string](p *[]S) string {
	if p == nil {
		return ""
	}
	s := make([]string, len(*p))
	for i, v := range *p {
		s[i] = string(v)
	}
	return resource.Join(s)
}

// Resource returns the definition of clients, which is all that cmd.NewRoot
// needs. The API has no PATCH, so the patch verb is not offered.
func Resource() *resource.Resource {
	d := &resource.Def[oauth2mgmt.Client]{
		Name:     "clients",
		Singular: "client",
		Aliases:  []string{"client"},
		Template: template,
		Columns: []resource.Column[oauth2mgmt.Client]{
			{Header: "CLIENT_NAME", Value: func(c *oauth2mgmt.Client) string { return resource.Str(c.ClientName) }},
			{Header: "GRANT_TYPES", Value: func(c *oauth2mgmt.Client) string { return strs(c.GrantTypes) }},
			{Header: "ENABLED", Value: func(c *oauth2mgmt.Client) string { return resource.Bool(c.Enabled) }},
			{Header: "TAGS", Value: func(c *oauth2mgmt.Client) string { return strs(c.Tags) }},
		},
		ListFlags: resource.ListSupport{Search: true},

		ID:     func(c *oauth2mgmt.Client) string { return resource.Str(c.ClientId) },
		NameOf: func(c *oauth2mgmt.Client) string { return resource.Str(c.ClientName) },

		// The server assigns these, so input that carries them (the output of
		// get) is accepted and they are dropped. Client has no client_secret
		// property, so there is nothing to mask.
		Strip: func(c *oauth2mgmt.Client) {
			c.ClientIdIssuedAt, c.ClientSecretExpiresAt = nil, nil
			c.DefinitionName, c.CreatedAt = nil, nil
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Delete:  remove,
	}
	return d.Resource()
}
