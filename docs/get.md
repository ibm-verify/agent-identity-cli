# ibm-agentid get

Shows one object, or lists objects. See [`resources.md`](resources.md) for the resources.

```text
ibm-agentid get <resource>
ibm-agentid get <resource> <id>
ibm-agentid get <resource> --name <name>
```

## Flags

| Flag | Meaning |
| :--- | :--- |
| `--name <name>` | Pick the object by its name instead of its ID. Names are unique. Not with an ID. |
| `-o`, `--output` | `table` (lists only), `yaml` or `json`. The default is `yaml` for one object and `table` for a list. |
| `--search`, `--sort`, `--limit`, `--page` | List flags. Each is offered only for a resource whose API supports it, so check `--help`. Not with an ID or `--name`. |

## Output

* Stdout holds only the object, the list or the table. A "No ... found" note for an empty list goes to stderr.
* An object is printed as the API returns it, with secrets shown as `********`. A list in `yaml` or `json` is an array of those objects. An empty list is `[]`.
* The output of `get <resource> <id>` can be edited and given to [`replace`](replace.md) or [`patch`](patch.md) with `-d`. The ID, read-only properties and masked secrets in it are ignored.
* A list does not show a total.

## Examples

```sh
ibm-agentid get foos
ibm-agentid get foos --search web --limit 10 --page 2 -o json
ibm-agentid get foo --name web > web.yaml
ibm-agentid --context prod get foo foo-1a2b3c
```

The command uses the saved context, or the [stateless variables](environment-variables.md#stateless-mode). Exit status is non-zero on any failure, including an object that does not exist.
