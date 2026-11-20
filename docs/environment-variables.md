# Environment Variables

Every environment variable the CLI reads is listed here. Add new ones in the same change that introduces them.

**Precedence.** For every setting, an explicit command-line flag wins over the environment variable, which wins over the built-in default. An empty variable counts as unset.

## State directory

| Variable | Effect | Default |
| :--- | :--- | :--- |
| `XDG_CONFIG_HOME` | When set, the CLI keeps its state in `$XDG_CONFIG_HOME/.ibm-agentid/` instead of `~/.ibm-agentid/`. | unset (uses the home directory) |
| `HOME` | Used to find `~/.ibm-agentid/` when `XDG_CONFIG_HOME` is not set. | set by the OS |

The state directory holds `config.yaml`, `cache/` and `logs/`.

## Contexts

| Variable | Effect | Default |
| :--- | :--- | :--- |
| `IBM_AGENTID_CONTEXT` | Name of the context to use. Overridden by `--context`; overrides `current-context` in `config.yaml`. | unset |

## Language

| Variable | Effect | Default |
| :--- | :--- | :--- |
| `IBM_AGENTID_LANG` | Language for messages, for example `fr` or `pt-BR`. Overrides the POSIX locale variables below. | unset |
| `LC_ALL`, `LC_MESSAGES`, `LANG` | Standard locale variables, read in that order after `IBM_AGENTID_LANG`. `C` and `POSIX` select English. | unset |

An unknown language falls back to English. See [`internal/i18n/spec.md`](../internal/i18n/spec.md).

## Logging

See [`internal/logging/spec.md`](../internal/logging/spec.md) for what each level records.

| Variable | Effect | Values | Default |
| :--- | :--- | :--- | :--- |
| `IBM_AGENTID_LOG_LEVEL` | Minimum level to record. `off` disables logging. | `off`, `error`, `warn`, `info`, `debug` | `info` |
| `IBM_AGENTID_LOG_OUTPUT` | Where logs go. | `file`, `stderr` | `file` |
| `IBM_AGENTID_LOG_FILE` | Path of the log file. Implies `file` output. | any path | `<state directory>/logs/ibm-agentid.log` |
| `IBM_AGENTID_LOG_FORMAT` | Record format. | `text`, `json` | `text` |
| `IBM_AGENTID_LOG_MAX_SIZE` | Size in MiB at which the log file is rotated. | whole number, 1 or more | `5` |
| `IBM_AGENTID_LOG_MAX_FILES` | Number of rotated files kept. `0` keeps no history. | whole number, 0 or more | `5` |

An invalid value falls back to the default. A warning is printed only when logging was configured explicitly, so a default run stays quiet.

A `debug` log can contain non-credential business data. Review it before sharing it.

## Authentication

See [`internal/oauth/spec.md`](../internal/oauth/spec.md).

| Variable | Effect | Default |
| :--- | :--- | :--- |
| `IBM_AGENTID_CLIENT_SECRET` | Client secret for a `client_secret_post` context, used only when the context has no saved `oauth.client_secret`. A saved value always wins. The variable is read in memory and never written to disk. | unset |

## Stateless mode

For CI, where there is no saved context. See [`internal/resource/spec.md`](../internal/resource/spec.md) §7.1. Only the resource commands (`get`, `create`, `replace`, `patch`, `delete`) use these.

| Variable | Effect | Default |
| :--- | :--- | :--- |
| `IBM_AGENTID_SERVER` | Base URL of the server, an `http` or `https` URL. Used together with the token. | unset |
| `IBM_AGENTID_TOKEN` | A bearer token, used as is. It is not refreshed, so an expired token fails with the server's `401`. | unset |

* When either is set, both must be, or the command fails. A saved context is not used as a fallback.
* `config.yaml` and `cache/` are not read or written.
* `--context` wins: the variables are then ignored and the saved context is used.
* The system trust store is used. A server with a private CA needs a saved context.
* The token is visible to anything that can read the process environment. It is never printed, logged or saved.

## Planned (not yet implemented)

Nothing is planned. Add a variable here, in the same change that documents it, before code uses it. Move it above when it is implemented.
