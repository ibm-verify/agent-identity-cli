# Specification: Diagnostic Logging

## 1. Overview
The CLI uses Go's standard `log/slog` for diagnostic logging. Logs exist to troubleshoot problems (failed logins, token refresh, TLS errors, server responses), not to report results to the user. Normal command output and user-facing errors are separate (stdout, and localized errors on stderr).

This spec covers the `internal/logging` package only. Registering the flags and wiring them into the command layer is out of scope (see §11).

### Design Principles
1. **On by default, quiet, and bounded.** Every run appends a few `info` records to a log file in the CLI's state directory, so a failure can be diagnosed after the fact without asking the user to re-run with a flag. `info` records never contain payloads. Disk use is capped by rotation (§8). Users can turn logging off.
2. **Verbose on demand.** `debug` adds decisions, inputs, and HTTP headers and payloads. It is enabled for the one run that misbehaves, by flag or environment variable.
3. **Credentials never reach a log, at any level.** Redaction is enforced centrally, not left to each call site (§7). Payloads logged at `debug` can still contain non-credential business data (names, agent definitions), so a debug log should be reviewed before it is shared.
4. **Never break the command.** If the log destination cannot be opened, the command continues with logging disabled. Logging failures are never fatal.
5. **Quiet when nobody asked.** Because logging is on by default, a failure to open the log (read-only home, no home directory, containers) is silent. Only logging that the user explicitly configured produces a warning (§9).
6. **Plain `slog`.** Library code calls `slog.Debug(...)` or `slog.Default()`. No custom logger type and no logger threaded through every function signature.
7. **English only.** Log messages are not localized, because they are read by developers and support, and have to be searchable. User-facing errors and the warnings in §9 are localized per the i18n spec.

---

## 2. Destinations: File or Stderr

| Destination | Good for | Weakness |
| :--- | :--- | :--- |
| **File** in `~/.ibm-agentid/logs/` | Interactive flows (browser login, device code) where stderr is cluttered by prompts; "attach the log to the ticket"; looking at the previous run after the fact | Persistent, so any secret that slips through persists too. Needs rotation and permissions handling. |
| **Stderr** | CI/CD, containers and pipelines, where the filesystem is often discarded; quick `--log-level debug` in a terminal | Mixes with prompts in interactive flows. Gone when the terminal scrolls. |

**Defaults:**

* Nothing configured: **file**, level `info`.
* `output=stderr`: stderr only; no file is created.
* `level=off`: nothing is written anywhere.

Alternatives considered and rejected:

* **OS-native logging** (journald, macOS unified logging, Windows Event Log). Not portable and harder for users to attach to a ticket.
* **OpenTelemetry tracing.** Disproportionate for a CLI with no backend to receive it.
* **A `diagnose` command that bundles logs and config.** A useful follow-up that needs this package first.

---

## 3. Directory & File Layout

```text
~/.ibm-agentid/                  # 0700 (respects $XDG_CONFIG_HOME, as in the profile spec)
└── logs/                        # 0700, created on first use
    ├── ibm-agentid.log          # Current log (0600)
    ├── ibm-agentid.log.1        # Most recent rotated file (0600)
    ├── ...
    └── ibm-agentid.log.5        # Oldest kept
```

The directory resolution is the same function used for `config.yaml` (`profile.Dir()`), so the two never disagree.

---

## 4. Configuration

All environment variables are listed in [`docs/environment-variables.md`](../../docs/environment-variables.md).

| Setting | Flag (future) | Environment variable | Values | Default |
| :--- | :--- | :--- | :--- | :--- |
| Level | `--log-level` | `IBM_AGENTID_LOG_LEVEL` | `off`, `error`, `warn`, `info`, `debug` | `info` |
| Destination | `--log-output` | `IBM_AGENTID_LOG_OUTPUT` | `file`, `stderr` | `file` |
| File path | `--log-file` | `IBM_AGENTID_LOG_FILE` | any path | `<dir>/logs/ibm-agentid.log` |
| Format | none | `IBM_AGENTID_LOG_FORMAT` | `text`, `json` | `text` |
| Max file size | none | `IBM_AGENTID_LOG_MAX_SIZE` | whole number of MiB, at least 1 | `5` |
| Rotated files kept | none | `IBM_AGENTID_LOG_MAX_FILES` | whole number, 0 or more | `5` |

