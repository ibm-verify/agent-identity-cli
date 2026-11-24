# Resources

The resources that [`get`](get.md), [`create`](create.md), [`replace`](replace.md), [`patch`](patch.md) and [`delete`](delete.md) work with. A verb is offered for a resource only if its API has it, so `ibm-agentid <verb> --help` lists what is available.

Each resource has a plural name, such as `agents`, and a singular alias, such as `agent`. Either works with any verb. Run `ibm-agentid create <resource> --template` for its properties.

| Resource | What it is | Verbs |
| :--- | :--- | :--- |
| `agents` | An agent registered with the Agent Identity server. | get, create, replace, patch, delete |
| `clients` | An OAuth client registered with the Agent Identity server. | get, create, replace, delete |
| `server-connections` | A reusable outbound connection (HTTP or LDAP) registered with the Agent Identity server. | get, create, replace, delete |
| `oauth-providers` | An OAuth 2.0 / OIDC provider definition (issuer, keystores and token settings) registered with the Agent Identity server. | get, create, replace, delete |
| `keystores` | A named keystore of keys and certificates (PKCS#12 or PEM) held by the Agent Identity server. | get, create, replace, delete |
| `auth-detail-types` | An OAuth 2.0 Rich Authorization Requests (RFC 9396) authorization detail type, with the schema of its payload. | get, create, replace, delete |

## Agent

| Property | Notes |
| :--- | :--- |
| `id` | Assigned by the server. Read-only. |
| `displayName` | Unique. Required. Max 255 characters. |
| `description` | Max 1024 characters. |
| `status` | `ACTIVE`, `SUSPENDED` or `UNDER_REVIEW`. The server uses `UNDER_REVIEW` when it is not given. |
| `externalIds` | Identifiers of the agent in other systems. Each one is unique. Max 10. A patch replaces the list as a whole. |
| `tags` | Max 10. A patch replaces the list as a whole. |
| `owners` | Users who own the agent, each with `type: User` and a `value` that is the user ID. Max 5. A patch replaces the list as a whole. |
| `meta`, `createdBy`, `oauthClients` | Assigned by the server. Read-only. They are accepted in input, such as the output of `get`, and ignored. |

`get agents` accepts `--search`. `--name` matches the `displayName`.

## Client

The API has no partial update, so `patch` is not offered for clients. Use `replace`, which sets the whole definition.

| Property | Notes |
| :--- | :--- |
| `client_id` | Assigned by the server. Read-only. |
| `client_name` | Name of the client, shown to the end user. |
| `enabled` | Whether the client is enabled. |
| `grant_types` | Grant types the client may use, such as `urn:ietf:params:oauth:grant-type:token-exchange`,  `urn:openid:params:grant-type:ciba`,  `client_credentials` `refresh_token` etc |
| `response_types` | Response types the client may use, such as `code`, `token` or `id_token`. |
| `redirect_uris` | URIs the server may send the user back to. |
| `token_endpoint_auth_method` | How the client authenticates at the token endpoint, such as `client_secret_basic` or `private_key_jwt`. |
| `scope` | Scopes the client may request, as a space-delimited string. |
| `tags` | Free-form labels. |
| `client_id_issued_at`, `client_secret_expires_at`, `definition_name`, `created_at` | Assigned by the server. Read-only. They are accepted in input, such as the output of `get`, and ignored. |

`get clients` accepts `--search`. `--search` matches the `client_name` exactly. Run `create client --template` for the full set of common properties.

## Server Connection

The API has no partial update, so `patch` is not offered for server connections. Use `replace`, which sets the whole definition.

| Property | Notes |
| :--- | :--- |
| `name` | Unique name of the server connection. Identifier, immutable. Required. |
| `type` | Type of connection: `http` or `ldap`. Required. |
| `http` | Configuration for type `http` (`base_url`, `timeout_seconds`, `authentication`, `operations`, `retry`). |
| `ldap` | Configuration for type `ldap` (`host`, `port`, `use_ssl`, `bind_dn_ref`, `bind_password_ref`, `searches`). |
| `created_at`, `updated_at` | Assigned by the server. Read-only. They are accepted in input, such as the output of `get`, and ignored. |

`get server-connections` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply. Run `create server-connection --template` for the full template with comments.

## OAuth Provider

The API has no partial update, so `patch` is not offered for OAuth providers. Use `replace`, which sets the whole definition.

| Property | Notes |
| :--- | :--- |
| `definition_name` | Unique name of the provider. Identifier, immutable. Required. |
| `issuer` | Trusted issuer URL of the provider. Required. |
| `jwks_sig_keystore` | Keystore identifier holding the JWKS signing keys/certificates. |
| `jwks_enc_keystore` | Keystore identifier holding the JWKS encryption keys/certificates. |
| `token_settings` | Token settings: `signing_algorithm`, `signing_key_store`, `signing_key_label`, `access_token_lifetime_seconds`, `refresh_token_lifetime_seconds`, `grant_types`, mapping rule and access policy IDs. |
| `dcr_settings` | Dynamic client registration settings. |
| `version`, `created_at`, `updated_at` | Assigned by the server. Read-only. They are accepted in input, such as the output of `get`, and ignored. |

`get oauth-providers` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `definition_name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply. Run `create oauth-provider --template` for the full template with comments.


## Keystore

The API has no partial update, so `patch` is not offered for keystores. Use `replace`, which sets the whole definition.

| Property | Notes |
| :--- | :--- |
| `keystore_name` | Unique name of the keystore. Identifier, immutable. Required. Other resources refer to it as `ks:<name>`. |
| `keystore_type` | `p12` or `pem`. Required. |
| `keystore_data` | Content of the keystore. Write-only, never returned. For `p12`, the base64-encoded PKCS#12 file. For `pem`, a JSON document with `certificates` and `keys`, each an array of `label` and PEM `content`. |
| `keystore_password` | Password of a `p12` keystore. Write-only, never returned. |
| `created_at`, `updated_at` | Assigned by the server. Read-only. They are accepted in input, such as the output of `get`, and ignored. |

Because `keystore_data` and `keystore_password` are never returned, the output of `get` cannot be sent back to `replace` as it is: add the data again. `get keystores` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `keystore_name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply. Deleting a keystore that does not exist succeeds. Run `create keystore --template` for the full template with comments.

## Authorization Detail Type

The API has no partial update, so `patch` is not offered for authorization detail types. Use `replace`, which sets the whole definition.

| Property | Notes |
| :--- | :--- |
| `id` | Assigned by the server: the base64url of `type`, without padding. Read-only. Used as the target of `get`, `replace` and `delete`. |
| `type` | The authorization detail type URI. Unique and immutable. Required. This is the name for `--name`. |
| `description` | Short description. |
| `schema` | The JSON schema describing the payload structure, as a string. |
| `signatureStrategy` | How the authorization detail signature is calculated: `full_hash` (the default), `partial_hash` or `custom`. |
| `signatureRule` | Rule used to calculate the signature when `signatureStrategy` is `custom`. |
| `inlineMode` | Whether inline HTML (`true`) or a template label (`false`) is used for consent. |
| `useCustomMapping` | Whether a custom privacy model is used. |
| `created_at`, `updated_at` | Assigned by the server. Read-only. They are accepted in input, such as the output of `get`, and ignored. |

`get auth-detail-types` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `type` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply. Run `create auth-detail-type --template` for the full template with comments.

Adding a resource is described in [`internal/resource/spec.md`](../internal/resource/spec.md) §10.
