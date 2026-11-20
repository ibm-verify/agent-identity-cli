# Specification: Resource Commands

## 1. Overview

The resource commands manage server-side objects through the CRUD functions in `verify-sdk-go`: agents, clients and server connections first, others later. There are five verbs.

```
ibm-agentid get     <resource> [<id> | --name <name>]
ibm-agentid create  <resource> (-d <file|-> | --template)
ibm-agentid replace <resource> (<id> | --name <name>) (-d <file|-> | --template)
ibm-agentid patch   <resource> (<id> | --name <name>) (-d <file|-> | --template)
ibm-agentid delete  <resource> (<id> | --name <name>) [--yes]
```

`get` is also the list command.

### Design principles

1. **Verb first.** The verb is the parent command and the resource is its subcommand.
2. **Just the data.** Input and output are the SDK payload. There is no `kind`, `apiVersion` or `metadata` wrapper.
3. **One definition per resource.** Behavior lives in the verbs. A resource is data: names, payload type, template and the SDK calls. Adding a resource adds one definition and no verb code.
4. **Round trip.** The output of `get <resource> <id>` is valid input for `replace` and `patch`, and for `create` after read-only properties are dropped.
5. **Explicit target.** A target is an `<id>` or `--name`, never a guess.
6. **Stdout is data.** Confirmations, warnings and prompts go to stderr.
7. **No secrets in argv, none echoed.** Same as [`internal/input/spec.md`](../input/spec.md).

The package does not parse flags or print. The command layer does those. It does not build the SDK client or resolve contexts. `profile` and `oauth` do.

### Change to the development rules

[`docs/development.md`](../../docs/development.md) says commands are plain cobra with "no registry, no executor interface". That was written for `login` and `context`, and it still holds for them. The resource verbs share one definition per resource, and `NewRoot` lists the definitions by hand. There is no `init()` self-registration, so the command list is readable from the source. `development.md` is updated with this spec (see §11).

---

## 2. Resources

| Resource | Aliases |
| :--- | :--- |
| `agents` | `agent` |
| `clients` | `client` |
| `server-connections` | `server-connection`, `server_connections`, `server_connection` |

* The same names are used under every verb. Aliases are cobra aliases, so help shows the canonical name.
* A definition holds: the names and aliases, the payload type, the table columns, the template, the read-only property names, the secret property names, which list flags the API supports, and the SDK functions (get by ID, list, create, replace, patch, delete). There is no get-by-name function: a name is resolved through list (§4). A verb is offered for a resource only if the definition has the function. An unsupported verb is not registered, so `--help` stays truthful.
* The SDK functions are fields of the definition. Tests substitute fakes and need no server.
* The payload types are generated from the OpenAPI document with `oapi-codegen`. This package does not redefine them.
* **Both tags.** Input is decoded by the YAML decoder or the JSON decoder, depending on its first character, so every property needs a `json` and a `yaml` tag with the same name. Generate the models with the `struct-tags` output option, for example a `yaml` tag with the template `{{.FieldName}}{{if .OmitEmpty}},omitempty{{end}}{{if .OmitZero}},omitzero{{end}}`. A model without the `yaml` tag silently drops properties written in YAML, so each resource has a test that checks every tag (see §9).
* **Pointers.** Optional properties are pointers with `omitempty`, as `oapi-codegen` generates them. For `patch` this is what makes "only the properties in the data" work with no diffing: a property that was not written is nil and is not sent.

---

## 3. Data

### Output

* `get <id>`, `get --name`, `create`, `replace` and `patch` print the object: the SDK payload as returned.
* A list prints an array of those objects. Paging details are not printed. A caller pages with `--limit` and `--page`.
* `-o yaml` (the default for objects) and `-o json` print the same data. A list also supports `-o table` (§5.1).

### Input

* The data is read with `-d`, using the rules in [`internal/input/spec.md`](../input/spec.md) §2: a path or `-`, JSON if it starts with `{`, YAML otherwise, 1 MiB limit, no terminal stdin.
* The document is the bare payload. A top-level array is `ErrInvalidInput`. One object per call.
* Parsing is lenient: unknown properties are ignored. Required values are checked by the SDK or the server, not copied into this package as rules.
* The data is decoded straight into the payload type. There is no intermediate map.
* Read-only properties (such as server-assigned IDs and timestamps) are dropped from every payload, so a `get` output can be reused. The definition supplies a function that clears them. The `id` is remembered first, to check the target (§4).
* A secret that is still the mask (`********`) is dropped too, so that an unedited `get` output does not overwrite the real secret. The definition does this in the same function.
* The data is parsed, and an empty `patch` refused, before the token is fetched. Bad data fails without a network call or a sign-in.

