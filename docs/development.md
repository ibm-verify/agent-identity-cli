# Development

Working rules and status for contributors and coding agents. User-facing usage lives in the per-command docs (`docs/login.md`, `docs/context.md`) and, later, `README.md` and `AGENTS.md`.

## Status

| Area | State |
| :--- | :--- |
| `internal/profile` | Done. Config, contexts, resolution, token cache. |
| `internal/i18n` | Done. |
| `internal/logging` | Done. File logging, redaction, HTTP round tripper. |
| `internal/oauth` | Done. Login, token, refresh, client credentials, `private_key_jwt`, revoke. |
| `internal/input` | Done. The `-d` reader, `login` payload and `--template`. |
| Command layer | `login` ([`docs/login.md`](login.md)), `context` ([`docs/context.md`](context.md)), `main` and the Makefile are done. |
| `logout` | Next, when asked. Spec: `internal/profile/spec.md` §6. `oauth.Provider.Revoke` and `profile.DeleteToken` / `DeleteAllTokens` already exist. |
| Not started | `--log-*` flags, the `IBM_AGENTID_TOKEN` / `IBM_AGENTID_SERVER` bypass, `README.md`, `AGENTS.md` (how agents and IDEs use the CLI). |

## Layout

* `cmd/ibm-agentid`: `main` only. It sets the language, starts logging, handles Ctrl-C and exits 1 on error.
* `internal/cmd`: the commands. `root.go` builds the root, `data.go` holds the shared `-d` / `--template` / `-o` flags, and each command has its own file and test.
* `internal/input`: reads `-d` data, parses the payload, prints templates.
* `internal/oauth`, `internal/profile`, `internal/i18n`, `internal/logging`: the libraries the commands call.

## Build

* `make build` writes `bin/ibm-agentid`. `make build-all` and `make build-<os>-<arch>` write `bin/ibm-agentid-<os>-<arch>`.
* `make check` runs the gate below. `make clean` removes `bin/`.

## Adding a command

Commands are plain cobra, wired in code: no registry, no executor interface.

1. Write the requirement in the spec, or in the command's doc if the spec already covers it. Add `docs/<command>.md` (usage, flags, output, examples) and a row in the table below.
2. Add `internal/cmd/<command>.go` with one constructor, `newX(opts *options)`, and register it in `NewRoot`. The work goes in ordinary `runX` functions that `RunE` calls.
3. Use `options.context` for `--context`. A command that takes data calls `dataFlags.add`, so `-d`, `--template` and `-o` behave the same everywhere. Add a `XxxTemplate` function in `internal/input` for its payload.
4. Output: stdout holds only data (a template, a token, a table). Confirmations, warnings and prompts go to stderr. Return errors and let `main` print them once.
5. Help text: `Short`, `Long` and flag descriptions go through `i18n`. `Use` and `Example` stay literal. `--help` must stand alone, because users only have the binary.
6. A destructive command asks `[y/N]` on stderr, reads stdin with `cmd.InOrStdin()`, and takes `--yes`. No input means no.
7. Never print an inline secret. Mask it, as `context view` does.
8. Tests go in `internal/cmd/<command>_test.go`. Use `isolate` (it points `XDG_CONFIG_HOME` at a temp directory, so the real state is never touched), `run` and `runWithInput`, and `seed` for saved contexts.
9. Run the gate. `make check` runs it.

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
| `docs/context.md` | The `context` commands. |
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
* CLI: `github.com/spf13/cobra`.
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
