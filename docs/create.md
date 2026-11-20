# ibm-agentid create

Creates an object from data. See [`resources.md`](resources.md) for the resources.

```text
ibm-agentid create <resource> -d <file|->
ibm-agentid create <resource> --template
```

## Flags

| Flag | Meaning |
| :--- | :--- |
| `-d`, `--data` | The data: a path, or `-` for stdin. YAML or JSON, as for [`login`](login.md). |
| `--template` | Print a skeleton of the data and exit. No sign-in is needed. |
| `-o`, `--output` | `yaml` (the default) or `json`. The format of the template and of the printed object. |

## Behavior

* The data has the properties of the object, as `get` shows them. Unknown properties are ignored. The server assigns the ID, so an ID, creation time or other read-only property in the data is ignored.
* The data is checked before any sign-in, so a malformed file fails quickly.
* The created object is printed on stdout, with secrets shown as `********`. `Created <resource> <id>` goes to stderr.
* Create is not idempotent. A name that is already used is an error from the server, shown as received.
* A secret in the data comes only from the file or stdin, never from the command line.

## Examples

```sh
ibm-agentid create foo --template > foo.yaml
ibm-agentid create foo -d foo.yaml
ibm-agentid create foo -d - -o json <<EOF
name: web
enabled: true
EOF
```