Precedence per setting: explicit option (flag), then environment variable, then default. Setting a file path implies `file` output.

Logging does not depend on any other CLI mode. In particular, the stateless CI mode (`IBM_AGENTID_TOKEN` and `IBM_AGENTID_SERVER` set, so `config.yaml` and `cache/` are not used) logs to the default file like any other run. Users who do not want that set `IBM_AGENTID_LOG_LEVEL=off` or `IBM_AGENTID_LOG_OUTPUT=stderr`.

A run is **explicitly configured** when level, output or file path is set by an option or an environment variable. This distinction only affects warnings (§9).

Unknown or invalid values fall back to the default for that setting. When the run is explicitly configured, one warning is printed.

**Levels** are the `slog` levels. There is no separate `trace` level.

* `error`: a command failed.
* `warn`: something unexpected that was recovered (for example a cached token ignored because the config hash changed).
* `info`: major steps (start-up record, context resolved, login started or completed, token refreshed) and one summary line per HTTP request (§6). Never payloads.
* `debug`: decisions and inputs (grant type chosen, discovery endpoints, cache hit or miss), and HTTP headers and payloads (§6), all redacted (§7).

**Format.** `text` (`slog.TextHandler`) by default. `json` (`slog.JSONHandler`) is available through the environment variable. File and stderr use the same format.

---

## 5. Go API (`internal/logging`)

```go
// Options carries the same settings as the flags. All fields are strings so
// that "unset" can be told apart from a real value; the environment fills in
// whatever is empty.
type Options struct {
    Level    string    // "", "off", "error", "warn", "info", "debug"
    Output   string    // "", "file", "stderr"
    File     string    // explicit file path; implies Output "file"
    Format   string    // "", "text", "json"
    MaxSize  string    // MiB; "" uses the environment or the default
    MaxFiles string    // rotated files kept; "" uses the environment or the default
    Stderr   io.Writer // where stderr output and warnings go; defaults to os.Stderr
}

// Init configures the default slog logger from opts and the environment and
// returns a function that flushes and closes the log file. It never returns an
// error that should stop the command: on failure it leaves logging disabled
// (with a warning only if the run was explicitly configured).
func Init(opts Options) (cleanup func())
```

* `Init` calls `slog.SetDefault`. Call it once, early in startup, and `defer cleanup()`.
* Each record carries a `pid` attribute, so interleaved lines from concurrent invocations can be told apart.
* The first record of a run (at `info`) states the CLI version, OS and architecture, and the command line **without argument values** (§7). This makes a pasted log useful on its own.
* When the level is `off`, or the destination could not be opened, `Init` installs a handler whose `Enabled` always returns false, so `slog.Debug(...)` calls cost one branch and build no record.

### 5.1 Call-site conventions

```go
slog.Debug("resolved context", "context", name, "source", source)
slog.Info("token refreshed", "context", name, "expires_at", expiresAt)
slog.Error("discovery failed", "issuer", issuer, "error", err)
```

* Message: a short lowercase phrase, no trailing punctuation.
* Data goes in attributes, not in the message string.
* Pass errors as the `error` attribute, not formatted into the message.
* Use the `context` attribute name for the CLI context, so a log can be filtered by it.
* Do not put payloads in `info` records. Payload logging is done only by the HTTP wrapper at `debug`.

---

## 6. HTTP Logging

`logging.RoundTripper(next http.RoundTripper) http.RoundTripper` wraps the transport the CLI builds from the TLS settings, so discovery, token, introspection and application API calls are all covered.

**At `info`:** one record per request with method, scheme, host, path, status code, duration and the value of the response's `X-Correlation-ID` header (attribute `correlation_id`, omitted when the header is absent). The correlation ID lets support match a log line to a server-side request. The query string is omitted, and no other headers and no bodies are logged.

**At `debug`**, in addition:

