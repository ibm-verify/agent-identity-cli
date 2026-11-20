# Specification: Internationalization (i18n) of User-Facing Messages

## 1. Overview
All user-facing text that is produced by library code (errors, warnings, status lines) is translatable. Translations live in embedded JSON catalogs and are looked up by a **stable message ID** using [`github.com/nicksnyder/go-i18n/v2`](https://github.com/nicksnyder/go-i18n). English is the source language and the fallback for everything.

This spec covers the `internal/i18n` package only. Wiring a `--lang` flag, localizing cobra help text, and translating the catalogs themselves are out of scope (see §9).

### Design Principles
1. **Stable IDs, not sentences as keys.** Call sites reference IDs such as `context.not_found`. Rewording the English text never breaks a translation lookup.
2. **English always works.** `en.json` is the source catalog and is always loaded. A missing translation silently falls back to English. A missing English entry is a programming error caught by tests, not at runtime.
3. **Named placeholders.** Messages use `{{.Name}}` template data, never positional `%s`/`%q`, so translators can reorder arguments for their language.
4. **Errors stay inspectable.** Localized errors wrap a sentinel/cause so `errors.Is` and `errors.As` keep working, and are translated when printed, not when created.
5. **Never break the CLI over i18n.** A bad or missing catalog, or an unknown language, degrades to English. The package never panics and never returns an empty message.
6. **Secrets never enter messages.** Template data must not contain tokens, client secrets, or key material.

---

## 2. Directory & File Layout

```text
internal/i18n/
├── spec.md
├── i18n.go            # Localizer, T(), TN(), Errorf(), Wrap(), SetLanguage()
├── i18n_test.go
└── locales/
    ├── en.json        # Source catalog (required)
    └── fr.json        # Example of a translation (added when translators contribute)
```

Catalogs are compiled into the binary with `//go:embed locales/*.json`. No catalog is read from disk at runtime, so there is no per-user translation directory to secure or keep in sync.

---

## 3. Message Catalog Format

Each file is a JSON object keyed by message ID. The file name (without `.json`) is a BCP 47 tag, for example `en`, `fr`, `pt-BR`.

```json
{
  "context.not_found": {
    "description": "A named context is not registered in config.yaml.",
    "other": "context \"{{.Name}}\" not found"
  },
  "context.deleted": {
    "description": "Printed after deleting contexts. Count is the number deleted.",
    "one": "{{.Count}} context deleted",
    "other": "{{.Count}} contexts deleted"
  }
}
```

* **`description`**: Context for translators. Required in `en.json`, optional elsewhere.
* **Plural forms** (`zero`, `one`, `two`, `few`, `many`, `other`) follow CLDR rules for the file's language. `other` is always required. `TN` passes the count as the plural count and also exposes it to the template as `{{.Count}}`.
* **Placeholders** are Go `text/template` fields (`{{.Name}}`). Only simple field substitution is allowed in catalogs, with no template functions, conditionals or pipelines, so translators cannot break rendering.
* **Quoting** is part of the message, not the call site. Messages quote values explicitly, for example `context \"{{.Name}}\" not found`, so languages with different quotation conventions can adapt.

### 3.1 Message ID Conventions
`<area>.<snake_case_name>`, lowercase, dot-separated, e.g. `config.read_failed`, `context.invalid_name`, `auth.token_expired`. IDs are never reused for a different meaning; retired IDs are deleted from all catalogs.

---

## 4. Go API (`internal/i18n`)

### 4.1 Core Functions

```go
// T returns the localized message for id. data supplies template fields.
// It never returns an empty string: unknown IDs return the ID itself.
func T(id string, data map[string]any) string

// TN is T for plural messages. count selects the plural form and is also
// available to the template as {{.Count}}.
func TN(id string, count int, data map[string]any) string

// SetLanguage selects the language used for all later calls. An empty tag
// auto-detects (§5). It returns the language actually in effect.
func SetLanguage(tag string) string
```

### 4.2 Localized Errors

```go
// Errorf returns an error whose text is rendered from id when Error() is
// called. It has no wrapped cause.
func Errorf(id string, data map[string]any) error

// Wrap is like Errorf and additionally wraps cause so that errors.Is/As match it.
func Wrap(cause error, id string, data map[string]any) error
```

* The returned type implements `Error() string` (localized at call time) and `Unwrap() error`.
* Sentinel errors (for example `profile.ErrContextNotFound`) remain plain `errors.New("...")` values used for matching only. Their text is **not** shown to users; the localized wrapper is.

```go
var ErrContextNotFound = errors.New("context not found") // for errors.Is, never printed directly

return nil, i18n.Wrap(ErrContextNotFound, "context.not_found", map[string]any{"Name": name})
```

### 4.3 Translating at Render Time
`Error()` looks up the active language when it is called, not when the error is created. An error created before `SetLanguage` still prints in the final language.

### 4.4 Concurrency
`SetLanguage`, `T`, `TN` and `Error()` are safe for concurrent use. Switching language swaps the active `*goi18n.Localizer` under a lock.

---

## 5. Language Selection

All environment variables are listed in [`docs/environment-variables.md`](../../docs/environment-variables.md).

Precedence, highest first:

1. Explicit `SetLanguage(tag)` argument (reserved for a future `--lang` flag).
2. **`IBM_AGENTID_LANG`** environment variable.
3. **`LC_ALL`**, then **`LC_MESSAGES`**, then **`LANG`** (POSIX locale variables).
4. English.

Normalization: strip encoding and modifier (`fr_CA.UTF-8@euro` → `fr-CA`), convert `_` to `-`. The values `C` and `POSIX` select English.

Matching uses `golang.org/x/text/language` through go-i18n's bundle matching: the requested tag is matched against the loaded catalogs, falling back from region to base language (`fr-CA` → `fr`) and then to English. Plural rules come from the bundle for the matched language.

---

## 6. Behavior on Failure

| Situation | Behavior |
| :--- | :--- |
| Language has no catalog | Use English. |
| ID missing from the active language | Use the English text for that ID. |
| ID missing from English | Return the ID string itself. A test fails the build (§7). |
| Template data missing a field | Render with go-i18n's default (`<no value>`) rather than failing. Tests cover every ID with sample data. |
| Malformed catalog JSON | Embedded catalogs are validated by a unit test, so this cannot ship. At runtime it is skipped, never fatal. |

---

## 7. Testing & Quality Gates

Unit tests in `internal/i18n` must enforce:

1. **Catalogs parse.** Every embedded file loads into a bundle without error.
2. **English completeness.** Every message ID referenced from Go code exists in `en.json`. Call sites pass IDs as string literals, and a test scans the module's source for `i18n.T(`, `i18n.TN(`, `i18n.Errorf(` and `i18n.Wrap(` literals to check this.
3. **No orphans.** Every ID in `en.json` is referenced from Go code (prevents dead strings accumulating).
4. **Translation parity.** For each non-English catalog, every ID also exists in `en.json`, and each message uses the same set of `{{.Field}}` placeholders as its English counterpart.
5. **English description.** Every entry in `en.json` has a non-empty `description`.
6. **Language matching.** Tag normalization, region→base fallback, environment precedence, and English fallback.
7. **Plurals.** `TN` selects `one` and `other` correctly for English.
8. **Errors.** `Wrap` preserves `errors.Is`, and `Error()` re-localizes after `SetLanguage`.

Untranslated IDs in non-English catalogs are **allowed** (they fall back to English); gate 4 only rejects extra or mismatched entries.

---

## 8. Workflow for Contributors

* **Adding a message:** add the ID with a `description` and `other` (and plural forms if needed) to `locales/en.json`, then use it at the call site.
* **Adding a language:** copy `en.json` to `locales/<tag>.json`, translate the message text, and keep or drop `description`. Untranslated entries may be removed and will fall back to English.
* **Changing wording:** edit the English text only. If the meaning changes, use a new ID and delete the old one in every catalog.

---

## 9. Out of Scope (for this package)

* A `--lang` flag, and localizing cobra command/flag help text. These belong to the command layer and will call `SetLanguage`.
* Localizing server-originated messages (for example OAuth `error_description` returned by the authorization server). These are shown verbatim.
* Date, number and currency formatting.
* Right-to-left layout concerns.
* Actually producing translations. Only `en.json` ships initially.
* Log lines and debug output, which stay English.

---

## 10. Open Questions for Review

1. **Named data type:** the spec uses `map[string]any`. A typed struct per message would be safer but heavier. Is the map acceptable?
2. **ID literal scanning (§7.2):** scanning source for string-literal IDs is simple and effective but forbids building IDs dynamically. Acceptable?
3. **Which package owns the sentinels?** The spec keeps sentinels in the domain package (`profile`) and the localized text in `i18n`. Confirm.
