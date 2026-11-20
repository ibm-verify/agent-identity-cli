# Specification: Command Input Data

## 1. Overview

Commands that take structured input read it with `-d <path>` or `-d -` (stdin, which covers heredocs and pipes). `login` is the first. This package reads that data, parses the `login` payload and prints the `--template` skeleton.

### Design principles

1. **One input model.** `-d` and `--template` behave the same on every command that takes data.
2. **No secrets in argv.** `-d` takes a path or `-`, never a literal.
3. **Explicit file references.** Only properties typed `profile.Source` or `profile.SecretSource` can name a file, and only with a leading `@`. There is no generic walker, escape sequence or `~` expansion.
4. **Lenient.** Unknown properties are ignored. Required values are still checked.
5. **Secrets never echoed.** No error, log or template contains a value from the data.

The package does not parse flags, print, run a login or write `config.yaml`. The command layer does those.

---

## 2. Reading the data

* **Source.** A path, or `-` for stdin. There is no inline literal.
* **Format.** JSON if the data starts with `{`, YAML otherwise. Each is decoded by its own unmarshaller into the command's type.
* **Rejected.** Empty or whitespace-only input, input over 1 MiB, and stdin attached to a terminal (a character device), so the command never waits for typing.
* **Base directory.** The directory of the `-d` file, or the working directory for stdin. Relative `@` paths resolve against it.

---

## 3. File references

The properties typed `Source` or `SecretSource` are `tls.ca_cert`, `oauth.private_jwk` and `oauth.client_secret`.

* A value starting with `@` is a file. The path is resolved against the base directory, made absolute, and saved as `@` plus the absolute path. The parser checks that it is a regular file and does not read it.
* Anything else is an inline value, saved as written. A bare path without `@` is therefore inline, and the `oauth` package rejects it when used.
* `@` alone is an error. In YAML a value starting with `@` must be quoted.
* `Login.InlineSecrets` lists the inline `oauth.private_jwk` and `oauth.client_secret`, so the command layer can warn on stderr and suggest `@file`. `tls.ca_cert` is public and never listed.

---

## 4. The `login` payload

`Login` mirrors the payload in §3.1 of [`internal/profile/spec.md`](../profile/spec.md): `context` plus a `profile.Environment`.

* `context`: the `--context` value wins, then the payload, then `default`. It is checked with `profile.ValidateName`, so a bad name fails before any login starts.
* `server`: required, an `http` or `https` URL with a host and no user name or password.
* `oauth.issuer`, `oauth.client_id`: required and non-empty.
* Everything else is copied as written. Whether a grant, auth type, key or redirect URI is usable is decided by the `oauth` package.

The command layer builds a provider from `Login.Environment`, logs in, and saves the context only on success. If saving fails after a login, the login has already written a cached token, and the command layer deletes it with `profile.DeleteToken`.

---

## 5. Template

`--template` prints a skeleton of the payload, YAML by default (with a comment per property) or JSON with `-o json` (no comments).

* File properties show example paths such as `"@./jwk_private.json"`. The files do not exist. The template is a skeleton to edit, not data to use as is.
* Templates are English data, not localized.

---

## 6. Errors

| Error | When |
| :--- | :--- |
| `ErrInvalidInput` | Bad syntax or wrong type, empty or oversized input, terminal stdin, a bad `server`, a missing required property, an unknown template format. |
| `profile.ErrInvalidContextName` | The resolved context name is invalid. |
| `profile.ErrSourceUnavailable` | The `-d` file, or a file named by `@`, is missing, unreadable, not a regular file, or has no path. |

* Messages use `i18n` IDs in the `input.*` namespace.
* Decoder errors are replaced by a fixed message, because the YAML and JSON libraries can quote a value from the data.

---

## 7. Logging

* `debug`: the source (path or `stdin`) and size in bytes when data is read, and the property and absolute path of each resolved file reference.
* Never content or an inline value.

---

## 8. Out of scope

* Flags, cobra, printing and the command layer.
* The `IBM_AGENTID_TOKEN` and `IBM_AGENTID_SERVER` bypass.
* Other `@`-style prefixes, `@@` escapes, `~` expansion, and copying file contents into `~/.ibm-agentid`.
