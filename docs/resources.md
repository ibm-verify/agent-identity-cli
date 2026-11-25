# Resources

The resources that [`get`](get.md), [`create`](create.md), [`replace`](replace.md), [`patch`](patch.md) and [`delete`](delete.md) work with. A verb is offered for a resource only if its API has it, so `ibm-agentid <verb> --help` lists what is available.

Each resource has a plural name, such as `agents`, and a singular alias, such as `agent`. Either works with any verb. Run `ibm-agentid create <resource> --template` for its properties, with a comment on each.

| Resource | What it is | Verbs |
| :--- | :--- | :--- |
| `agents` | An agent registered with the Agent Identity server. | get, create, replace, patch, delete |
| `clients` | An OAuth client registered with the Agent Identity server. | get, create, replace, delete |
| `server-connections` | A reusable outbound connection (HTTP or LDAP) registered with the Agent Identity server. | get, create, replace, delete |
| `oauth-providers` | An OAuth 2.0 / OIDC provider definition (issuer, keystores and token settings) registered with the Agent Identity server. | get, create, replace, delete |
| `keystores` | A named keystore of keys and certificates (PKCS#12 or PEM) held by the Agent Identity server. | get, create, replace, delete |
| `auth-detail-types` | An OAuth 2.0 Rich Authorization Requests (RFC 9396) authorization detail type, with the schema of its payload. | get, create, replace, delete |
| `custom-token-types` | A trusted external token issuer, so its tokens are accepted as a `subject_token_type` or `actor_token_type` in token exchange. | get, create, replace, delete |
| `rules` | A server-side JavaScript rule: a mapping rule that shapes tokens and claims, or an access policy that decides a request. | get, create, replace, delete |

## Properties

This page does not list the properties of each resource, because the model is defined by the API. The schemas are in [`api/openapi.yaml`](../api/openapi.yaml), and the sections below name the one to read for each resource. `--template` prints the same properties, with comments, for the CLI you have.

Properties the server assigns, such as IDs and timestamps, are read-only. They are accepted in input, such as the output of `get`, and ignored.

A resource has no partial update unless its section says so, so `patch` is not offered. Use `replace`, which sets the whole definition.

## Agent

Schemas: `Agent` (read), `CreateAgentRequest` (create), `UpdateAgentRequest` (replace and patch).

- `--name` matches the `displayName`. `get agents` accepts `--search`.
- A patch replaces `externalIds`, `tags` and `owners` as a whole.
- Table: `ID`, `NAME`, `STATUS`, `TAGS`, `DESCRIPTION`.

## Client

Schemas: `Client` (read), `ClientWriteRequest` (create and replace).

- The ID is the `client_id`. `--name` matches the `client_name`.
- `client_secret` is write-only: it is accepted on `create` and `replace` and never returned. If you leave it out on `create`, the server generates one that cannot be read, so set your own. If you leave it out on `replace`, the stored one is kept.
- `get clients` accepts `--search`, which matches the `client_name` exactly.
- Table: `ID`, `NAME`, `CLIENT_NAME`, `GRANT_TYPES`, `ENABLED`, `TAGS`.

## Server Connection

Schemas: `ServerConnection` (read), `ServerConnectionWriteRequest` (create and replace).

- The `name` is the identifier. It is unique and immutable.
- `sc` is a short alias: `ibm-agentid get sc` is the same as `ibm-agentid get server-connections`.
- `get server-connections` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply.
- Table: `ID`, `NAME`, `TYPE`, `ENDPOINT`.

## OAuth Provider

Schemas: `OAuthProviderResponse` (read), `OAuthProviderWriteRequest` (create and replace).

- The `definition_name` is the identifier. It is unique and immutable.
- `get oauth-providers` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `definition_name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply.
- Table: `ID`, `NAME`, `ISSUER`, `SIGNING_KEY`, `ALGORITHM`.

## Keystore

Schemas: `KeystoreSummary` (list), `KeystoreWriteRequest` (create and replace).

- The `keystore_name` is the identifier. It is unique and immutable. Other resources refer to it as `ks:<name>`.
- `keystore_data` and `keystore_password` are write-only and never returned. So the output of `get` cannot be sent back to `replace` as it is: add the data again.
- `get keystores` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `keystore_name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply.
- Deleting a keystore that does not exist succeeds.
- Table: `ID`, `NAME`, `TYPE`, `UPDATED_AT`.

## Authorization Detail Type

Schemas: `AuthDetailType` (read), `AuthDetailTypeWriteRequest` (create and replace).

- The `type` URI is unique and immutable, and it is the name for `--name`. The `id` is assigned by the server: the base64url of `type`, without padding. It is the target of `get`, `replace` and `delete`.
- `get auth-detail-types` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `type` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply.
- Table: `ID`, `NAME`, `DESCRIPTION`, `SIGNATURE_STRATEGY`.

## Custom Token Type

Schemas: `CustomTokenType` (read), `CustomTokenTypeWriteRequest` (create and replace).

- The `name` is the value clients send as `subject_token_type` or `actor_token_type`, and it is the name for `--name`. The `id` is assigned by the server: the base64url of `name`, without padding. It is the target of `get`, `replace` and `delete`.
- `name` and `type` are immutable: `replace` must carry the same values as the stored type, or the server refuses it.
- `get custom-token-types` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply.
- Deleting a custom token type that does not exist succeeds.
- Table: `ID`, `NAME`, `TYPE`, `ISSUER`.

## Rule

Schemas: `Rule` (read), `RuleSummary` (list), `RuleCreateRequest` (create), `RuleReplaceRequest` (replace).

- The `name` is the identifier. It is immutable, so `replace` does not send it, and a payload that carries a different `name` than the target is refused.
- A list leaves out the `source`: use `get rule <name>` to read it. The server does not check the syntax when a rule is written.
- `get rules` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply.
- Deleting a rule that does not exist succeeds.
- Table: `ID`, `NAME`, `CLASS`, `UPDATED_AT`.

Adding a resource is described in [`internal/resource/spec.md`](../internal/resource/spec.md) §10.
