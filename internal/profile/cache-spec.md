# Specification: Token Cache

## 1. Overview
The token cache stores the short-lived OAuth tokens for each context in `~/.ibm-agentid/cache/<context>.json`, so commands can reuse a token, refresh it silently, or tell the caller that a new `login` is needed. The file format and the lifecycle rules come from §3.3 and §4.2 of [`spec.md`](spec.md). This document covers the Go API and the behavior those sections leave open.

The cache lives in `internal/profile` because it shares `Dir()`, `writeFileAtomic` and context-name validation with `config.yaml`.

### Design Principles
1. **Cache only, never configuration.** Routine refreshes write `cache/`, never `config.yaml`.
2. **Secrets stay in the cache.** Token values are written only to the cache file (`0600`, directory `0700`, atomic rename). They are never logged, never placed in error messages and never put in i18n data.
3. **A bad cache is a miss.** A missing, unreadable, corrupt or stale file never fails a command. The caller falls back to refresh or `login`.
4. **No OAuth in this package.** It stores and judges tokens. It does not call the authorization server, so it has no dependency on `verify-auth-go`.

---

## 2. Data Model

```go
type Token struct {
    ConfigHash   string    `json:"config_hash"`
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token,omitempty"`
    TokenType    string    `json:"token_type"`
    ExpiresAt    time.Time `json:"expires_at"` // RFC 3339, UTC
}
```

The JSON layout is exactly §3.3 of the profile spec. `refresh_token` is omitted when the grant has none (for example `client_credentials`).

### 2.1 Building a Token

```go
// NewToken stamps a token response for env. expiresIn is the "expires_in"
// value in seconds; now is the time the response was received.
func NewToken(env *Environment, accessToken, refreshToken, tokenType string, expiresIn int64, now time.Time) *Token
```

* `ExpiresAt = now + expiresIn seconds`, in UTC. The OAuth response carries only a relative lifetime, so the caller supplies `now`, and tests can pin it.
* `ConfigHash` is computed from `env` (§3).
* An empty `tokenType` is stored as `Bearer`.
* **Refresh-token rotation is the caller's job.** When a refresh response has no `refresh_token`, the provider passes the previous one in, so a refresh never drops it.
* When `expiresIn` is zero or negative (the server sent no `expires_in`), `ExpiresAt` is the zero time, meaning the expiry is unknown. Such a token is treated as valid (§4).

---

## 3. Config Hash

`ConfigHash(env *Environment) string` returns `sha256:<hex>` over these fields of the context:

| Field | Normalization |
| :--- | :--- |
| `server` | as stored |
| `oauth.issuer` | as stored |
| `oauth.client_id` | as stored |
| `oauth.grant_type` | empty becomes `authorization_code`, so omitting it and writing the default hash the same |
| `oauth.scopes` | copy, sorted, duplicates removed, so order does not matter |

The hash input is the JSON encoding of `[server, issuer, client_id, grant_type, [scope, ...]]`. Encoding the fields as a structure, instead of joining them into one string, means no field value can run into the next: `["a","bc"]` and `["ab","c"]` hash differently.

`auth_type`, `private_jwk`, `redirect_uri` and `tls` are not part of the hash. Changing them does not change what the token is for, so the cached token stays valid.

---

## 4. Validity

```go
const expirySkew = 30 * time.Second

func (t *Token) Expired(now time.Time) bool // ExpiresAt is set and now >= ExpiresAt - skew
```

A token within 30 seconds of `ExpiresAt` counts as expired, as in §4.2 of the profile spec. A token with no known expiry (zero `ExpiresAt`) is never expired: it is used as is, and if the resource API rejects it, the caller renews it then.

### 4.1 Lookup

```go
type Status int

const (
    TokenMissing Status = iota // no file, unreadable, or corrupt
    TokenStale                 // config_hash does not match the context
    TokenExpired               // matches, but past expiry (see RefreshToken)
    TokenValid                 // matches and still usable
)

