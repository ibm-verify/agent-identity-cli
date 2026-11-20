# ibm-agentid login

Signs in to an IBM Agent Identity server, saves the connection as a named context, and caches the token.

```text
ibm-agentid login -d <file | -> [flags]
ibm-agentid login --template [-o yaml|json]
```

## Quick start

```sh
ibm-agentid login --template > prod.yaml   # write a skeleton
# edit prod.yaml
ibm-agentid login -d prod.yaml             # sign in
```

Or from a heredoc, which keeps values out of the shell history:

```sh
ibm-agentid login -d - <<EOF
server: https://agentid.example.com
oauth:
  issuer: https://idp.example.com
  client_id: my-client-id
EOF
```

## Flags

| Flag | Meaning | Default |
| :--- | :--- | :--- |
| `-d`, `--data <path>` | The login data, a file or `-` for stdin. YAML or JSON. | required unless `--template` |
| `--template` | Print a skeleton of the login data and exit. Nothing else runs. | |
| `-o`, `--output <yaml\|json>` | Format of `--template`. | `yaml` |
| `--context <name>` | Name to save the context under. Overrides `context` in the data. | the data, then `default` |
| `--no-browser` | Print the sign-in URL and do not open a browser. | off |
| `--no-save` | Sign in without writing `config.yaml` or the token cache. | off |
| `--print-token` | Print the access token on stdout. | off |
| `--timeout <duration>` | How long to wait for the browser sign-in, for example `90s` or `10m`. | `5m` |

## Login data

| Property | Required | Meaning |
| :--- | :--- | :--- |
| `context` | no | Context name. |
| `server` | yes | Base URL of the server. `http` or `https`, no user name or password. |
| `tls.ca_cert` | no | CA bundle for the server and issuer. `@file` or inline PEM. |
| `tls.insecure_skip_verify` | no | Skip TLS verification. For local development only. |
| `oauth.issuer` | yes | OIDC issuer URL. |
| `oauth.client_id` | yes | OAuth client ID. |
| `oauth.grant_type` | no | `authorization_code` (default) or `client_credentials`. |
| `oauth.auth_type` | no | `none` (default), `private_key_jwt` or `client_secret_post`. |
| `oauth.private_jwk` | no | Private JWK that signs the client assertion for `private_key_jwt`. `@file` or inline JSON. |
| `oauth.client_secret` | no | Client secret. `@file` or inline value. |
| `oauth.redirect_uri` | no | Fixed loopback redirect URI. Defaults to a free port on `127.0.0.1`. |
| `oauth.scopes` | no | List of scopes to request. |

Unknown properties are ignored. `--template` prints every property with an example value.

### File references

`tls.ca_cert`, `oauth.private_jwk` and `oauth.client_secret` accept `@path` to read the value from a file.

* In YAML, quote the value: `client_secret: "@./secret.txt"`.
* A relative path is relative to the data file, or to the working directory when the data comes from stdin.
* The saved context keeps the absolute path. The file is read when needed and never copied into `~/.ibm-agentid`.
* Any value without a leading `@` is used as written.

An inline `private_jwk` or `client_secret` is saved to `config.yaml` as written, and `login` warns about it on stderr. Use `@file` to keep secrets out of `config.yaml`.

## What it does

1. Reads and checks the data. The file references must exist.
2. Signs in with the configured grant:
   * `authorization_code` opens your browser, or prints the URL with `--no-browser`, and waits for the sign-in to finish.
   * `client_credentials` needs no interaction.
3. Caches the token.
4. Saves the context to `config.yaml` and makes it the current context.

If the sign-in fails, `config.yaml` is not changed. Signing in again to an existing context replaces its settings.

With `--no-save`, only step 2 runs. Use it with `--print-token` to get a token without leaving any state behind:

```sh
TOKEN=$(ibm-agentid login -d - --no-save --print-token <<EOF
...
EOF
)
```

## Output

* Prompts, warnings and errors go to stderr.
* Stdout holds only the template or the token, so it is safe to pipe or capture.
* Exit status is `0` on success and non-zero on any failure.

## Examples

```sh
# Sign in to a context named prod.
ibm-agentid login -d prod.yaml --context prod

# On a machine with no browser.
ibm-agentid login -d prod.yaml --no-browser

# Print a JSON skeleton.
ibm-agentid login --template -o json

# Machine-to-machine, token only.
ibm-agentid login -d ci.yaml --no-save --print-token
```

## See also

* [Environment variables](environment-variables.md): `IBM_AGENTID_CONTEXT`, `IBM_AGENTID_CLIENT_SECRET` and the logging settings.
* [`internal/input/spec.md`](../internal/input/spec.md): how the data is read.
* [`internal/profile/spec.md`](../internal/profile/spec.md): contexts and the token cache.
* [`internal/oauth/spec.md`](../internal/oauth/spec.md): the sign-in flows.
