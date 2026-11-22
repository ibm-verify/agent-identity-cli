# Resources

The resources that [`get`](get.md), [`create`](create.md), [`replace`](replace.md), [`patch`](patch.md) and [`delete`](delete.md) work with. A verb is offered for a resource only if its API has it, so `ibm-agentid <verb> --help` lists what is available.

Each resource has a plural name, such as `agents`, and a singular alias, such as `agent`. Either works with any verb. Run `ibm-agentid create <resource> --template` for its properties.

| Resource | What it is | Verbs |
| :--- | :--- | :--- |
| `agents` | An agent registered with the Agent Identity server. | get, create, replace, patch, delete |
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
