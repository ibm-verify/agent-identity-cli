# Resources

The resources that [`get`](get.md), [`create`](create.md), [`replace`](replace.md), [`patch`](patch.md) and [`delete`](delete.md) work with.

The properties of each resource are defined by the API, not by this page. In [`api/openapi.yaml`](../api/openapi.yaml), find the resource's operations by the tag below, and read the request and response schemas they use. For the CLI itself, run `ibm-agentid <verb> --help`, and `ibm-agentid create <resource> --template` for a commented skeleton.

Each resource has a plural name, such as `agents`, and a singular alias, such as `agent`. Either works with any verb. `server-connections` also has the alias `sc`. A verb is offered for a resource only if its API has it.

| Resource | Verbs | Tag in `api/openapi.yaml` |
| :--- | :--- | :--- |
| `agents` | get, create, replace, patch, delete | `Registry` |
| `clients` | get, create, replace, delete | `OAuth2` |
| `server-connections` | get, create, replace, delete | `Platform` |
| `oauth-providers` | get, create, replace, delete | `OAuth2` |
| `keystores` | get, create, replace, delete | `Platform` |
| `auth-detail-types` | get, create, replace, delete | `OAuth2` |
| `custom-token-types` | get, create, replace, delete | `OAuth2` |
| `rules` | get, create, replace, delete | `Platform` |

Adding a resource is described in [`internal/resource/spec.md`](../internal/resource/spec.md) §10.
