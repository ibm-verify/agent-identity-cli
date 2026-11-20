# ibm-agentid context

Manages the saved contexts. A context holds the server, TLS and OAuth settings of one deployment, plus its own cached token. `login` creates and updates contexts.

```text
ibm-agentid context list
ibm-agentid context use <name>
ibm-agentid context current
ibm-agentid context view [name]
ibm-agentid context rename <old> <new>
ibm-agentid context delete <name>
```

## Which context is used

In order, the first one set wins:

1. The `--context <name>` flag.
2. The `IBM_AGENTID_CONTEXT` environment variable.
3. The current context, set by `login` and `context use`.
4. `default`.

## Subcommands

| Command | What it does |
| :--- | :--- |
| `list` | Prints a table of the saved contexts: name, server and token state. The current context is marked with `*`. |
| `use <name>` | Makes `<name>` the current context. |
| `current` | Prints the name of the context that commands would use, following the order above. |
| `view [name]` | Prints the settings of a context as YAML. The name defaults to the context chosen by the order above. |
| `rename <old> <new>` | Renames a context and moves its cached token. If it was the current context, it stays current under the new name. Fails if `<new>` exists. |
| `delete <name>` | Deletes a context and its cached token, after asking `[y/N]` on stderr. Only `y` or `yes` confirms. With no input, such as in a script, nothing is deleted and the command fails. Use `--yes` (`-y`) to skip the question. If it was the current context, no context is current afterwards. |

Token state in `list` is `valid`, `expired` (a refresh may still work), `stale` (the settings changed after the token was issued, so it is ignored) or `none`.

Context names use letters, digits, `.`, `_` and `-`, start with a letter or digit, and have at most 64 characters.

## Output

* Stdout holds only data: the table, the name or the settings. It is safe to pipe.
* Confirmations and the empty-list hint go to stderr.
* An inline `private_jwk` or `client_secret` is shown as `(inline)` by `view`. A file reference is shown as saved.
* Exit status is `0` on success and non-zero on any failure, including an unknown context.

## Examples

```sh
ibm-agentid context list
ibm-agentid context use prod
ibm-agentid context view prod
ibm-agentid context rename prod production
ibm-agentid context delete staging --yes

# Use another context for one command.
ibm-agentid --context staging context view
```
