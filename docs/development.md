# Development

Working rules and status for contributors and coding agents. User-facing usage belongs in `README.md` and, later, `AGENTS.md`.

## Status

| Area | State |
| :--- | :--- |
| `internal/profile` | Done. Config, contexts, resolution, token cache. |
| `internal/i18n` | Done. |
| `internal/logging` | Done. File logging, redaction, HTTP round tripper. |
| `internal/oauth` | Done. Login, token, refresh, client credentials, `private_key_jwt`, revoke. |
| `internal/input` | Done. The `-d` reader, `login` payload and `--template`. |
| Command layer | `login`, `main` and the Makefile are done (see [`docs/login.md`](login.md)). `logout` and `context ...` are next, when asked. |
| `AGENTS.md` | With the command layer. It describes how agents and IDEs use the CLI. |

Scope today is `login` (`internal/cmd`, `cmd/ibm-agentid`) on top of the libraries under `internal/`. Commands are plain cobra, wired in code: one constructor per command, shared flags in `internal/cmd/data.go`, no registry or executor interface. Help text goes through `i18n`, and `--help` must stand alone because users only have the binary.

## Build

* `make build` writes `bin/ibm-agentid`. `make build-all` and `make build-<os>-<arch>` write `bin/ibm-agentid-<os>-<arch>`.
* `make check` runs the gate below. `make clean` removes `bin/`.

## Where the requirements live

| File | Covers |
| :--- | :--- |
| `internal/profile/spec.md` | Main requirements: payload, config, token state, grants, context resolution, commands, security. |
| `internal/profile/cache-spec.md` | Token cache. |
| `internal/oauth/spec.md` | OAuth provider. |
| `internal/input/spec.md` | The `-d` data input, `@` file references, the `login` payload, `--template`. |
| `internal/i18n/spec.md` | Localization. |
| `internal/logging/spec.md` | Logging. |
| `docs/login.md` | The `login` command: flags, data, behavior. |
| `docs/environment-variables.md` | Every environment variable. |

## Process

* Work one piece at a time. Write a `spec.md` for a new subsystem and get it reviewed before implementing.
* Specs name types and errors in prose. They hold decisions, rationale, security rules, behavior tables and out-of-scope items, and no Go snippets. The exact API lives in the code and its doc comments.
* Keep replies and docs short. No "(`ibm-agentid`)" suffix in titles.
* Before calling work done, run:

  ```sh
  gofmt -l .
  go vet ./...
  go test -race -count=1 ./...
  ```

## Dependencies

* Module: `github.com/ibm-verify/agent-identity-cli`.
* YAML: `go.yaml.in/yaml/v3`, not `gopkg.in/yaml.v3`.
* i18n: `github.com/nicksnyder/go-i18n/v2`.
* OAuth: `github.com/ibm-verify/verify-auth-go` at v0.1.2 or newer. No `replace`.

## State directory

* `~/.ibm-agentid`, or `$XDG_CONFIG_HOME/.ibm-agentid`. The leading dot is kept in both.
* Directories are `0o700` and files `0o600`, written atomically.

## i18n

* Message IDs are stable and dotted, such as `auth.login_timeout`. Pass them as string literals at call sites. A source-scan test fails on non-literal IDs.
* Placeholders are named, as in `{{.Name}}`.
* Every ID used must exist in `internal/i18n/locales/en.json` with a `description`, and no ID may go unused.
* Localized errors wrap a plain sentinel with `i18n.Wrap(sentinel, id, data)`.
* Logs are English and not localized. OAuth `error` and `error_description` from the server are shown as received.

## Logging

* The default is `info` level, as text, to a rotated file.
* Redaction applies only through the handler that `logging.Init` installs.
* Never log tokens, codes, PKCE verifiers, client secrets, assertions, JWK contents or the authorization URL query.
* `state` is never logged. It does appear in the sign-in URL printed to the user.

## Environment variables

Every variable is documented in `docs/environment-variables.md`. The `docs` test requires code-used variables to be documented, "implemented" ones to be used in code, and "Planned" ones not to be used yet.

## OAuth decisions

* Authorization code with PKCE is the interactive flow. Device flow is not planned, and `device_code` returns `ErrUnsupportedGrant`.
* A public client (`auth_type: none`) is the default and recommended for the CLI. A secret is optional for authorization code and required for `client_credentials`.
* The discovery issuer check is strict. A callback `iss` must match if present, and a missing `iss` is accepted.
* The callback server accepts only the redirect URI's host and port, and the default callback path is `/callback`.
* `logout` revokes the token on a best-effort basis. A revocation failure never blocks it.
* `oauth.private_jwk`, `oauth.client_secret` and `tls.ca_cert` are `Source` / `SecretSource` values: `@path` is a file, anything else is inline. File references are saved as absolute paths and read in memory only when needed, never copied into `~/.ibm-agentid`. An inline value is saved as written and `login` warns about it.
* The context's `client_secret` wins over `IBM_AGENTID_CLIENT_SECRET`, which applies only when the context has none.
* A token with no `expires_in` has no expiry and is treated as valid.
* `client_credentials` re-mints and `authorization_code` refreshes automatically, using the saved credentials.

## Tooling notes

* `go mod tidy` after adding an import. `verify-auth-go` shows as `// indirect` until code imports it.
* Stay inside the workspace. Check dependency APIs with `go doc` and `go build`, not by reading the module cache.