* **Query string**, with sensitive parameter values redacted (§7).
* **Request and response headers.** Sensitive headers are redacted (§7). The `Authorization` value keeps its scheme and masks the rest, for example `Bearer [REDACTED]`.
* **Request and response bodies**, subject to the rules below.

### 6.1 Body rules

* **Logged only for text content.** `application/json` and any `+json` type, `application/x-www-form-urlencoded`, and `text/*`. Any other type is logged as `[body omitted: <content-type>, <n> bytes]`.
* **Size cap: 32 KiB.** A body larger than the cap is logged as `[body omitted: larger than 32 KiB]`. It is not truncated, because redacting half a document could leave part of a secret in the log.
* **The command is not affected.**
  * *Request bodies:* read once, logged, and replaced with an identical reader (or `GetBody` is used when present). The server receives the same bytes.
  * *Response bodies:* wrapped in a reader that captures up to the cap as the caller reads, and writes the record when the body is closed. The wrapper does not read ahead, so streaming responses are unchanged. A body that is never closed produces no body record.
* **Redaction before writing.** Bodies are never logged raw (§7.2).

---

## 7. Redaction

Redaction is applied centrally, in a `slog.HandlerOptions.ReplaceAttr` for attributes and in the HTTP wrapper for headers, query strings and bodies, all using the same functions. Call sites cannot forget it.

### 7.1 Sensitive keys

A key is sensitive when, after lowercasing and replacing `-` with `_`, it either:

* **equals** one of: `token`, `access_token`, `refresh_token`, `id_token`, `client_secret`, `client_assertion`, `assertion`, `code`, `device_code`, `code_verifier`, `password`, `secret`, `authorization`, `proxy_authorization`, `cookie`, `set_cookie`, `key`, `private_key`, `jwk`, `private_jwk`; or
* **ends with** `_token`, `_secret`, `_password`, `_assertion` or `_key` (so `x_api_key` and `session_token` match, while `token_type` does not).

Group-qualified names (`oauth.client_secret`) are matched on their final segment. A match replaces the value with `[REDACTED]`. `Authorization` and `Proxy-Authorization` keep their scheme word.

### 7.2 Structured payloads

* **JSON:** parsed, every value under a sensitive key is replaced (at any depth, including inside arrays), remaining strings go through §7.3, and the result is re-encoded compactly. If parsing fails, the body is handled as plain text.
* **Form-encoded** (request bodies, query strings, the loopback callback URL): parsed, values of sensitive keys replaced, then re-encoded.
* **Other text:** handled by §7.3 only.

### 7.3 By value shape
Any string value that looks like a credential is masked even under an innocuous key:

* `Bearer <anything>` or `Basic <anything>`: keep only the scheme.
* Three base64url segments separated by dots (a JWT): replaced with `[JWT REDACTED]`.
* Embedded `key=value` or `"key":"value"` pairs for the sensitive keys in §7.1, inside text that could not be parsed structurally.

### 7.4 Other rules

* **Command line:** only the executable name, subcommand path and flag *names* are logged, never flag values. A file path given with `-f` is logged (useful and not secret); `-` (stdin) is logged as `-`.
* **Errors:** `error` attributes go through §7.3, since an error text can embed a URL.
* Redaction is conservative. A false positive hides some debug text; a false negative leaks a secret, and is the failure mode the tests focus on.

---

## 8. File Handling

* **Open:** `O_WRONLY|O_CREATE|O_APPEND`, mode `0600`; the `logs/` directory is created with `0700`. If the file already exists with broader permissions, it is tightened to `0600`.
* **Concurrency:** several CLI invocations may append at once (for example a script). Each record is written with a single `Write` call, which `O_APPEND` keeps whole on local POSIX filesystems. Network filesystems are best effort.
* **Rotation:** checked once at `Init` with a single `Stat`, not on every write. If the current file is larger than the max size, it is renamed to `.1` (shifting `.1` to `.2` and so on) and a new file is started. At most the configured number of rotated files is kept, and older ones are deleted. With the defaults, disk use is bounded at about 30 MiB (the current file plus five of 5 MiB). A max-files value of `0` keeps no history: the full file is discarded when it fills. A single run that writes more than the max size is not truncated mid-run.
* **Rotation races:** two processes rotating simultaneously may lose a rotated file but never corrupt the live log. Rename failures are ignored.
* **Cleanup:** the returned `cleanup` closes the file. No background goroutines.
* **Windows:** permissions are best effort (the mode bits are largely ignored), as with `config.yaml`.