// LookupToken reads the cache for the named context and judges it against env.
// It returns a nil *Token for TokenMissing and TokenStale.
func LookupToken(name string, env *Environment, now time.Time) (*Token, Status, error)
```

What the caller does with each status:

| Status | Caller action |
| :--- | :--- |
| `TokenValid` | Use `AccessToken`. |
| `TokenExpired` with a `RefreshToken` | Refresh silently, then `SaveToken`. |
| `TokenExpired` without a `RefreshToken` | Re-mint if the grant allows it (`private_key_jwt`, or `client_secret` from `IBM_AGENTID_CLIENT_SECRET`); otherwise ask the user to run `login`. |
| `TokenStale` or `TokenMissing` | Same as the row above: mint, or ask for `login`. |

`LookupToken` returns an error only for an invalid context name or an unresolvable state directory. File problems become `TokenMissing` and are logged at `warn` through `slog` (English, path and reason only, never contents). A `TokenStale` result is logged at `warn` too, as the logging spec describes.

`LookupToken` does not delete anything. A stale or corrupt file is overwritten by the next `SaveToken`.

---

## 5. Storage API

All functions take a context name and validate it with the same rule as `Config.Set` (`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`). That keeps the name from escaping `cache/`, because it becomes a file name. An invalid name returns an error wrapping `ErrInvalidContextName`.

```go
func TokenPath(name string) (string, error)      // <dir>/cache/<name>.json
func SaveToken(name string, t *Token) error      // atomic, 0600
func DeleteToken(name string) error              // missing file is not an error
func DeleteAllTokens() error                     // every *.json in cache/; missing dir is not an error
func RenameToken(oldName, newName string) error  // missing source is not an error
```

* `SaveToken` reuses `writeFileAtomic`, so the cache directory is created `0700` on first use and the file is `0600`, with no partial writes.
* `DeleteAllTokens` removes only `*.json` files in `cache/`. It leaves other files and the directory alone.
* `RenameToken` overwrites a file that already exists under the new name, because `Config.Rename` has already refused a name that is registered. The only possible leftover is a stale file from a deleted context.

### 5.1 Relationship to `Config`
`Config.Delete` and `Config.Rename` change `config.yaml` only. The command layer calls `DeleteToken` or `RenameToken` next. The profile spec lists the cache change as part of `context delete` and `context rename`. If the second step fails, the cache holds an orphan file or lacks the migrated one. Both are harmless, because §4 treats a missing or stale file as a miss.

---

## 6. Errors & Logging

No new user-facing messages. `LookupToken` never fails on file problems, and the write and delete functions return the underlying `os` error, as `Load` and `Save` do. An invalid name uses the existing `context.invalid_name` message.

Log lines (English, `slog`): cache hit or miss and the reason at `debug`, and a corrupt or mismatched cache at `warn`. They carry the context name and path, never a token, a hash input or any part of the file contents.

---

## 7. Testing & Quality Gates

Tests run with `XDG_CONFIG_HOME` pointed at a temporary directory:

1. **Round trip.** `SaveToken` then `LookupToken` returns an equal token. `expires_at` keeps its value.
2. **Permissions and atomicity.** The file is `0600`, `cache/` is `0700`, and no `*.tmp-*` files remain.
3. **Expiry.** The boundary at exactly 30 seconds before `ExpiresAt`, a zero `ExpiresAt` (never expired), and a pinned `now` in `NewToken`, including `expires_in` of zero.
4. **Hash.** The same context gives the same hash. Each hashed field changes it. Scope order and duplicates do not. An omitted `grant_type` equals the explicit default. The `["a","bc"]` versus `["ab","c"]` case differs. Fields outside §3 do not change it.
5. **Status.** Valid, expired, stale, missing, and a corrupt file (not JSON, or empty) each give the right status and no error.
6. **Names.** `../x`, `a/b`, empty and over-long names are rejected by every function. Nothing is written outside `cache/`.
7. **Delete and rename.** Missing files and a missing directory are not errors. `DeleteAllTokens` leaves non-`.json` files in place. `RenameToken` overwrites an existing target.
8. **No leaks.** Neither the logs nor the error text from any function contain the token values used in the test.

`gofmt`, `go vet` and `go test -race` must pass, including the `docs` and `i18n` source-scan tests.

---

## 8. Out of Scope

* Calling the token endpoint, running login flows, and rotating refresh tokens. These belong to the OAuth token provider.
* Locking across processes. Writes are atomic and the last writer wins. Two commands refreshing a rotating refresh token at the same moment can invalidate each other's token, and the loser is asked to `login` again.
* Renewing a token whose expiry is unknown. If the resource API rejects it, the provider deals with renewal then.
* Normalizing `server` and `issuer` before hashing. A trailing-slash change invalidates the cached token.
* Encrypting the cache or using an OS keychain. The protection is file permissions, as for `config.yaml`.
* `IBM_AGENTID_TOKEN` and `IBM_AGENTID_SERVER` (the stateless CI mode). In that mode the cache is not read or written, and the check belongs to the command layer.
* Printing token validity in `context list`. The command layer calls `LookupToken` for that.

