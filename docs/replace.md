# ibm-agentid replace

Replaces an object with the data. This is a full update. To change only some properties, use [`patch`](patch.md).

```text
ibm-agentid replace <resource> <id> -d <file|->
ibm-agentid replace <resource> --name <name> -d <file|->
ibm-agentid replace <resource> --template
```

## Flags

| Flag | Meaning |
| :--- | :--- |
| `--name <name>` | Pick the object by name instead of ID. Name an object one way, not both. |
| `-d`, `--data` | The data: a path, or `-` for stdin. YAML or JSON. |
| `--template` | Print a skeleton of the data and exit. |
| `-o`, `--output` | `yaml` (the default) or `json`. |

## Behavior

* A property that the data leaves out is cleared or set to its default by the server, as the API defines. Write the whole object.
* A typical edit is `get`, change the file, then `replace`. The ID, read-only properties and masked secrets in the file are ignored, so the file can be used as it is. A secret you leave as `********` is not sent. To change it, write the new value.
* If the data has an `id`, it must match the object. Otherwise the command fails and sends nothing. The name can differ, which renames the object.
* There is no confirmation, because the object still exists afterwards.
* The updated object is printed on stdout, and `Replaced <resource> <id>` goes to stderr.

## Examples

```sh
ibm-agentid get foo --name web > web.yaml
# edit web.yaml
ibm-agentid replace foo --name web -d web.yaml
```
