# Specification: CLI Environment Context & Authentication State (`ibm-agentid`)

## 1. Overview
`ibm-agentid` interacts with self-hosted **IBM Agent Identity** deployments secured by a customer-provided (BYO) OAuth 2.0 / OIDC authorization server. Because each deployment involves two distinct endpoints—the self-hosted application server (`server`) and the external OAuth authorization server (`issuer`)—the CLI uses named **contexts** (environments) to bind server endpoints, TLS settings, OAuth metadata, and cached tokens together.

### Core Design Principles
1. **Secrets by Reference, Never Copied:** `oauth.private_jwk`, `oauth.client_secret` and `tls.ca_cert` take either a file reference (`@path`) or an inline value. A file reference is saved as an absolute path, and the file's contents are read in memory only when needed and never copied into `~/.ibm-agentid/`. An inline value is saved as written, which is the user's choice, and `login` warns when it saves an inline `private_jwk` or `client_secret`. Using `@file` keeps `config.yaml` free of secrets.
2. **Separation of Config and Token Cache:** Connection metadata lives in `config.yaml`, while short-lived tokens live in `~/.ibm-agentid/cache/` so routine token refreshes never mutate `config.yaml`.
3. **File & Heredoc First (No Auth Flag Soup):** All environment definitions are supplied as structured YAML/JSON via file (`-d <file>`) or `stdin` heredoc (`-d - <<EOF`), keeping secrets out of process lists and shell history while maintaining a single uniform input model across the CLI. See [`internal/input/spec.md`](../input/spec.md).
4. **Multi-Environment Ready:** Users can register multiple environments (e.g., `local`, `dev`, `prod`) and switch between them instantaneously or target a specific context per command via `--context`.

---

## 2. Directory & File Layout

All persistent CLI state resides under `~/.ibm-agentid/` (respecting `$XDG_CONFIG_HOME` if configured):

```text
~/.ibm-agentid/
├── config.yaml                 # Environment definitions & active context pointer (0600)
└── cache/
    ├── default.json            # Cached access/refresh tokens for "default" context (0600)
    └── prod.json               # Cached access/refresh tokens for "prod" context (0600)
```

---

## 3. Data Models & Schemas

### 3.1 Input Environment Payload (`login -d <file.yaml>` or `login -d -`)
A portable YAML or JSON payload passed via file or `stdin` heredoc. Teams may check environment files that use `@file` references into Git for fast developer onboarding.

```yaml
context: prod                                      # Optional; overridden by --context flag (defaults to "default")
server: https://corp.internal                      # Required: Base URL of the self-hosted application

tls:
  ca_cert: "@/etc/ssl/certs/corp-root-ca.pem"      # Optional: Custom CA bundle, "@file" or inline PEM
  insecure_skip_verify: false                      # Optional: Disable TLS verification (for local dev only)

oauth:
  issuer: https://corp.internal # Required: OIDC issuer URL for .well-known discovery
  client_id: c020b99d-c9f6-4ccd-96ce-2226c91009ff  # Required: OAuth client identifier
  grant_type: authorization_code                   # Optional: authorization_code (default) | device_code | client_credentials
  auth_type: none                                  # Optional: none (PKCE, default) | private_key_jwt | client_secret_post
  private_jwk: "@./jwk_pr.json"                    # Optional: private JWK that signs the client assertion for private_key_jwt. "@file" (relative to the data file) or inline JSON
  client_secret: "@./secret.txt"                   # Optional: "@file" or inline value
  redirect_uri: http://127.0.0.1                   # Optional: Fixed loopback URI (defaults to ephemeral port 127.0.0.1:0)
  scopes: ["openid", "offline_access", "agent:admin"]
```

### 3.2 Persistent Configuration (`~/.ibm-agentid/config.yaml`)
Stores all registered contexts and the active context pointer. File references (`private_jwk`, `client_secret`, `ca_cert`) are normalized to absolute paths and keep their `@`. Inline values are saved as written.

```yaml
current-context: prod
contexts:
  default:
    server: https://localhost:8443
    tls:
      insecure_skip_verify: true
    oauth:
      issuer: https://keycloak.local
      client_id: dev-cli-client
      grant_type: authorization_code
      auth_type: none
      scopes: ["openid", "offline_access", "agent:admin"]

  prod:
    server: https://corp.internal
    tls:
      ca_cert: "@/etc/ssl/certs/corp-root-ca.pem"
    oauth:
      issuer: https://corp.internal
      client_id: c020b99d-c9f6-4ccd-96ce-2226c91009ff
      grant_type: client_credentials
      auth_type: private_key_jwt
      private_jwk: "@/Users/vivek/.ssh/agentid-prod-jwk.json"
      scopes: ["agent:admin"]
```

### 3.3 Cached Token State (`~/.ibm-agentid/cache/<context>.json`)
Created with `0600` file permissions whenever a token is acquired or refreshed.

```json
{
  "config_hash": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "access_token": "<access token>",
  "refresh_token": "<refresh token>",
  "token_type": "Bearer",
  "expires_at": "2026-11-20T12:30:00Z"
}
```
* **`config_hash`:** SHA-256 digest of `(server + issuer + client_id + grant_type + scopes)`. If the context's metadata changes, any cached token with a mismatched hash is automatically invalidated.

