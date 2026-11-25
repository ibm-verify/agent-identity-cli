# IBM Agent Identity CLI

[![Build Status](https://github.com/ibm-verify/agent-identity-cli/actions/workflows/validate-build.yml/badge.svg)](https://github.com/ibm-verify/agent-identity-cli/actions/workflows/validate-build.yml)
[![Lint](https://github.com/ibm-verify/agent-identity-cli/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/ibm-verify/agent-identity-cli/actions/workflows/golangci-lint.yml)
[![Release](https://img.shields.io/github/v/release/ibm-verify/agent-identity-cli?display_name=tag)](https://github.com/ibm-verify/agent-identity-cli/releases)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.3%2B-00ADD8.svg)](go.mod)

`ibm-agentid` is the command-line client for IBM Agent Identity. It signs you in with OAuth 2.0 and OIDC, then manages the objects of an Agent Identity server: agents, OAuth clients, OAuth providers, keystores, server connections, authorization detail types, custom token types and rules.

---

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [login](#login)
  - [context](#context)
  - [get](#get)
  - [create](#create)
  - [replace](#replace)
  - [patch](#patch)
  - [delete](#delete)
- [Resources](#resources)
- [Workflows](#workflows)
- [Output and Errors](#output-and-errors)
- [Configuration](#configuration)
- [Security](#security)
- [Shell Completion](#shell-completion)
- [Project Structure](#project-structure)
- [Development](#development)
- [Contributing](#contributing)
- [Support](#support)
- [License](#license)

---

## Installation

### Download a release

Each [GitHub release](https://github.com/ibm-verify/agent-identity-cli/releases) has one binary per platform: `ibm-agentid-<os>-<arch>` for `linux`, `darwin` and `windows` (with `.exe`) on `amd64` and `arm64`.

```bash
# Example: Apple silicon. Replace the file name for your platform.
curl -L -o ibm-agentid \
  https://github.com/ibm-verify/agent-identity-cli/releases/latest/download/ibm-agentid-darwin-arm64
chmod +x ibm-agentid && sudo mv ibm-agentid /usr/local/bin/

ibm-agentid --version
```

The version is the release tag, stamped in when the release is built.

### Build from source

For local development only. It needs Go 1.26.3 or newer.

```bash
git clone https://github.com/ibm-verify/agent-identity-cli.git
cd agent-identity-cli
make build            # bin/ibm-agentid
make build-all        # bin/ibm-agentid-<os>-<arch> for all six platforms
```

---

## Quick Start

You need an Agent Identity server and an OAuth client that the CLI can sign in with.

```bash
# 1. Sign in. Print a skeleton, fill in server, oauth.issuer and oauth.client_id, then log in.
ibm-agentid login --template > prod.yaml
ibm-agentid login -d prod.yaml --context prod

# 2. Create an agent from a template.
ibm-agentid create agent --template > agent.yaml
ibm-agentid create agent -d agent.yaml
# Created agent 01a20d9d-a450-78a6-bd05-8a52e442a1d9.

# 3. List it, show it, change it, delete it.
ibm-agentid get agents
ibm-agentid get agent --name "Production API Service" -o json
ibm-agentid patch agent --name "Production API Service" -d - <<EOF
status: ACTIVE
EOF
ibm-agentid delete agent --name "Production API Service" --yes
```

The sign-in opens a browser. The token is cached and refreshed, so later commands need no sign-in.

The command shape is the same everywhere:

```text
ibm-agentid [--context <name>] <verb> <resource> [<id> | --name <name>] [flags]
```

- **Context:** a saved connection to one server, with its own cached token. `login` creates it.
- **Resource:** a kind of object, such as `agents` or `rules`. The singular (`agent`) works too.
- **Data:** YAML or JSON given with `-d <file>`, or `-d -` for stdin.
- **Template:** `--template` prints a commented skeleton of the data. It needs no sign-in.

The output of `get` is accepted by `replace` and `patch`. Read-only properties and masked secrets in it are ignored.

---

## Commands

### login

Signs in, saves the connection as a context, and caches the token. If the sign-in fails, nothing is saved.

```bash
ibm-agentid login -d prod.yaml [--context prod] [--no-browser] [--no-save] [--print-token] [--timeout 5m]
ibm-agentid login --template [-o yaml|json]
```

| Flag | Meaning | Default |
|---|---|---|
| `-d`, `--data` | The login data: a file, or `-` for stdin. | required unless `--template` |
| `--template` | Print a skeleton of the data and exit. | |
| `--context` | Name to save the context under. | the data, then `default` |
| `--no-browser` | Print the sign-in URL and do not open a browser. | off |
| `--no-save` | Do not write `config.yaml` or the token cache. | off |
| `--print-token` | Print the access token on stdout. | off |
| `--timeout` | How long to wait for the browser sign-in. | `5m` |

The login data:

```yaml
context: prod                          # optional
server: https://agentid.example.com    # required
tls:
  ca_cert: "@./ca.pem"                 # optional. "@file" or inline PEM
  insecure_skip_verify: false          # optional. Local development only
oauth:
  issuer: https://idp.example.com      # required. OIDC issuer URL
  client_id: my-client-id              # required
  grant_type: authorization_code       # authorization_code (default) | client_credentials
  auth_type: none                      # none (default) | private_key_jwt | client_secret_post
  private_jwk: "@./jwk_private.json"   # for private_key_jwt. "@file" or inline JSON
  client_secret: "@./secret.txt"       # "@file" or inline value
  redirect_uri: http://127.0.0.1       # optional. Defaults to a free loopback port
  scopes: [openid, offline_access]
```

| Use case | `grant_type` | `auth_type` |
|---|---|---|
| A person at a terminal (default) | `authorization_code` with PKCE | `none` |
| A person, confidential client | `authorization_code` with PKCE | `client_secret_post` |
| A machine, no secret on disk | `client_credentials` | `private_key_jwt` |
| A machine, shared secret | `client_credentials` | `client_secret_post` |

- `tls.ca_cert`, `oauth.private_jwk` and `oauth.client_secret` accept `@path` to read a file. In YAML, quote the value. The saved context keeps the path, not the content.
- An inline `private_jwk` or `client_secret` is written to `config.yaml`, and `login` warns about it. Prefer `@file`.
- The device flow is not supported.

Full reference: [`docs/login.md`](docs/login.md).

### context

```bash
ibm-agentid context list                    # name, server and token state
ibm-agentid context current
ibm-agentid context use <name>
ibm-agentid context view [name]
ibm-agentid context rename <old> <new>
ibm-agentid context delete <name> [--yes]
```

A command uses the first of: `--context`, `IBM_AGENTID_CONTEXT`, the current context, then `default`. The token state is `valid`, `expired`, `stale` (the settings changed after it was issued) or `none`. See [`docs/context.md`](docs/context.md).

### get

```bash
ibm-agentid get <resource>                  # list
ibm-agentid get <resource> <id>             # one object
ibm-agentid get <resource> --name <name>    # one object, by name
```

| Flag | Meaning |
|---|---|
| `--name` | Pick one object by name instead of ID. |
| `--search <name>` | List only the objects with exactly this name. |
| `--limit`, `--page` | Page through a long list, starting at page 1. |
| `-o`, `--output` | `table` (lists only), `yaml` or `json`. A list is a table by default, one object is YAML. |

Secrets are shown as `********`. See [`docs/get.md`](docs/get.md).

### create

```bash
ibm-agentid create <resource> -d <file | -> [-o yaml|json]
ibm-agentid create <resource> --template
```

The server assigns the ID. The data is checked before any sign-in, and read-only properties in it are ignored. `create` is not idempotent: a name that is already in use is an error from the server. See [`docs/create.md`](docs/create.md).

### replace

```bash
ibm-agentid replace <resource> <id> -d <file | ->
ibm-agentid replace <resource> --name <name> -d <file | ->
```

A full update: a property that the data leaves out is cleared, or set to its default, by the server. An `id` in the data must match the target. A secret left as `********` is not sent. See [`docs/replace.md`](docs/replace.md).

### patch

```bash
ibm-agentid patch agent <id> -d <file | ->
```

Changes only the properties in the data. Offered for `agents` only. For `externalIds`, `tags` and `owners`, a patch replaces the whole list. See [`docs/patch.md`](docs/patch.md).

### delete

```bash
ibm-agentid delete <resource> <id> [--yes]
ibm-agentid delete <resource> --name <name> [--yes]
```

Asks `[y/N]` on stderr. With no input, such as in a script, nothing is deleted and the command fails, so use `--yes`. See [`docs/delete.md`](docs/delete.md).

---

## Resources

| Resource | Verbs | What it is |
|---|---|---|
| `agents` | get, create, replace, patch, delete | An agent registered with the server. |
| `clients` | get, create, replace, delete | An OAuth client. |
| `server-connections` (`sc`) | get, create, replace, delete | A reusable outbound HTTP or LDAP connection. |
| `oauth-providers` | get, create, replace, delete | An OAuth 2.0 / OIDC provider definition. |
| `keystores` | get, create, replace, delete | A named keystore of keys and certificates. |
| `auth-detail-types` | get, create, replace, delete | An RFC 9396 authorization detail type. |
| `custom-token-types` | get, create, replace, delete | A trusted external token issuer, for token exchange. |
| `rules` | get, create, replace, delete | A server-side JavaScript mapping rule or access policy. |

Run `ibm-agentid create <resource> --template` to print every property of a resource, with comments. The model of each resource is defined by the API: see its schema in [`api/openapi.yaml`](api/openapi.yaml).

Things worth knowing:

- `--name` matches the `displayName` for agents, and the identifying name for the other resources.
- `keystores` never return `keystore_data` or `keystore_password`. Add them again before you `replace` from the output of `get`.
- `rules` are listed without their `source`. Use `get rule <name>` to read it. The server does not check the syntax when a rule is written.
- `auth-detail-types` and `custom-token-types` get a server-assigned ID that is the base64url of the type and the name.
- Deleting a keystore, rule or custom token type that does not exist succeeds.

The schema to read for each resource, and what is specific to the CLI: [`docs/resources.md`](docs/resources.md).

---

## Workflows

**Edit an object:**

```bash
ibm-agentid get oauth-provider --name default > default.yaml
# edit default.yaml
ibm-agentid replace oauth-provider --name default -d default.yaml
```

**Several servers:**

```bash
ibm-agentid login -d dev.yaml  --context dev
ibm-agentid login -d prod.yaml --context prod
ibm-agentid context use dev
ibm-agentid --context prod get agents      # one command against prod
```

**CI without a saved context.** Give the server and a bearer token in the environment. The resource commands use them and write no state.

```bash
export IBM_AGENTID_SERVER=https://agentid.example.com
export IBM_AGENTID_TOKEN=$(ibm-agentid login -d ci.yaml --no-save --print-token)
ibm-agentid get agents -o json | jq -r '.[].displayName'
```

- Set both variables, or neither. `--context` wins over them.
- The token is used as it is and is not refreshed.
- The system trust store is used. A server with a private CA needs a saved context.

`ci.yaml` for a machine sign-in:

```yaml
server: https://agentid.example.com
oauth:
  issuer: https://idp.example.com
  client_id: ci-client
  grant_type: client_credentials
  auth_type: private_key_jwt
  private_jwk: "@./ci-private.jwk"
```

In GitHub Actions:

```yaml
- name: Register the agent
  env:
    IBM_AGENTID_SERVER: ${{ vars.AGENTID_SERVER }}
    IBM_AGENTID_TOKEN: ${{ secrets.AGENTID_TOKEN }}
  run: ibm-agentid create agent -d agent.yaml
```

---

## Output and Errors

- Stdout holds only data: an object, a list, a template or a token. It is safe to pipe.
- Stderr holds confirmations (`Created ...`), prompts, warnings and errors.
- The exit code is `0` on success and `1` on any failure.

```text
$ ibm-agentid get agent --name nope
no agent "nope" was found

$ ibm-agentid --context nosuch get agents
context "nosuch" is not saved; run 'ibm-agentid login' to create it, or set IBM_AGENTID_SERVER and IBM_AGENTID_TOKEN
```

| Situation | What to do |
|---|---|
| `... is not saved` | Run `login`, or set `IBM_AGENTID_SERVER` and `IBM_AGENTID_TOKEN`. |
| `401` from the server | The token is missing, expired or revoked. Sign in again. |
| A TLS error | Set `tls.ca_cert`. Use `insecure_skip_verify` for local development only. |
| The `id` differs on `replace` | Remove the `id` from the data, or target the right object. |
| A duplicate on `create` | Names are unique. Use `replace`, or choose another name. |
| Sign-in times out | Raise `--timeout`, or use `--no-browser` and open the printed URL. |

For more detail, set `IBM_AGENTID_LOG_LEVEL=debug` and `IBM_AGENTID_LOG_OUTPUT=stderr`.

---

## Configuration

State is kept in `~/.ibm-agentid/`, or `$XDG_CONFIG_HOME/.ibm-agentid/`: `config.yaml`, `cache/` for tokens and `logs/`. Directories are `0700` and files `0600`.

| Variable | Effect | Default |
|---|---|---|
| `IBM_AGENTID_CONTEXT` | Context to use. `--context` wins. | unset |
| `IBM_AGENTID_SERVER`, `IBM_AGENTID_TOKEN` | Server and bearer token, with no saved context. | unset |
| `IBM_AGENTID_CLIENT_SECRET` | Client secret for a `client_secret_post` context that has none saved. | unset |
| `IBM_AGENTID_LANG` | Language of messages, such as `fr`. | your locale |
| `IBM_AGENTID_LOG_LEVEL` | `off`, `error`, `warn`, `info` or `debug`. | `info` |
| `IBM_AGENTID_LOG_OUTPUT` | `file` or `stderr`. | `file` |

A flag wins over a variable, which wins over the default. The log file, format and rotation variables are in [`docs/environment-variables.md`](docs/environment-variables.md).

---

## Security

- Secrets are masked in output. Tokens, codes, PKCE verifiers, client secrets, assertions and JWKs are never logged.
- Secrets come from a file or stdin, never from a command-line argument, so they stay out of shell history and the process list.
- Prefer `@file` over an inline secret: an inline secret is written to `config.yaml`.
- A token in `IBM_AGENTID_TOKEN` is visible to anything that can read the process environment.
- `insecure_skip_verify` turns TLS verification off. Use it for local development only.

To report a vulnerability, see [`SECURITY.md`](SECURITY.md). Do not open a public issue.

---

## Shell Completion

```bash
source <(ibm-agentid completion bash)          # bash
source <(ibm-agentid completion zsh)           # zsh
ibm-agentid completion fish | source           # fish
ibm-agentid completion powershell | Out-String | Invoke-Expression   # PowerShell
```

Run `ibm-agentid completion <shell> --help` to install it permanently.

---

## Project Structure

```
agent-identity-cli/
├── cmd/ibm-agentid/        # main: sets the language, starts logging, handles Ctrl-C
├── internal/
│   ├── cmd/                # login, context, get, create, replace, patch, delete
│   ├── resource/           # Def[T], and one package per resource
│   ├── client/             # generated OpenAPI clients (registry, oauth2, platform)
│   ├── input/              # -d data, @file references, templates
│   ├── oauth/              # sign-in flows
│   ├── profile/            # config, contexts, token cache
│   ├── i18n/               # messages
│   └── logging/
├── api/                    # OpenAPI source of the generated clients
├── docs/                   # per-command and per-resource documentation
├── .github/workflows/      # validate-build, golangci-lint, release
└── Makefile
```

---

## Development

```bash
make build        # bin/ibm-agentid
make check        # gofmt, go vet and go test -race
make build-all    # every platform
make generate     # regenerate the OpenAPI clients
make clean        # remove bin/

golangci-lint run ./...
```

The version is `git describe --tags --always --dirty`. Override it with `make build VERSION=v1.2.3`. The tests need no server, and they never touch your own contexts.

To add a resource, follow [`internal/resource/spec.md`](internal/resource/spec.md) §10. Working rules are in [`docs/development.md`](docs/development.md).

---

## Contributing

You can contribute, or open an issue to discuss a significant change before you submit a pull request.

- Prefix commits with `feat`, `fix`, `docs`, `test`, `ci`, `refactor`, `build`, `chore`, `security` or `revert`, and sign them off with `git commit -s`.
- Run `make check` and `golangci-lint run ./...` before you push. Both must pass.
- Add tests for every change, and document a new command or resource in `docs/`.

See [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). Publishing a [GitHub release](https://github.com/ibm-verify/agent-identity-cli/releases) runs [`release.yml`](.github/workflows/release.yml), which builds every platform and attaches the binaries. The release notes are the changelog.

---

## Support

- **Issues and questions:** [GitHub Issues](https://github.com/ibm-verify/agent-identity-cli/issues)
- **Security issues:** see [`SECURITY.md`](SECURITY.md)

---

## License

[Apache License 2.0](LICENSE)
