# Resources

The resources that [`get`](get.md), [`create`](create.md), [`replace`](replace.md), [`patch`](patch.md) and [`delete`](delete.md) work with. A verb is offered for a resource only if its API has it, so `ibm-agentid <verb> --help` lists what is available.

Each resource has a plural name, such as `foos`, and a singular alias, such as `foo`. Either works with any verb. Run `ibm-agentid create <resource> --template` for its properties.

| Resource | What it is | Verbs |
| :--- | :--- | :--- |
| `foos` | A stand-in used to try the commands. It is stored in `foo.json` in the state directory, and it goes away when real resources are added. | get, create, replace, patch, delete |

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