---

## 9. Behavior on Failure

| Situation | Behavior |
| :--- | :--- |
| Level `off` | Discard handler. No file touched. |
| Log directory or file cannot be created, run **not** explicitly configured | **Silent.** Logging is disabled for the run. |
| Log directory or file cannot be created, run explicitly configured | One localized warning on stderr, logging disabled for the run. Exit code unaffected. |
| Invalid value for a setting | Default for that setting; one localized warning only if explicitly configured. |
| Write error while logging | Ignored by `slog`. The command continues. |
| `stderr` output | Written to the configured `Stderr` writer. Warnings use the same writer. |

---

## 10. Testing & Quality Gates

1. **Default on.** `Init` with no options and no environment, in a temporary home directory, creates `logs/ibm-agentid.log` with `0600` inside a `0700` directory, and writes the start-up record at `info`.
2. **Level off.** Creates no file or directory.
3. **Configuration precedence.** Option beats environment beats default, for each setting. Invalid values fall back to the default.
4. **Levels.** Each level filters correctly; `debug` records do not appear at `info`.
5. **Silent vs warned failure.** An unwritable home directory produces no output when nothing was configured, and one warning when level, output or file was set. In both cases there is no panic or error return.
6. **File permissions.** A pre-existing broad-permission log file is tightened.
7. **Rotation.** An over-size file is rotated; with the defaults at most five rotated files remain; numbering shifts correctly; `MAX_SIZE` and `MAX_FILES` overrides are honored, including `MAX_FILES=0`.
8. **Redaction by key.** Table test over every key and suffix in §7.1, including group-qualified names and header-style names (`X-Api-Key`).
9. **Redaction by value.** Bearer/Basic values, JWTs, and `key=value` pairs inside text are masked.
10. **Body redaction.** JSON (nested objects and arrays), form-encoded, unparseable text, non-text content (omitted), and over-cap bodies (omitted) all behave as in §6.1 and §7.2.
11. **No secret leakage end to end.** A test sends requests through the HTTP wrapper to an `httptest` server using a known token, client secret, authorization code, refresh token and assertion in headers, query and bodies, with a realistic token response, runs at `debug`, and asserts none of those values appear in the output. The same run at `info` shows no header or payload content at all.
12. **The command is unaffected.** The server receives the request body byte for byte, and the caller reads the full response body, with logging at `debug`. A streamed response is not buffered by the wrapper.
13. **Concurrency.** Records written by parallel goroutines through the file handler do not interleave within a line (run with `-race`).
14. **Command-line scrub.** Flag values are not present in the start-up record.

---

## 11. Out of Scope (for this package)

* Registering `--log-level`, `--log-output` and `--log-file` and calling `Init`. These belong to the command layer.
* A `diagnose` or `support-bundle` command.
* Remote log shipping, metrics and tracing.
* Localizing log messages.
* Log-file locking for strict cross-process ordering.

---

## 12. Decisions Recorded from Review

| Question | Decision |
| :--- | :--- |
| Default file format | `text` |
| Rotation | 5 rotated files kept (was 3); size and count overridable by environment variable |
| Always-on logging | Yes: `info` to a file by default |
| `trace` level | Dropped; `debug` also logs HTTP payloads |
| Broad key-name redaction (`code`, `key`) | Kept as written |
| CI mode (`IBM_AGENTID_TOKEN` + `IBM_AGENTID_SERVER`) | Logs to the default file like any other run; turned off with the level or output setting |
| HTTP summary line at `info` | Kept: one record per request, including the `X-Correlation-ID` response header |
| Payloads at `debug` | Existing safeguards are sufficient (redaction, text-only, 32 KiB cap); no extra opt-in |

Documentation for the CLI should tell users that a `debug` log can contain non-credential business data and should be reviewed before it is shared.
