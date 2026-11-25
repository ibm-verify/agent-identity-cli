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
- [Resources](#resources)
- [Configuration](#configuration)
- [Security](#security)
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

### Build from source

For local development only. It needs Go 1.26.3 or newer.

```bash
git clone https://github.com/ibm-verify/agent-identity-cli.git
cd agent-identity-cli
make build
```

---

## Quick Start

You need an Agent Identity server and an OAuth client that the CLI can sign in with.

```bash
ibm-agentid login --template > login.yaml     # fill it in
ibm-agentid login -d login.yaml
ibm-agentid get agents
```

The sign-in opens a browser. The token is cached and refreshed, so later commands need no sign-in.

---

## Commands

```text
ibm-agentid [--context <name>] <verb> <resource> [<id> | --name <name>] [flags]
```

| Command | Purpose |
|---|---|
| `login` | Sign in and save the connection as a context. See [`docs/login.md`](docs/login.md). |
| `context` | List, switch, view, rename and delete contexts. See [`docs/context.md`](docs/context.md). |
| `get` | Show one object or list objects. See [`docs/get.md`](docs/get.md). |
| `create` | Create an object from data. See [`docs/create.md`](docs/create.md). |
| `replace` | Replace an object with data. See [`docs/replace.md`](docs/replace.md). |
| `patch` | Change some properties of an object. See [`docs/patch.md`](docs/patch.md). |
| `delete` | Delete an object. See [`docs/delete.md`](docs/delete.md). |

Run `ibm-agentid <command> --help` for the flags and examples of a command.

---

## Resources

| Resource | Verbs |
|---|---|
| `agents` | get, create, replace, patch, delete |
| `clients` | get, create, replace, delete |
| `server-connections` (`sc`) | get, create, replace, delete |
| `oauth-providers` | get, create, replace, delete |
| `keystores` | get, create, replace, delete |
| `auth-detail-types` | get, create, replace, delete |
| `custom-token-types` | get, create, replace, delete |
| `rules` | get, create, replace, delete |

The properties of each resource are defined by the API: find the resource's operations by their tag (`Registry`, `OAuth2` or `Platform`) in [`api/openapi.yaml`](api/openapi.yaml). Run `ibm-agentid create <resource> --template` for a commented skeleton. More in [`docs/resources.md`](docs/resources.md).

---

## Configuration

State is kept in `~/.ibm-agentid/`, or `$XDG_CONFIG_HOME/.ibm-agentid/`: `config.yaml`, `cache/` for tokens and `logs/`. Every environment variable is listed in [`docs/environment-variables.md`](docs/environment-variables.md).

---

## Security

- Secrets are masked in output. Tokens, codes, PKCE verifiers, client secrets, assertions and JWKs are never logged.
- Secrets come from a file or stdin, never from a command-line argument, so they stay out of shell history and the process list.
- `insecure_skip_verify` turns TLS verification off. Use it for local development only.

To report a vulnerability, see [`SECURITY.md`](SECURITY.md). Do not open a public issue.

---

## Development

```bash
make build        # bin/ibm-agentid
make check        # gofmt, go vet and go test -race
make build-all    # every platform
make generate     # regenerate the OpenAPI clients
```

Working rules are in [`docs/development.md`](docs/development.md). To add a resource, follow [`internal/resource/spec.md`](internal/resource/spec.md) §10.

---

## Contributing

You can contribute, or open an issue to discuss a significant change before you submit a pull request.

- Prefix commits with `feat`, `fix`, `docs`, `test`, `ci`, `refactor`, `build`, `chore`, `security` or `revert`, and sign them off with `git commit -s`.
- Run `make check` and `golangci-lint run ./...` before you push. Both must pass.

See [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). Publishing a [GitHub release](https://github.com/ibm-verify/agent-identity-cli/releases) runs [`release.yml`](.github/workflows/release.yml), which builds every platform and attaches the binaries.

---

## Support

- **Issues and questions:** [GitHub Issues](https://github.com/ibm-verify/agent-identity-cli/issues)
- **Security issues:** see [`SECURITY.md`](SECURITY.md)

---

## License

[Apache License 2.0](LICENSE)