---

## 4. Targets

`replace`, `patch` and `delete` need exactly one target. `get` takes at most one.

| Given | Meaning |
| :--- | :--- |
| `<id>` positional | Look up by ID. |
| `--name <name>` | Look up by name. |
| Both | `ErrInvalidInput`. |
| Neither | `get` lists. Other verbs fail with `ErrInvalidInput`. |

* The positional ID is a single argument. `create` takes no target.
* **Name resolution.** The CLI lists with the name as the search filter (the server's search is a substring match, and names are unique) and requires an exact match. One match is the target. No match is `ErrNotFound`. More than one is `ErrAmbiguousName`, and the message lists the IDs. The CLI never picks one.
* `replace` and `patch` resolve the target first, then send the payload to that ID.
* If the payload has an ID that differs from the target, the command fails with `ErrInvalidInput` and sends nothing. This prevents a copied file from overwriting the wrong object. An equal or absent ID is fine. The name is not compared, because `replace` and `patch` can rename.

---

## 5. Verbs

### 5.1 `get`

* With a target, prints one object. Without, prints a list.
* `-o table|yaml|json`. The default is `yaml` for one object and `table` for a list. `--template` is not a `get` flag.
* List flags `--limit`, `--page`, `--sort` and `--search` are defined once and added only where the definition says the API supports them.
* `--name` with a list flag is `ErrInvalidInput`.
* The table has the columns from the definition, with `ID` and `NAME` first. No colors, one header row. It is for people. Scripts use `-o json`.
* An empty list prints an empty table or array to stdout, not an error. A "no resources found" note goes to stderr.

### 5.2 `create`

* Reads the payload, calls the SDK create, prints the created object to stdout in `-o` format (default `yaml`) and `Created <resource> <id>` to stderr.
* The definition's create function returns the created object. If the API returns only an ID, the function fetches the object, so that the output is the same either way.
* All output goes through the definition's masking function. A resource whose server returns a secret only once (for example a generated client secret) must show it on create, and the first such resource decides how, together with a stderr note that it cannot be read again. Foo has no such secret.
* `create` is not idempotent. A name conflict is the server's error, shown as received.

### 5.3 `replace`

* Full replacement, the PUT. Every property absent from the payload is cleared or defaulted by the server as its contract says. The CLI does not merge.
* There is no prompt, because the object still exists afterwards. The help text says what a full replace means and points to `patch`.
* Output is the same as `create`, and the stderr line is `Replaced <resource> <id>`.

### 5.4 `patch`

* Partial update. The data has the same layout as the object, with only the properties to change. A property that is not written is nil in the payload type, so it is not sent.
* The CLI does not diff or merge. What a `null`, nested or array value means is defined by the server's PATCH contract, which the SDK function wraps, and each resource's help says what it is. Help text states this, so users do not assume `replace` semantics.
* An empty payload (no properties after read-only fields are dropped) is `ErrInvalidInput`.
* Output is the same as `create`, and the stderr line is `Patched <resource> <id>`.
* The data is a subset of the object, not a list of `key=value` pairs. It round-trips with `get`, handles nested properties, and keeps values, which may be secrets, out of argv. A `--set` shorthand for scalar properties can be added later without changing this contract.

### 5.5 `delete`

* One object per call. The target is resolved first. A missing target is `ErrNotFound`.
* The command asks `Delete <resource> <name or id>? [y/N]` on stderr and reads `cmd.InOrStdin()`. `--yes` skips the question. No input, or anything but `y` or `yes`, means no, and the command exits non-zero with `ErrNotConfirmed`.
* On success it prints `Deleted <resource> <id>` to stderr. Stdout stays empty.
* A server refusal, such as an object still referenced by another, is shown as received.

### 5.6 `--template`

* Available on `create`, `replace` and `patch`. It prints a skeleton payload with no server call and no login needed, like `login --template`.
* YAML has a comment per property. `-o json` prints JSON with no comments. This is the behavior in [`internal/input/spec.md`](../input/spec.md) §5. Templates are English data.
* The same skeleton serves all three verbs. For `patch` the help text says to keep only the properties to change.
* The template is a function of the resource definition and lives with it, not in `internal/input`.

---

## 6. Output flags

* `get` has its own `-o`. `dataFlags.addInput` adds `-d` and `--template` without `-o`, and `dataFlags.add` still adds all three for `login`. On `create`, `replace` and `patch`, `-o` is the format of both the template and the printed object.
* `table` is valid only for `get` lists. A single object with `-o table` is `ErrInvalidInput`.
* There is no `raw` mode. `-o json` is the payload as is.

---

## 7. Authentication, secrets, errors

A resource function receives a connection (the server URL, a token that is already valid, and an HTTP client that logs requests and applies the context's TLS settings) and never fetches a token itself.

### Authentication

* Every verb except `--template` gets a server URL and a bearer token from one helper, and builds the SDK client from them. There are two modes: saved context (below) and stateless (§7.1).
* **Saved context.** The helper resolves the context (`--context`, `IBM_AGENTID_CONTEXT`, `current-context`), gets a valid token through `oauth` (refresh or re-mint, as for `client_credentials`), and uses the context's `server` and TLS settings.
* No context, or a token that cannot be refreshed, is an error that tells the user to run `login`.
* HTTP calls go through the logging round tripper, so redaction applies.

### 7.1 Stateless mode (`IBM_AGENTID_TOKEN` / `IBM_AGENTID_SERVER`)

For CI and other places where there is no `~/.ibm-agentid`. It is described in [`internal/profile/spec.md`](../profile/spec.md) §5 as an optional escape hatch. This spec makes it part of the resource commands.

* **When.** Both variables are set and non-empty. An empty variable counts as unset, as everywhere else.
* **Only one set.** `ErrInvalidInput`, naming the missing variable. The CLI does not fall back to a saved context, because that could send a request to a different server than the user meant.
* **What it does.** The token is used as a bearer token as is, and `IBM_AGENTID_SERVER` is the base URL. `config.yaml` and `cache/` are not read, created or written. The state directory is not touched.
* **Precedence.** An explicit `--context` flag wins, so the saved-context mode is used and the variables are ignored. Otherwise the two variables win over `IBM_AGENTID_CONTEXT` and `current-context`. This keeps "flag over environment variable" and makes a CI job independent of whatever state is on the machine.
* **Server.** The same rule as the `login` payload: an `http` or `https` URL with a host and no user name or password. Anything else is `ErrInvalidInput`. A trailing slash is ignored.
* **Token.** Opaque. The CLI does not decode, validate or check its expiry. A value with whitespace or control characters is `ErrInvalidInput`, which also prevents header injection. The message never shows the value.
* **No refresh.** An expired or rejected token is the server's `401`, shown as received, followed by a stderr hint that `IBM_AGENTID_TOKEN` may be expired. The CLI does not re-mint, because it has no credentials.
* **TLS.** The system trust store is used. There is no `tls.ca_cert` equivalent. A user who needs a private CA uses a saved context. Adding a variable is out of scope.
* **Scope.** Resource commands only. `login`, `logout` and `context` do not use the variables.
* **Secrets.** The token is never printed, logged, put in an error, or written. Logging redaction applies to it as to any bearer token. The log records only that stateless mode was used and the server host. The token is visible to anything that can read the process environment, which the docs state.
* **Logging.** Unchanged. The default log file is still written, as [`internal/logging/spec.md`](../logging/spec.md) §4 already says. `IBM_AGENTID_LOG_LEVEL=off` or `IBM_AGENTID_LOG_OUTPUT=stderr` turns that off.

### Secrets

* A secret in a payload comes only from the `-d` file or stdin, never argv.
* Output masks the properties the definition lists as secret, using a fixed mask. If the server never returns them, the list is empty. The exception is the one-time value in §5.2, which is real data.
* No error, log or prompt contains a payload value. Decoder errors are replaced by a fixed message, as in the `input` package.
* The `@file` mechanism applies only to properties typed `Source` or `SecretSource`. Whether any resource secret should accept `@file` is left to each resource (§12). The default is inline in the file.

### Errors

| Error | When |
| :--- | :--- |
| `ErrInvalidInput` | (existing) Bad data, bad flag combination, a payload ID or name that differs from the target, an empty `patch`, `-o table` on one object. |
| `ErrNotFound` | The target does not exist. |
| `ErrAmbiguousName` | `--name` matches more than one object. |
| `ErrNotConfirmed` | `delete` was not confirmed. |

* Messages use `i18n` IDs in the `resource.*` namespace. Resource names in messages come from `resource.<name>` IDs and are passed as `{{.Resource}}`, so one message serves every resource.
* SDK and server errors are wrapped, not rewritten. The server's message is shown as received.
* `main` prints each error once and exits 1.

---

## 8. Logging

* `info`: verb and target (id or name) of each call. `debug`: name resolution and the number of matches, the list parameters.
* Never payload content, secret or not.

---

## 9. Tests

* Each verb is tested once against a fake definition: flag combinations, target resolution, data in and out, the prompt, and the stdout/stderr split.
* Each real resource gets a small test of its definition: every property has the same `json` and `yaml` name, the template decodes in both formats to the same object, read-only fields and masked secrets are dropped, and it offers the verbs it should. `internal/resource/foo/foo_test.go` is the model to copy.
* Tests use `isolate`, `run`, `runWithInput` and `seed`.

---

## 10. Adding a resource

`internal/resource/foo` is a working example, and it goes away when the first real resource exists. Read `foo.go` first: its comments explain each part of the definition. A resource is a package `internal/resource/<name>` with four files:

| File | For a real resource |
| :--- | :--- |
| `foo.go` | Copy it. `Resource()` holds the names and aliases, the template, the table columns, the ID and name accessors, `Strip` and `Mask`, and the list of calls. Leave a call out if the API has none, and the verb is not offered. |
| `model.go` | Replace it with the `oapi-codegen` output, with both tags (§2). |
| `server.go` | Replace it with a thin adapter over the generated client. Each function builds the request from the `Conn` it is given (server URL, token, HTTP client), calls the client, and returns the payload. A `401` becomes `resource.Unauthorized()` and a `404` becomes `resource.NotFound(singular, target)`. Any other server error is returned as received. |
| `foo_test.go` | Copy it. The tag and template tests need no change. The strip and mask test uses Foo's store, so point it at a fake client. |

Then:

1. Add the resource to `resources()` in `internal/cmd/target.go`. No verb code and no i18n strings change, because the help text is generic.
2. Add the resource to `docs/resources.md`.

Foo's `server.go` behaves like a server so that every path can be tried: it refuses an empty token, assigns the ID and creation time, keeps names unique, merges a patch into a nested object and replaces a list as a whole. Its data is in `foo.json` in the state directory.

---

## 11. Documentation changes

* `docs/get.md`, `docs/create.md`, `docs/replace.md`, `docs/patch.md`, `docs/delete.md`, and `docs/resources.md` for each resource and its properties.
* `docs/development.md`: the status table, the Layout and Adding a command sections, the "no registry" line (§1), and this spec in "Where the requirements live".
* `docs/environment-variables.md`: move `IBM_AGENTID_SERVER` and `IBM_AGENTID_TOKEN` from "Planned" to "Authentication", with the precedence in §7.1. The `docs` test then requires them to be used in code. A "Planned" section with nothing left is removed.
* `internal/profile/spec.md` §5: the escape hatch is no longer optional, and points here. `internal/oauth/spec.md` (where it says the variables stay planned) and `docs/development.md` (the "Not started" row) are updated to match.
* Pilot order: `agents` through all verbs first to settle the helpers, then `clients` and `server-connections`.

---

## 12. Open questions

Decided:

* **Patch** is a subset of properties, in the layout of the object, limited to what the API supports.
* **Names** are unique, and are sent to the server as a search filter.
* **Paging.** A list shows no total. That is acceptable.

Left to each resource, when its model is generated:

* The ID and name properties.
* The secret and read-only properties, and whether any secret should accept `@file`.
* Which list flags the API supports.
* What a `null`, a list and a nested object mean in a patch.

---

## 13. Out of scope

* `apply`, `edit`, `diff`, `--dry-run`, `--wait` and watching.
* Deleting several objects, or deleting by selector.
* A `create -d file` that works out the resource from the data.
* `--set key=value` and other argv value input.
* A CA certificate or client certificate variable for stateless mode.
* Refreshing or validating `IBM_AGENTID_TOKEN`.
* Using the variables in `login`, `logout` or `context`.
* Client-side filtering, sorting or column selection beyond the API's own.