---

## 4. Authentication & Token Lifecycle Requirements

### 4.1 Supported Grant Types
1. **Authorization Code + PKCE (`authorization_code` — Default):**
   * Spins up a temporary loopback HTTP server on `127.0.0.1` (ephemeral port by default, or fixed port via `redirect_uri`).
   * Opens the system browser to the IdP's `authorization_endpoint` with S256 PKCE challenge and random `state`.
   * Exchanges the authorization code + `code_verifier` at `token_endpoint` and caches `access_token` + `refresh_token`.
2. **Device Authorization Grant (`device_code`):**
   * Fallback for SSH, containers, and headless terminals (`grant_type: device_code`).
   * Displays `verification_uri` and `user_code`, polls `token_endpoint`, and caches `access_token` + `refresh_token`.
3. **Client Credentials (`client_credentials`):**
   * Intended for service accounts and CI/CD automation using either `client_secret_post` or `private_key_jwt` (RFC 7523).

### 4.2 Expiration & Auto-Refresh Behavior
Tokens within **30 seconds** of `expires_at` are treated as expired.

| Grant / Auth Method | What `config.yaml` holds | Behavior Upon `access_token` Expiration |
| :--- | :--- | :--- |
| **`authorization_code` / `device_code` (Public Client)** | No secret | Silently refreshes using `refresh_token` + `client_id` (no secret needed). If `refresh_token` is missing/expired, prompts user to re-run `login`. |
| **`client_credentials` (`client_secret_post`)** | `client_secret` as `@file` or inline | Silently mints a new `access_token` with the saved secret. If the context has none, uses `IBM_AGENTID_CLIENT_SECRET`, otherwise prompts to re-run `login`. A saved secret always wins over the variable. |
| **`client_credentials` (`private_key_jwt`)** | `private_jwk` as `@file` or inline JWK | Silently signs a fresh JWT assertion and mints a new `access_token`. |

---

## 5. Context Resolution Precedence

All environment variables are listed in [`docs/environment-variables.md`](../../docs/environment-variables.md).

Every API command resolves its target context in descending order of precedence:

1. **`--context <name>` flag** on the command (e.g., `ibm-agentid get agents --context prod`)
2. **`IBM_AGENTID_CONTEXT`** environment variable
3. **`current-context`** in `~/.ibm-agentid/config.yaml` (defaults to `"default"`)

*(Optional CI Escape Hatch: If `IBM_AGENTID_TOKEN` and `IBM_AGENTID_SERVER` are both set in the environment, the CLI uses them directly without reading `config.yaml` or `cache/`.)*

---

## 6. CLI Command Interface

### Login & Logout
* `ibm-agentid login -d <env-file.yaml> [--context <name>]` — Imports environment settings from a file, executes the OAuth flow, saves the context to `config.yaml`, caches the token, and sets `current-context`.
* `ibm-agentid login -d - [--context <name>]` — Reads the environment definition from `stdin` (e.g., via heredoc `<<EOF`).
* `ibm-agentid login --template [-o yaml|json]` — Outputs a boilerplate environment payload.
* `ibm-agentid login -d - --no-save --print-token` — Mints and prints a bearer token to `stdout` from a `stdin` payload without writing to `config.yaml` or `cache/`.
* `ibm-agentid logout [--context <name> | --all]` — Purges cached tokens for the target context(s).

`-d` and `--template` are the same on every command that takes a data payload.

### Context Management
* `ibm-agentid context list` — Lists all saved contexts, marking `current-context` with `*` and showing token validity.
* `ibm-agentid context use <name>` — Updates `current-context` in `config.yaml`.
* `ibm-agentid context current` — Prints the active context name.
* `ibm-agentid context view [<name>]` — Displays the resolved configuration for a context.
* `ibm-agentid context rename <old> <new>` — Renames a context in `config.yaml` and migrates its cache file.
* `ibm-agentid context delete <name>` — Removes a context from `config.yaml` and deletes its cache file.

---

## 7. Security & File Handling Requirements
* **File Permissions:** `~/.ibm-agentid/config.yaml` and all files in `~/.ibm-agentid/cache/` must be created with `0600` permissions (and `~/.ibm-agentid/` with `0700`).
* **Atomic Writes:** Updates to `config.yaml` and token cache files must write to a temporary file in the same directory and use an atomic `rename` to prevent file corruption across concurrent CLI invocations.
* **File References:** Only `oauth.private_jwk`, `oauth.client_secret` and `tls.ca_cert` accept a file reference. A value starting with `@` is a file, and anything else is the inline value. A bare path without `@` is just an inline value. A relative `@` path resolves against the directory of the data file (the working directory for `stdin`) and is saved as an absolute path. `login` checks that the file exists, and reads its contents only when needed. Contents are never copied into `~/.ibm-agentid/`.
* **Inline Secrets:** An inline `private_jwk` or `client_secret` is saved as written, and `login` prints a warning on stderr. `context view` and logs never show an inline `private_jwk` or `client_secret`. A file reference is shown as written.
