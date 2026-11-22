# Resources

The resources that [`get`](get.md), [`create`](create.md), [`replace`](replace.md), [`patch`](patch.md) and [`delete`](delete.md) work with. A verb is offered for a resource only if its API has it, so `ibm-agentid <verb> --help` lists what is available.

Each resource has a plural name, such as `agents`, and a singular alias, such as `agent`. Either works with any verb. Run `ibm-agentid create <resource> --template` for its properties.

| Resource | What it is | Verbs |
| :--- | :--- | :--- |
| `agents` | An agent registered with the Agent Identity server. | get, create, replace, patch, delete |
| `clients` | An OAuth client registered with the Agent Identity server. | get, create, replace, delete |
| `foos` | A stand-in used to try the commands. It is stored in `foo.json` in the state directory, and it goes away when real resources are added. | get, create, replace, patch, delete |

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

`get agents` accepts `--search`, `--sort`, `--limit` and `--page` (from 1). `--name` matches the `displayName`.

## Client

The API has no partial update, so `patch` is not offered for clients. Use `replace`, which sets the whole definition.

| Property | Notes |
| :--- | :--- |
| `client_id` | Assigned by the server. Read-only. |
| `client_name` | Name of the client, shown to the end user. |
| `enabled` | Whether the client is enabled. |
| `grant_types` | Grant types the client may use, such as `authorization_code`, `client_credentials` or `refresh_token`. |
| `response_types` | Response types the client may use, such as `code`, `token` or `id_token`. |
| `redirect_uris` | URIs the server may send the user back to. |
| `token_endpoint_auth_method` | How the client authenticates at the token endpoint, such as `client_secret_basic` or `private_key_jwt`. |
| `scope` | Scopes the client may request, as a space-delimited string. |
| `tags` | Free-form labels. |
| `client_id_issued_at`, `client_secret_expires_at`, `definition_name`, `created_at` | Assigned by the server. Read-only. They are accepted in input, such as the output of `get`, and ignored. |

`get clients` accepts `--search`, `--limit` and `--page` (from 1). `--search` matches the `client_name` exactly. When it is given, every page is searched, so `--limit` and `--page` do not apply. Run `create client --template` for the full set of common properties.

## Foo

| Property | Notes |
| :--- | :--- |
| `id` | Assigned by the server. Read-only. |
| `name` | Unique. Required. |
| `description`, `enabled`, `tags` | A patch replaces `tags` as a whole. |
| `settings.timeout`, `settings.retries` | A patch changes only the properties it gives. |
| `secret` | Shown as `********`. |
| `createdAt` | Assigned by the server. Read-only. |

`get foos` accepts `--search` (the name contains the text), `--limit` and `--page` (from 1).

Adding a resource is described in [`internal/resource/spec.md`](../internal/resource/spec.md) §10.
