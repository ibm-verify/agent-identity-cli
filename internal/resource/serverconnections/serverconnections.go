package serverconnections

import (
	"fmt"

	"github.com/ibm-verify/agent-identity-cli/internal/client/platform"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

const template = `# Name of the server connection. It is the identifier, unique and immutable.
name: my-connection
# Type of the connection: http or ldap. Supply the matching block below, and
# only that one.
type: http
# Configuration for type http.
http:
  # Base URL of the remote service. Required.
  base_url: https://api.example.com
  # Maximum time to wait for one outbound request, in seconds.
  timeout_seconds: 30
  # How the connection authenticates: none, basic, api_key, oauth or mtls.
  authentication:
    type: oauth
    # OAuth token endpoint, client ID and client secret.
    token_url: https://auth.example.com/oauth2/token
    client_id: my-client-id
    client_secret: my-client-secret
    # For type basic use username and password instead.
    # For type api_key use api_key_header and api_key_ref instead.
  # Named operations, keyed by operation name. Path and query values may use
  # {variable} placeholders.
  operations:
    get-user:
      method: GET
      path: /v2/users/{user_id}
      headers:
        Accept: application/json
  # How failed requests are retried: backoff is none, fixed or exponential.
  retry:
    max_attempts: 3
    backoff: exponential

# Configuration for type ldap. Use it instead of the http block, and set type
# to ldap.
# ldap:
#   # Host and port of the directory server. Required.
#   host: ldap.example.com
#   port: 636
#   use_ssl: true
#   # References to the service-account bind DN and password secrets.
#   bind_dn_ref: ldap-bind-dn
#   bind_password_ref: ldap-bind-password
#   # Named search templates, keyed by search name. The filter may use
#   # {variable} placeholders. Scope is base, one or subtree.
#   searches:
#     find-user:
#       base_dn: ou=people,dc=example,dc=com
#       filter: (uid={user_id})
#       scope: subtree
#       attributes: [uid, cn, mail]
`

// endpoint returns the base URL of an http connection or host:port of an
// ldap connection.
func endpoint(s *platform.ServerConnection) string {
	switch {
	case s.Http != nil:
		return s.Http.BaseUrl
	case s.Ldap != nil:
		return fmt.Sprintf("%s:%d", s.Ldap.Host, s.Ldap.Port)
	}
	return ""
}

// authentication returns the authentication block of an http connection, or
// nil when there is none.
func authentication(s *platform.ServerConnection) *platform.ServerConnectionAuthentication {
	if s.Http == nil {
		return nil
	}
	return s.Http.Authentication
}

// Resource returns the definition of server connections, which is all that
// cmd.NewRoot needs. The API has no PATCH, so the patch verb is not offered.
func Resource() *resource.Resource {
	d := &resource.Def[platform.ServerConnection]{
		Name:     "server-connections",
		Singular: "server-connection",
		Aliases:  []string{"server-connection", "server_connections", "server_connection", "sc"},
		Template: template,
		Columns: []resource.Column[platform.ServerConnection]{
			{Header: "TYPE", Value: func(s *platform.ServerConnection) string { return resource.Str((*string)(&s.Type)) }},
			{Header: "ENDPOINT", Value: endpoint},
		},
		ListFlags: resource.ListSupport{Limit: true, Page: true, Search: true},

		ID:     func(s *platform.ServerConnection) string { return s.Name },
		NameOf: func(s *platform.ServerConnection) string { return s.Name },

		// The server assigns the timestamps, so input that carries them (the
		// output of get) is accepted and they are dropped. A secret still
		// holding the mask came from masked output, so it is cleared rather
		// than sent back as the secret.
		Strip: func(s *platform.ServerConnection) {
			s.CreatedAt, s.UpdatedAt = nil, nil
			if a := authentication(s); a != nil {
				if a.ClientSecret != nil && *a.ClientSecret == resource.Mask {
					a.ClientSecret = nil
				}
				if a.Password != nil && *a.Password == resource.Mask {
					a.Password = nil
				}
			}
		},
		Mask: func(s *platform.ServerConnection) {
			if a := authentication(s); a != nil {
				mask := resource.Mask
				if a.ClientSecret != nil {
					a.ClientSecret = &mask
				}
				if a.Password != nil {
					a.Password = &mask
				}
			}
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Delete:  remove,
	}
	return d.Resource()
}
