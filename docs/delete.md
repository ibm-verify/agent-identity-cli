# ibm-agentid delete

Deletes one object.

```text
ibm-agentid delete <resource> <id>
ibm-agentid delete <resource> --name <name>
```

## Flags

| Flag | Meaning |
| :--- | :--- |
| `--name <name>` | Pick the object by name instead of ID. |
| `-y`, `--yes` | Delete without asking. |

## Behavior

* The command asks `Delete the <resource> "<id or name>"? [y/N]` on stderr. Only `y` or `yes` confirms. With no input, such as in a script, nothing is deleted and the command fails. Use `--yes` in scripts.
* The object is looked up first, so an unknown name fails before the question.
* `Deleted <resource> <id>` goes to stderr. Stdout is empty.
* If the server refuses, for example because the object is still in use, its message is shown as received.

## Examples

```sh
ibm-agentid delete foo --name web
ibm-agentid delete foo foo-1a2b3c --yes
```
