# Specification: OAuth Token Provider

## 1. Overview
The token provider gets an access token for a context. It reuses a valid cached token, refreshes an expired one, mints a new one for non-interactive grants, and runs the interactive **authorization code + PKCE** login. The CLI is a native app, so the browser redirects back to a short-lived HTTP server that the CLI starts on the loopback interface ([RFC 8252 §7.3](https://datatracker.ietf.org/doc/html/rfc8252#section-7.3)).

The OAuth protocol work (discovery, authorization URL, PKCE, token requests, client authentication) is done by [`verify-auth-go/oauth2`](https://github.com/ibm-verify/verify-auth-go). This package adds what the SDK leaves to the application: the loopback listener, opening the browser, TLS and logging on the HTTP client, loading the signing key, and the cache policy. Context settings and the token file format come from [`internal/profile`](../profile/spec.md) and its [`cache-spec.md`](../profile/cache-spec.md).

The package is `internal/oauth`. It is library code only. The `login` and `logout` commands call it later.

### Design Principles
1. **The SDK does the protocol.** No hand-built authorization URLs, PKCE, token requests or client assertions.
2. **Loopback only.** The callback server binds to a loopback address, accepts one valid callback, and shuts down. It never listens on a routable interface.
3. **A bad callback cannot abort a login.** Requests without the expected `state` are rejected and ignored. Only the browser that was sent the authorization URL can finish or fail the login.
4. **Secrets stay out of everything.** Tokens, authorization codes, PKCE verifiers, client secrets and key material never appear in logs, prompts, the callback page or i18n data. `state` is part of the sign-in URL that is printed for the user, but it is never logged.
5. **Server messages are shown as received.** OAuth `error` and `error_description` from the authorization server are not translated (i18n spec §9). Everything the CLI itself says is localized.
6. **Non-interactive by default.** Getting a token never opens a browser or prompts. When it cannot produce one, it returns `ErrLoginRequired`. Only `Login` interacts.

---

## 2. Grants and Client Authentication

| `grant_type` | Supported | Notes |
| :--- | :--- | :--- |
| `authorization_code` (default) | Yes | Interactive, PKCE (S256), loopback redirect. |
| `client_credentials` | Yes | Non-interactive. Needs `client_secret_post` or `private_key_jwt`. |
| `device_code` | No | Not planned, because the authorization code flow covers the CLI. The context is valid, but a token request returns `ErrUnsupportedGrant`. |
| anything else | No | `ErrUnsupportedGrant`. |

| `auth_type` | Used for | Credential |
| :--- | :--- | :--- |
| `none` (default) | `authorization_code`, public client | none |
| `client_secret_post` | `authorization_code` or `client_credentials` | client secret (§6) |
| `private_key_jwt` | `authorization_code` or `client_credentials` | private JWK in `oauth.private_jwk` (§6) |

A public client (`none`) is the default and the recommended choice for interactive use: the CLI cannot keep a secret, and PKCE protects the code exchange. No secret is needed to log in, refresh or revoke. `none` with `client_credentials` is invalid and returns `ErrInvalidAuthConfig`.

Scopes are used exactly as written in `oauth.scopes`. The provider adds none, so a context that needs `openid` or `offline_access` lists them. Without `offline_access` many servers return no refresh token, and the user logs in again when the access token expires.

---

## 3. Shape of the Package

* A `Provider` is built from a context name and its resolved `Environment`. Optional fields: an HTTP client, an output writer for prompts (default stderr), a browser-opener hook, and a clock. The hooks exist so tests can replace the browser, the network and time. Credentials come from the `Environment`, not the `Provider` (§6).
* `Login` runs the grant and, unless told not to save, caches the token. Options: skip opening the browser, skip the cache write (for `--no-save`), and a timeout for the callback (default 5 minutes).
* `Token` returns a usable token with no user interaction (§4).
* `Revoke` asks the server to revoke the cached tokens, best effort (§4.1).
* `NewHTTPClient` builds the HTTP client for a context from its TLS settings (§7).
* Errors are three sentinels wrapped with localized messages, as in `profile`: `ErrLoginRequired`, `ErrUnsupportedGrant`, `ErrInvalidAuthConfig`.
* Prompts go to the output writer only. Stdout is never written, so `--print-token` output stays clean.

---

## 4. Getting a Token Without a User

The decision order for `Token`:

| Cache state | Action |
| :--- | :--- |
| Valid | Return it. No network call and no discovery. |
| Expired, has refresh token | Refresh. On success, save and return. On a server rejection (`invalid_grant`), log at `info` and continue to the next row. Any other failure (network, TLS, 5xx) is returned as an error and the cache is left alone. |
| Expired without refresh token, stale or missing | `client_credentials`: mint a new token if credentials are available, otherwise `ErrLoginRequired`. `authorization_code`: `ErrLoginRequired`. |

* **Rotation.** If a refresh response has no `refresh_token`, the previous one is kept. If it has one, the new one replaces it.
* **Mint** (`client_credentials` only). For `private_key_jwt`, the key file is read and a fresh assertion signed on every call. For `client_secret_post`, the secret must be available (§6).
* **Confidential `authorization_code` clients** (`client_secret_post`) need the secret to refresh too. Without it, the result is `ErrLoginRequired`.
* **`ErrLoginRequired` message.** It tells the user to run `login`, and mentions `IBM_AGENTID_CLIENT_SECRET` for secret-based contexts.
* A token with an unknown expiry is valid, as in the cache spec.

### 4.1 Revoking on Logout
`Revoke` is called by `logout` before the cache file is deleted. It is best effort and never blocks the logout.

* It revokes the **refresh token** (hint `refresh_token`) if the cache has one, otherwise the access token (hint `access_token`). Servers revoke the grant's other tokens along with the refresh token (RFC 7009 §2.1), so one call is enough.
* It does nothing, and returns no error, when there is no cached token, or when the server's metadata has no `revocation_endpoint`. The second case is logged at `info`.
* It needs client authentication like any token request (§6). If the credentials are not available (a secret-based context with no secret), it skips the call, logs at `info`, and returns no error.
* Any failure (network, TLS, a server error) is returned to the caller, which prints it as a warning and still deletes the cache file. The user is then logged out locally even if the server could not be reached.
* The request uses a short timeout of 10 seconds, so an unreachable server cannot hang `logout`.
* Discovery happens only when there is a token to revoke.

---

## 5. Interactive Login (`authorization_code`)

### 5.1 Steps
1. Validate the context (§2) and the redirect URI (§5.2).
2. Discover the server metadata (§8).
3. Bind the loopback listener, then build the redirect URI from the actual port. The listener must exist **before** the authorization URL is built, because the URL carries the redirect URI.
4. Ask the SDK for the authorization URL, `state` and PKCE verifier.
5. Open the browser, or print the URL (§5.3, §5.4).
6. Wait for the callback, the timeout, or context cancellation (§5.5).
7. Exchange the code through the SDK, which checks `state` again and reports authorization-server errors as `*auth.Error`.
8. Build the token and cache it, unless saving is off. If the cache write fails after a successful login, the error is returned together with the token, so a `--print-token` run is not lost.

The listener is closed on every return path.

### 5.2 Redirect URI and Listener

| `oauth.redirect_uri` | Listener | Redirect URI sent |
| :--- | :--- | :--- |
| unset | `127.0.0.1`, ephemeral port | `http://127.0.0.1:<port>/callback` |
| `http://127.0.0.1` or `http://127.0.0.1:0` | `127.0.0.1`, ephemeral port | same URI with the chosen port added |
| `http://127.0.0.1:8765/cb` | `127.0.0.1:8765` | as written |
| `http://[::1]:8765/cb` | `[::1]:8765` | as written |
| `http://localhost:8765/cb` | `127.0.0.1:8765` | as written |

* The scheme must be `http` and the host `127.0.0.1`, `[::1]` or `localhost`. Anything else, including `https` and routable hosts, is `ErrInvalidAuthConfig`. A loopback redirect is the only kind this CLI can receive.
* The path is used as written. An empty path stays empty in the redirect URI, because servers compare it exactly, and matches `/` on the listener.
* A fixed port that is in use is an error that names the port. The listener never falls back to another port, because the redirect URI may be registered with the server.
* Servers compare the redirect URI exactly, apart from the loopback port (RFC 8252 §7.3). A server that does not allow an ephemeral port needs a fixed `redirect_uri` registered with it.

### 5.3 Opening the Browser
The default opener runs the platform command with the URL as a single argument, never through a shell: `open` on macOS, `xdg-open` on Linux and others, `rundll32 url.dll,FileProtocolHandler` on Windows. Only `http` and `https` URLs are passed to it.

If the opener fails or is missing (SSH, container), the login does not fail. The URL has already been printed, so the provider keeps waiting. This works only when the browser runs on the same machine as the CLI. From a remote shell, the user forwards the callback port, which needs a fixed `redirect_uri`. An option skips the opener.

### 5.4 Prompts
Localized, one line each, written to the output writer:

* `Opening your browser to sign in. If it does not open, visit this URL:`, then the URL on its own line. With the no-browser option, the first sentence is dropped.
* `Waiting for sign-in to complete (press Ctrl+C to cancel)...`

The authorization URL holds the PKCE challenge and `state`, not secrets, so printing it is safe. The log gets only its scheme, host and path.

### 5.5 The Callback Server
A plain `net/http` server on the listener, with a 10 second header timeout and only the callback path registered.

| Request | Response | Effect |
| :--- | :--- | :--- |
| `GET` on the callback path, matching `state`, with `code` or `error` | `200` (`400` for `error`), result page | Completes the wait. |
| Same path, wrong or missing `state` | `400`, generic page | Ignored. Keeps waiting. |
| Same path, a second matching request | `400` | Ignored. |
| Any other path (for example `/favicon.ico`) | `404` | Ignored. |
| Any method other than `GET` | `405` | Ignored. |
| `Host` header that is not the redirect URI's host and port | `400` | Ignored (DNS-rebinding defense). |

* The first matching callback wins, and the server then shuts down gracefully. Its parameters go to the SDK unchanged.
* The result page is a fixed, localized, self-contained HTML document: a heading and a sentence ("You can close this window and return to the terminal."). It **never echoes** `code`, `state`, `error` or `error_description`, so a crafted URL cannot inject content. Responses set `Cache-Control: no-store`, `Referrer-Policy: no-referrer` and a UTF-8 HTML content type.
* If the server returned an OAuth error such as `access_denied`, the page says the sign-in did not complete, the terminal shows the error, and the login fails with the server's `*auth.Error`.
* **Issuer in the callback.** If the callback carries an `iss` parameter (RFC 9207), it must equal the issuer, ignoring trailing slashes. A mismatch fails the login with a localized error and the code is not exchanged, because it indicates the response came from a different authorization server. A missing `iss` is accepted, since many servers do not send one.
* The wait ends on the first of: a matching callback, the timeout (a localized error), or cancellation of the caller's context (its error).

---

## 6. Client Credentials

**Client secret.** The context's `oauth.client_secret` always wins. It is a `SecretSource`: a file reference is read on each use and its trailing line break is trimmed, and an inline value is used as written. `IBM_AGENTID_CLIENT_SECRET` applies only when the context has none. The secret is never written to the cache or logged. An unreadable secret file is `ErrSourceUnavailable`. For `client_secret_post` with no secret from either place, `Token` returns `ErrLoginRequired` and `Login` returns `ErrInvalidAuthConfig`. A public client needs no secret.

**Private key (`private_key_jwt`).** `oauth.private_jwk` is a `SecretSource`: a private JWK, as a file reference or inline. It is the key that signs the client assertion, and is not used to validate anything. On each use:

* The key is read and parsed as a single JWK. It must be a private key with `kid` and `alg` set, because the SDK needs both. A public JWK is rejected. Errors name the file, or `oauth.private_jwk` for an inline value, and the missing member, never its contents.
* The SDK builds the assertion: `iss` and `sub` are the client ID, `aud` is the issuer and the token endpoint, and the lifetime is 30 minutes.
* The key is not kept in memory between calls. A missing or unreadable file is an error, not `ErrLoginRequired`, because logging in again cannot fix it.

---

## 7. HTTP Client

One client serves discovery, authorization-server calls, and later the Agent Identity API.

* **Transport.** A clone of the default transport with TLS 1.2 as the minimum version, wrapped by the logging `RoundTripper`.
* **`tls.ca_cert`.** A `Source`: a PEM file reference or inline PEM, **added to** the system pool, not replacing it. An unreadable file is `ErrSourceUnavailable`. A value with no certificates is a localized error that names the path, or `tls.ca_cert` for an inline value.
* **`tls.insecure_skip_verify`.** Disables verification. Each client logs one `warn` record when it is built with this setting. Nothing else is printed, because the setting is deliberate and documented as for local development.
* **Timeout.** 30 seconds per request. The interactive wait is not an HTTP request and is not affected.
* **Redirects and proxies.** Go defaults: at most 10 redirects, proxy from the environment.

The same client goes to discovery and to the SDK client, so TLS settings and request logging apply to every call.

---

## 8. Discovery

* Trailing slashes on the issuer are trimmed before calling the SDK, which appends `/.well-known/openid-configuration` as is.
* The `issuer` in the response must equal the configured issuer, ignoring trailing slashes. A mismatch is an error, which guards against a server answering for a different issuer. The check is strict, with no warn-only mode.
* `token_endpoint` must be present for every grant, and `authorization_endpoint` for `authorization_code`. A missing endpoint is an error that names it.
* Discovery runs only when a network call is needed, so a valid cached token never triggers it. The document is not cached between runs.
* `code_challenge_methods_supported` is not checked. The SDK always sends S256, and a server that cannot honor it rejects the request.

---

## 9. Environment Variables

| Variable | Effect |
| :--- | :--- |
| `IBM_AGENTID_CLIENT_SECRET` | Client secret for a `client_secret_post` context, used only when the context has no saved `oauth.client_secret`. Never written to disk. |

The implementing change moves it from the "Planned" table in [`docs/environment-variables.md`](../../docs/environment-variables.md) to a new "Authentication" section. `IBM_AGENTID_TOKEN` and `IBM_AGENTID_SERVER` are not read here: the command layer decides whether to use the provider at all, and in stateless mode it does not (see [`internal/resource/spec.md`](../resource/spec.md) §7.1).

---

## 10. Logging

English `slog` records, as in the logging spec. HTTP traffic is logged by the `RoundTripper`, redacted.

| Level | Record |
| :--- | :--- |
| `info` | Login started and completed (context, grant, issuer, client ID). Token refreshed. Refresh rejected, falling back. Token revoked, or revocation skipped and why. |
| `warn` | `insecure_skip_verify` in use. |
| `debug` | Discovered endpoints (scheme, host, path). Listener address. Callback received (carried a `code` or an `error`, and the error code). Opener result. |

Never logged: access, refresh and ID tokens, authorization codes, `state`, PKCE verifiers, client secrets, client assertions, JWK contents, and the query string of the authorization URL.

---

## 11. Errors

| Situation | Result |
| :--- | :--- |
| No silent way to get a token | `ErrLoginRequired` |
| Unknown or unsupported `grant_type` | `ErrUnsupportedGrant` |
| Bad `auth_type` combination, missing key or secret, bad redirect URI | `ErrInvalidAuthConfig` |
| Authorization server returns an OAuth error | The SDK's `*auth.Error`, kept in the chain, text as received |
| Callback `iss` differs from the issuer | Localized error, no code exchange |
| Revocation fails | Error returned to the caller, which warns and still deletes the cache |
| Callback timeout | Localized error |
| Network, TLS or discovery failure | Localized error wrapping the cause |

New messages go in `en.json` under `auth.*`, each with a description. Template data holds names, paths, ports and durations, never credentials.

---

## 12. Testing

Tests run offline against an `httptest` authorization server that serves discovery and the token endpoint and checks what it receives. The browser is the opener hook: it parses the authorization URL and calls the redirect URI itself. They must cover:

1. **Happy path.** The S256 challenge matches the verifier sent to the token endpoint, `state` round-trips, the redirect host is `127.0.0.1` with the listener's port, the cached token has the right hash and expiry, and no-save writes nothing.
2. **Redirect URI table (§5.2)**, plus `https`, a routable host, an invalid URL, and a port already in use.
3. **Callback hardening.** A wrong or missing `state` is ignored and the login still completes afterwards. A second callback, other paths and methods, and a foreign `Host` are rejected. The page never contains the `code`, `state` or `error_description` that were sent. Response headers are set.
4. **Server error.** `access_denied` stops the login with the code.
5. **Cancel and timeout.** The listener is released, so the port can be bound again, after success, error, timeout and cancel.
6. **Browser.** An opener error does not fail the login and the URL was printed. The no-browser option never calls the opener. Prompts go to the output writer and nothing goes to stdout.
7. **`Token` decision table (§4).** Valid cache, refresh with rotation, refresh response without a new refresh token, `invalid_grant` falling through, a 5xx returned with the cache untouched, stale hash, missing file, unknown expiry.
8. **Client credentials.** Secret inline, from a file, and from the environment, with the context winning over the environment, a missing secret, an unreadable secret file, `private_key_jwt` from a file and inline with the signature verified at the server, missing `kid` or `alg`, an unreadable key file, and `none` with `client_credentials`.
9. **Discovery.** Trailing slash, issuer mismatch, missing endpoints, and no call when the cache is valid.
10. **HTTP client.** A TLS test server works with `ca_cert` (file and inline), fails without it, and works with `insecure_skip_verify` (with its `warn`). A missing CA file and a value with no certificates are rejected. Requests are logged as the logging spec describes.
11. **Unsupported grants.** `device_code` and an unknown value return `ErrUnsupportedGrant`.
12. **Callback issuer.** A matching `iss` (with and without a trailing slash) and a missing `iss` succeed. A different `iss` fails the login without calling the token endpoint.
13. **Revocation.** The refresh token is revoked with the right hint and client authentication, and the access token when there is no refresh token. Nothing happens, with no error, when there is no cached token, no `revocation_endpoint`, or no credentials. A server error and an unreachable server return an error and respect the timeout. Discovery is not called when there is nothing to revoke.
14. **No leaks.** The tokens, code, verifier, secret and assertion used in tests appear in no log, prompt or error string, and `state` appears in no log.

`gofmt`, `go vet` and `go test -race` pass, including the `docs` drift test and the `i18n` source-scan tests.

---

## 13. Out of Scope

* The device authorization grant. The SDK supports it, so it could be added later, but the authorization code flow is sufficient.
* Reading `exp` from JWT access tokens, ID-token validation, `nonce`, DPoP, mTLS client authentication, PAR, and Rich Authorization Requests.
* Locking across processes (see the cache spec).
* The `login` payload parser and `@path` resolution. The provider receives an `Environment` whose file references are already absolute.
* Flags, cobra and the command layer.

---

## 14. Dependency

The package imports `github.com/ibm-verify/verify-auth-go` at its latest tag (`v0.1.2` today), which includes the extra-parameters fix for the authorization code flow. Move to a newer tag when one is released.
