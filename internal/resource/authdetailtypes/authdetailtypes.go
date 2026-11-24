package authdetailtypes

import (
	"github.com/ibm-verify/agent-identity-cli/internal/client/oauth2"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

const template = `# The authorization detail type (RFC 9396). Unique and immutable. Required.
# This example describes access to a path in a vault.
type: vault:path_access
# Short description of the type.
description: VaultAuthDetails
# JSON schema (as a string) that an authorization detail of this type must
# satisfy. Its "type" property is fixed to the type above.
schema: |
  {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "type": { "const": "vault:path_access" },
      "path": { "type": "string" },
      "capabilities": {
        "type": "array",
        "items": { "type": "string" },
        "minItems": 1
      }
    },
    "required": ["type", "path", "capabilities"]
  }
# How the authorization detail signature is calculated: full_hash,
# partial_hash or custom. The server uses full_hash when it is not given.
signatureStrategy: full_hash
# Rule used to calculate the signature when signatureStrategy is custom.
# signatureRule: ""
# Whether inline HTML (true) or a template label (false) is used for consent.
inlineMode: false
# Whether a custom privacy model is used.
useCustomMapping: false
`

// Resource returns the definition of auth-detail-types, which is all that
// cmd.NewRoot needs. The API has no PATCH, so the patch verb is not offered.
// The ID is the server-generated base64url of the type; the name is the type.
func Resource() *resource.Resource {
	d := &resource.Def[oauth2.AuthDetailType]{
		Name:     "auth-detail-types",
		Singular: "auth-detail-type",
		Aliases:  []string{"auth-detail-type", "auth_detail_types", "auth_detail_type"},
		Template: template,
		Columns: []resource.Column[oauth2.AuthDetailType]{
			{Header: "DESCRIPTION", Value: func(a *oauth2.AuthDetailType) string { return resource.Str(a.Description) }},
			{Header: "SIGNATURE_STRATEGY", Value: func(a *oauth2.AuthDetailType) string {
				if a.SignatureStrategy == nil {
					return ""
				}
				return string(*a.SignatureStrategy)
			}},
		},
		ListFlags: resource.ListSupport{Limit: true, Page: true, Search: true},

		ID:     func(a *oauth2.AuthDetailType) string { return resource.Str(a.Id) },
		NameOf: func(a *oauth2.AuthDetailType) string { return a.Type },

		// The server assigns the ID and timestamps, so input that carries them
		// (the output of get) is accepted and they are dropped.
		Strip: func(a *oauth2.AuthDetailType) {
			a.Id, a.CreatedAt, a.UpdatedAt = nil, nil, nil
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Delete:  remove,
	}
	return d.Resource()
}
