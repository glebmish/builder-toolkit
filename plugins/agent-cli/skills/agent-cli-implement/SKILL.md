---
name: implement-cli
description: Use when scaffolding or implementing an agent-first Go CLI from a completed design doc and an OpenAPI spec — produces the project layout, hand-written API client (Bearer/Basic), config cascade, embedded schema introspection with operation-mapping test, format/validate helpers, and the cobra command tree with offline-command skip and SilenceUsage. Don't use for non-Go ports.
---

# implement-cli

## Overview

Implements the design produced by `design-cli` as a Go CLI built on Cobra + plain `net/http` + `gopkg.in/yaml.v3`. Intentionally minimal: **no codegen, no HTTP frameworks, no struct-binding libs**. All operations are thin wrappers over three shared helpers (`doGet`, `doMutate`, `doDelete`) that delegate to a hand-written client.

**Code lives in `snippets/`**, not in this file. Each section below points to the file you should read and paste from. Swap `github.com/example/acme-cli` for your own module path when you copy.

## Prerequisites

- A `docs/design.md` from `design-cli` containing: surface shape, auth scheme + flag/env/config naming, tenant param, envelope decision, pagination scheme, exit-code policy, error-hint table, sanitization strategy, skill list.
- An OpenAPI spec file (`openapi.yaml` or `spec.json`) — see Step 0.
- Go ≥ 1.22, `yq` on PATH.

## When to Use

- Bootstrapping a fresh agent-first CLI in Go.
- The design phase is complete (see `design-cli`).

**Don't use for:** non-Go implementations; modifying an existing CLI of this shape (use it as a reference).

## Running example

Examples below use the same fictional CLI as `design-cli`:

| | Value |
|---|---|
| CLI short name | `acme` |
| Module path | `github.com/example/acme-cli` |
| Auth flag / env | `--access-token` / `ACME_ACCESS_TOKEN` |
| Tenant flag / env / placeholder | `--account-id` / `ACME_ACCOUNT_ID` / `{accountId}` |
| Resource groups | `users`, `projects`, `tasks` |

Substitute your own values throughout.

## Step 0 — Make sure an OpenAPI spec exists

The spec is the canonical schema. Without it there's no `schema` command, no operation-mapping test, no `--json` discoverability for agents.

If the API has a published spec, fetch it: `curl -o openapi.yaml <url>`.

If the API doesn't publish one, write one. Browser-DevTools your way through every operation you need, capture method + path + request shape + response shape, and produce a hand-written `openapi.yaml`. Yes, this is tedious; it's a one-time cost and pays for the operation-mapping test forever.

Don't proceed without a spec. The downstream steps assume `openapi.yaml` is present and valid.

## Build quirk — read this second

`go install .` names the binary after the **module directory**, not the CLI short name. So `github.com/example/acme-cli` produces `$GOBIN/acme-cli`, not `acme`. Alias the binary in the Makefile (see Step 14 / `snippets/Makefile`).

Document this in `CLAUDE.md` so future Claude doesn't lose ten minutes wondering why `acme` doesn't pick up changes.

## Project layout

```
acme/
├── go.mod                    # module github.com/example/acme-cli
├── main.go                   # entry; maps error type to exit code 0–5
├── Makefile                  # spec, build, test, install
├── README.md                 # human quickstart
├── CLAUDE.md                 # architecture + add-operation recipe + build quirk
├── docs/
│   └── design.md             # from design-cli
├── openapi.yaml              # source spec (Step 0)
└── internal/
    ├── api/
    │   ├── client.go         # HTTP client, Do/DoWithContext, path substitution, APIError, DryRun
    │   └── client_test.go
    ├── cliexit/
    │   └── cliexit.go        # AuthError, ValidationError, DiscoveryError typed errors
    ├── cmd/
    │   ├── root.go           # cobra root + persistent flags + config bootstrap
    │   ├── helpers.go        # doGet, doMutate, doDelete, doPaginate, mergeParams, etc.
    │   ├── schema.go         # `schema --list`, `schema <op>`, `schema <Type>`
    │   ├── schema_test.go    # spec parsing, listing, type rendering
    │   ├── integration_test.go  # operation-mapping test (spec ↔ map ↔ cobra)
    │   ├── config_cmd.go     # `config init`, `config path`, `config show --unmasked`
    │   ├── skills.go         # `skills list`, `skills get`, `skills install`
    │   ├── users.go          # one file per resource group
    │   ├── projects.go
    │   ├── tasks.go
    │   ├── skills/
    │   │   ├── acme-shared/SKILL.md
    │   │   ├── acme-users/SKILL.md
    │   │   ├── acme-projects/SKILL.md
    │   │   ├── acme-tasks/SKILL.md
    │   │   └── recipe-<workflow>/SKILL.md
    │   └── openapi-spec.json # //go:embed (built from openapi.yaml via yq)
    ├── config/
    │   ├── config.go         # YAML + env + flag cascade
    │   └── config_test.go
    ├── format/
    │   ├── output.go         # FormatFromFlag, Write, sanitize, filterFields
    │   └── output_test.go
    └── validate/
        ├── input.go          # PathParam, IntParam, DateParam, JSONBody (+ domain validators)
        └── input_test.go
```

**Universal**: `main.go`, everything in `api/`, `cliexit/`, `config/`, `format/`, `validate/`, `cmd/{root,helpers,schema,schema_test,integration_test,skills,config_cmd}.go`.

**Conditional on the design**: `DoMultipart` in client (only if API accepts uploads), `unwrapEnvelope` in format (only if design says envelope=yes), `doPaginate` (only if API paginates), API-specific helpers (`doDownload`, `doUpload`, `doPostDelete` — see §8), per-API quirk validators.

## Step-by-step

### 1. Module & deps

```bash
go mod init github.com/example/acme-cli
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3@latest
```

`go.mod` should have **2 direct entries** (cobra, yaml.v3); pflag and mousetrap come in indirectly via cobra.

### 2. main.go and structured exit codes

→ `snippets/main.go`, `snippets/internal/cliexit/cliexit.go`.

`main.go` does **two** things: run cobra, and map the returned error type to an exit code. It does **not** print the error — cobra already does that (`SilenceUsage: true`, `SilenceErrors: false`, see §7). Printing in `main.go` on top of cobra's print means every error appears twice, doubling the agent-context cost.

Three typed error wrappers in `internal/cliexit/`:

| Type | Maps to |
|---|---|
| `*AuthError` | exit 2 (no token / wrong scope) |
| `*ValidationError` | exit 3 (input rejected pre-request) |
| `*DiscoveryError` | exit 4 (spec/schema problem) |

`api.APIError` already exists in `internal/api/`. Its `IsAuth()` (true on 401/403) is also exit 2; otherwise exit 1.

**Frame-of-reference: typed at source.** Callers don't remember to wrap. Validators (§6) return typed errors at the source so every callsite propagates the right exit code automatically. `cfg.Validate()` returns `*cliexit.AuthError`. `schema.go` parse failures return `*cliexit.DiscoveryError`. The HTTP client's 401/403 are detected via `APIError.IsAuth()`. The downside (validate package importing cliexit) is a single, no-cycle, no-allocation dependency — pay it once.

### 3. internal/api/client.go

→ `snippets/internal/api/client.go`.

Responsibilities:
- Constructor: `NewClient(baseURL, token, tenantID, tenantPlaceholder string) *Client`.
- `Do` and `DoWithContext` — `Do` delegates to `DoWithContext(context.Background(), ...)`.
- Path substitution: replace **one** disambiguated tenant placeholder (e.g. `{accountId}`) from `c.tenantID`. Replace remaining `{key}` from `params`. Leftover params become the query string.
- Inject auth header per design (Bearer or Basic).
- 30–60 second timeout, no retries.
- `DryRun(method, path, params, body) string` — formatted preview.
- `WithContext(ctx, *Client)` / `FromContext(ctx) *Client` for cobra context plumbing.
- `APIError` whose `Error()` returns `"API error <code> <text>: <method> <path>: <body>"` plus the per-status hint. `IsAuth()` returns true on 401/403.
- `DoMultipart` if the API accepts uploads (see `design-cli` §14).

Auth variants:

```go
// Bearer (in the snippet):
req.Header.Set("Authorization", "Bearer "+c.token)

// Basic — replace with:
req.SetBasicAuth("API_KEY", c.token)
```

**Path templates always use disambiguated placeholders.** The command code passes `/v1/accounts/{accountId}/projects/{projectId}` — the client substitutes `{accountId}` (the configured tenant placeholder) and `{projectId}` (from params).

### 4. internal/config/config.go

→ `snippets/internal/config/config.go`.

Cascade: **defaults → file → env → flags**, each stage only overriding non-empty values. Done in `root.go`'s `PersistentPreRunE` (after the offline-command skip):

```go
cfg, err := config.Load(config.DefaultPath())  // file (or default if missing)
cfg.ApplyEnv()                                  // env vars
cfg.ApplyFlags(token, baseURL, accountID)       // CLI flags (highest)
if err := cfg.Validate(); err != nil { ... }    // missing token → cliexit.AuthError
```

`Validate()` returns `*cliexit.AuthError` so the missing-token case exits with code 2 from `main.go`. The error message must be **actionable**: name the env var, name the config init command, ideally explain how to obtain the credential.

The default `BaseURL` is hard-coded in `Load` so a fresh install with no config and no env var still hits the right host.

### 5. internal/format/output.go

→ `snippets/internal/format/output.go`.

`Write(w, data, opts)` pipeline:

1. `json.Unmarshal` — non-JSON bodies (plain strings, integer timestamps) pass through as-is.
2. (Conditional) `unwrapEnvelope(v, "data", recursive)` — only included when `docs/design.md` says `Envelope: yes`.
3. `sanitize(v)` — strip control chars + `<system>`/`<assistant>`/`<tool_use>`/`<tool_result>` tag wrappers from string fields. Always on.
4. `filterFields(v, opts.Fields)` — dotted-path mask, descends into arrays implicitly.
5. Encode as pretty JSON or NDJSON per `--format`.

`sanitize` and `filterFields` are fully generic — paste verbatim from the snippet, no per-API tweaks. `filterFields` builds a trie so a single walk handles arbitrary depth (`rows.id` filters `{"rows":[{"id":1,"x":2}]}` → `{"rows":[{"id":1}]}`).

Other helpers in the same file:
- `WriteRaw(w, data)` — bypass parsing for cases where the caller already produced bytes (e.g. NDJSON pages from `doPaginate`).
- `DryRunOutput(w, preview)` — used by every helper when `--dry-run` is set.

### 6. internal/validate/input.go

→ `snippets/internal/validate/input.go`.

Validators return `*cliexit.ValidationError` directly so every callsite propagates the right exit code automatically — callers don't need to remember to wrap. A tiny `vErr` helper keeps call sites readable:

```go
func vErr(format string, args ...any) error {
    return &cliexit.ValidationError{Err: fmt.Errorf(format, args...)}
}
```

Built-in validators:
- `PathParam(name, value)` — non-empty, no `..`, no `?`/`#`/`&`, no `%`, no control chars.
- `IntParam(name, value)` — non-empty, parses as Go int (allows negatives).
- `DateParam(name, value)` — strict `YYYY-MM-DD`.
- `JSONBody(body)` — non-empty, no rogue control chars, parses as JSON.

Apply in `RunE` **before** any HTTP call.

**Domain validators** — server-quirk catchers that reject inputs the API itself won't accept (e.g. "this asset type is required to be `STOCK` for `SHARE_IN` ops"). One function per quirk; same `vErr` wrapper. Document the why in a function comment so future readers don't undo the check.

Why typed-at-source: with N callsites sprinkled across resource files, "remember to wrap with `validationErr(err)` at each callsite" is a rule the future agent will forget. Returning the typed error from the validator is once-and-done.

### 7. internal/cmd/root.go

→ `snippets/internal/cmd/root.go`.

Two cobra settings that matter:

- `SilenceUsage: true` — don't dump cobra's `--help` block on every error. Without this, every failure costs the agent ~30–80 lines of usage output.
- `SilenceErrors: false` (the default; set explicitly for clarity) — let cobra print the error itself. `main.go` does NOT print, only maps the error type to an exit code. Setting `SilenceErrors: true` *and* having `main.go` print would work too, but the cobra-prints version keeps `main.go` tiny.

`PersistentPreRunE` skips offline subcommands first — they must run without a token: `schema`, `skills`, `config`, `help`, `init`. The `skills` entry covers `skills list`, `skills get`, and `skills install` because `isOffline` walks the cobra parent chain. Then load → ApplyEnv → ApplyFlags → Validate → stash client on context.

Persistent flags (standard set per `design-cli` §3): `--format`, `--fields`, `--dry-run`, `--yes`, `--access-token`, `--base-url`, `--account-id` (if tenant), `--json`, `--params`, plus pagination flags (only if API paginates): `--page-all`, `--page-limit`, `--page-delay`, `--page-size`.

`--json` and `--params` are root persistent — not per-command. Per-command `--json` declarations conflict with the inherited persistent flag and lose the canonical-payload framing.

### 8. internal/cmd/helpers.go — workhorses + accessors

→ `snippets/internal/cmd/helpers.go`.

Every resource-file command goes through one of these:

| Helper | Wraps |
|---|---|
| `doGet(cmd, path, params)` | JSON GET; falls through to `doPaginate` when `--page-all` is set |
| `doMutate(cmd, method, path, params, jsonBody)` | POST / PUT / PATCH with optional JSON body |
| `doDelete(cmd, path, params, resource, id)` | HTTP DELETE with `--yes` confirmation |

Each one:
1. Calls `mergeParams` to overlay `--params` (caller-set entries win — see below).
2. Checks `--dry-run` *before* any HTTP call.
3. Routes the response through `format.Write` (sanitization + field mask).

Plus accessors:
- `fmtOpts(cmd)` — reads `--format` and `--fields` into `format.Options`.
- `requireString(cmd, name)` — non-empty flag or `*cliexit.ValidationError`.
- `requireJSON(cmd)` — `requireString("json")` + `validate.JSONBody`.
- `confirmDelete(cmd, resource, id)` — `--yes` short-circuit, isatty check, prompt.

#### mergeParams: caller wins on collision

Order matters. `--params` is overlaid first, then `base` is written on top so caller-set keys (validated path params, etc.) win on collision. `--params` is strictly additive — it can fill query-param gaps the command didn't expose, but cannot silently override path or required params the command built itself.

#### doPaginate (only if API paginates)

Snippet ships an offset/limit version + a generic `pageItemCount` stop helper that recognises `{content|rows|items|data: [...]}` envelopes and bare top-level arrays.

For page-number or cursor schemes, swap the per-scheme line at the marked spot in `doPaginate` and adjust the stop condition:

| Scheme | Per-iteration | Stop condition |
|---|---|---|
| **OFFSET-LIMIT** (snippet default) | `walking["offset"] = strconv.Itoa(page * pageSize)` | `count < pageSize` |
| **PAGE-NUMBER** | `walking["page"] = strconv.Itoa(page + 1)` | `count < pageSize` or `has_more=false` from response |
| **CURSOR** | `walking["cursor"] = nextCursor` | `next_cursor` empty in response |

#### API-specific helpers (extend, don't inline)

The four workhorses (`doGet`, `doMutate`, `doDelete`, `doPaginate`) cover JSON GET, JSON write, HTTP DELETE, and offset/limit pagination. Real APIs grow shapes those don't fit. When that happens, **add a helper next to the workhorses; don't inline the pattern in resource files.** The same `--dry-run` check and `format.Write` discipline must apply.

| Helper | When to add | Notes |
|---|---|---|
| `doDownload(cmd, path, params, outPath)` | API serves binary (charts, exports, attachments) | Streams `resp.Body` to `os.Stdout` or `--out <path>`. **Skip** `format.Write` (which JSON-parses). |
| `doUpload(cmd, path, params, fieldName, paths)` | API accepts `multipart/form-data` (file imports, avatars) | Builds multipart, calls `c.DoMultipart`. Sandbox file paths to CWD per `design-cli` §14. |
| `doPostDelete(cmd, path, jsonBody, resource, id)` | API uses POST-with-body for delete (e.g. `POST /trades/delete`) | `confirmDelete` + `doMutate("POST", …)`. |

These are *expected*, not deviations. Each one earns its place by absorbing a pattern that would otherwise repeat across resource files. Don't pre-add them — wait for the second occurrence, then factor.

### 9. internal/cmd/<resource>.go — the operation pattern

One file per resource group: `internal/cmd/users.go`, `internal/cmd/projects.go`, `internal/cmd/tasks.go`. One `init()` per file registers everything.

Each operation is a `newResourceActionCmd() *cobra.Command`:

```go
func newProjectsListCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "list",
        Short: "List projects (GET /v1/accounts/{accountId}/projects)",
        RunE: func(cmd *cobra.Command, args []string) error {
            params := map[string]string{}
            if v, _ := cmd.Flags().GetString("since"); v != "" {
                if err := validate.DateParam("since", v); err != nil { return err }
                params["since"] = v
            }
            return doGet(cmd, "/v1/accounts/{accountId}/projects", params)
        },
    }
    cmd.Flags().String("since", "", "YYYY-MM-DD")
    return cmd
}

func newProjectsCreateCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "create",
        Short: "Create a project (POST /v1/accounts/{accountId}/projects)",
        RunE: func(cmd *cobra.Command, args []string) error {
            body, err := requireJSON(cmd)
            if err != nil { return err }
            return doMutate(cmd, "POST", "/v1/accounts/{accountId}/projects", nil, body)
        },
    }
    return cmd
}

func init() {
    parent := &cobra.Command{Use: "projects", Short: "Project ops"}
    parent.AddCommand(newProjectsListCmd(), newProjectsCreateCmd())
    rootCmd.AddCommand(parent)
}
```

Rules:
- One file per resource group; one `init()` registers everything.
- Each operation is `newResourceActionCmd() *cobra.Command`.
- Required flags validated **manually** in `RunE` via `requireString` — no `MarkFlagRequired` (its error messages are bad and it short-circuits before `--dry-run` can preview).
- Path templates use **disambiguated placeholders**; the `{accountId}` substitution is the client's job, every other placeholder comes from `params`.
- `--json` is **not** declared per-command — it's inherited from root.
- Every command path goes through `doGet` / `doMutate` / `doDelete` (or an API-specific helper from §8).

### 10. internal/cmd/schema.go — runtime introspection

→ `snippets/internal/cmd/schema.go`.

The OpenAPI spec is embedded at build time via `//go:embed openapi-spec.json`. The `operationIDToCommand` map is the single source of truth for spec ↔ CLI names; the bijection test (§11) enforces no orphans in either direction.

```go
//go:embed openapi-spec.json
var specData []byte

var operationIDToCommand = map[string]string{
    "listProjects":  "projects.list",
    "createProject": "projects.create",
    // ... one entry per operation
}
```

Three subcommands:
- `acme schema --list` — print the table (sorted by CLI name).
- `acme schema <op>` — print one operation's request/response shape.
- `acme schema <Type>` — print a type definition; `--resolve-refs` to inline `$ref`s.

Errors that indicate spec/schema problems return `*cliexit.DiscoveryError` so they exit with code 4.

### 11. The operation-mapping test

→ `snippets/internal/cmd/integration_test.go`.

A "bijection" check: every spec op ↔ every CLI command, no orphans either direction. Catches API drift the moment you regenerate the spec.

**File location: either `schema_test.go` or `integration_test.go`** — both are reasonable. The test traverses the embedded spec and the whole cobra tree, so calling it integration-shaped is honest; calling it schema-shaped is also fine. Pick one and don't split the bijection itself across files. If you also have small unit tests for spec parsing or `--list` rendering, those go in `schema_test.go` regardless.

```go
// Direction 1: every operationId in the spec must be mapped.
func TestAllOperationIdsMapped(t *testing.T) { ... }

// Direction 2: every mapped CLI name must resolve to a real cobra command.
func TestEveryMappedCLINameExists(t *testing.T) { ... }
```

### 12. internal/cmd/skills.go

`embed.FS` of the `skills/` directory exposed as a `skills` cobra group with three subcommands. All three are **offline** — they appear in the `isOffline` switch (§7) and require no token.

**`acme skills list`** — walks the embedded FS, reads each `<name>/SKILL.md` frontmatter, prints one entry per skill.

- Default (text): `<name>  <description>` per line (two-column).
- `--format json`: array of objects, **full frontmatter passed through** as `map[string]any`. Don't enumerate known keys — emit whatever the SKILL.md declares so the contract stays stable as skill authors add fields.

**`acme skills get <name>`** — fetches one skill from the embedded FS.

- Default (raw): print **only the SKILL.md body**, with the frontmatter block (`---\n…\n---\n`) stripped. The agent has already seen name + description via `skills list`.
- `--format json`: envelope `{"name": ..., "description": ..., "files": [{"path": "SKILL.md", "content": "<full file>"}, ...]}`. File contents keep their frontmatter — this view is a faithful file dump that also covers multi-file skills (`snippets/`, `references/`).

**`acme skills install [--output-dir P]`** — copies the embedded FS to disk:

1. Prompt 1: scope — project (`./.claude/skills`) or user (`~/.claude/skills`).
2. Prompt 2: target dir — `.claude/skills` or `.agents/skills`.
3. `--output-dir <path>` skips both prompts.

Files written `0644`; parent dirs `0755`.

**Errors:**

| Condition | Type | Exit |
|---|---|---|
| Unknown skill name on `get` | `*cliexit.ValidationError` | 3 |
| Embedded FS missing or unreadable | `*cliexit.DiscoveryError` | 4 |
| SKILL.md frontmatter parse failure | `*cliexit.DiscoveryError` | 4 |

Validation-error hint must name `acme skills list` so the agent self-recovers.

**Frontmatter parsing:** minimal hand-rolled splitter on the `---\n…\n---\n` block plus `yaml.Unmarshal` into `map[string]any`. The CLI already depends on `gopkg.in/yaml.v3` (config).

### 13. internal/cmd/config_cmd.go

`acme config` group with three subcommands:

- `acme config init` — interactive prompt; preserves existing fields if file exists; writes via `config.Save` with `0600`.
- `acme config path` — print the resolved config path.
- `acme config show [--unmasked]` — print fields, mask token unless `--unmasked` (for headless export — see `design-cli` §15).

`config init` must work with non-interactive stdin: detect `!isatty(os.Stdin)` and either fail with an actionable error pointing at the env var, or accept piped input. **Never** require a browser flow.

### 14. Tests

| Layer | Style |
|-------|-------|
| `api/client_test.go` | `httptest.NewServer`; assert method, URL, auth header, query string, body. |
| `config/config_test.go` | `t.TempDir()`; cascade priority cases (defaults → file → env → flags). |
| `format/output_test.go` | Field filter, NDJSON, raw passthrough; sanitize cases (control chars, injection tags); envelope unwrap if applicable. |
| `validate/input_test.go` | One case per rejection rule. |
| `cmd/schema_test.go` or `cmd/integration_test.go` | The two-direction operation-mapping test in §11. |
| `cmd/integration_test.go` | End-to-end: `t.Setenv("ACME_CONFIG", t.TempDir()+"/config.yaml")`, run cobra commands against `httptest.NewServer`. |

The bijection test is the cheapest insurance against silent API drift — keep it green.

### 15. Makefile

→ `snippets/Makefile`.

Targets: `make spec` (yq → embedded JSON), `make test`, `make build`, `make install` (with the `cp $(MOD) $(NAME)` alias for the build quirk).

`yq` is a hard dependency — the spec build target checks for it and fails with an install hint.

### 16. Docs

- **`README.md`** — install + `acme config init` + 3 quickstart examples. Under 100 lines.
- **`CLAUDE.md`** — four sections:
  1. Architecture (one paragraph: Cobra + helpers + client + config cascade + embedded spec).
  2. Build commands (`make build`, `make test`, `make install`, **and the build quirk**).
  3. Adding a new operation (recipe below).
  4. Conventions (manual validation, context-stored client, no `MarkFlagRequired`, helpers always, offline skip, `SilenceUsage`, disambiguated placeholders).

**Agent-facing docs go in `internal/cmd/skills/acme-shared/SKILL.md`**, not in a top-level `CONTEXT.md`. The shared skill is the agent front door — surface, global flags, schema discovery, exit codes, error hints, copy-pasteable patterns.

## The "add a new operation" recipe

Put this verbatim in `CLAUDE.md`:

1. Identify the resource group → `internal/cmd/<resource>.go`.
2. Add `func newResourceActionCmd() *cobra.Command { ... }` following §9.
3. Use disambiguated placeholders in the path template.
4. Register in the file's `init()` via `parent.AddCommand(...)`.
5. Add an entry to `operationIDToCommand` in `schema.go`.
6. Update the `acme-<resource>` skill if the operation has non-obvious quirks.
7. `make build && make test`.

## Quick reference

| Component | Lives in | Key fact |
|-----------|----------|----------|
| Entry point | `main.go` | Lean — cobra prints, main only maps error type to exit code 0–5 |
| Typed errors | `internal/cliexit/cliexit.go` | `AuthError`/`ValidationError`/`DiscoveryError` |
| HTTP client | `internal/api/client.go` | Hand-written; Bearer or Basic; one tenant placeholder; `IsAuth()` for 401/403 |
| File uploads | `internal/api/client.go` | `DoMultipart` (only if API accepts multipart) |
| Pagination | `internal/cmd/helpers.go doPaginate` | Per-scheme loop body; emits NDJSON; GETs only |
| Config cascade | `internal/config/config.go` | defaults → file → env → flags; `Validate` returns `*cliexit.AuthError` |
| Output formatter | `internal/format/output.go` | `Write` does sanitize + envelope-unwrap (opt-in) + filterFields + JSON/NDJSON |
| Input validators | `internal/validate/input.go` | Return `*cliexit.ValidationError` directly |
| Cobra root | `internal/cmd/root.go` | `SilenceUsage:true`; offline skip in `PersistentPreRunE` |
| Workhorse helpers | `internal/cmd/helpers.go` | `doGet`, `doMutate`, `doDelete`, `doPaginate` + accessors + API-specific extensions |
| Resource commands | `internal/cmd/<resource>.go` | One file per group; `init()` registers all; disambiguated placeholders |
| Schema introspection | `internal/cmd/schema.go` | `//go:embed openapi-spec.json` + `operationIDToCommand` map |
| Operation-mapping test | `internal/cmd/{schema,integration}_test.go` | Two directions: spec→map, map→cobra |
| Skills | `internal/cmd/skills/` + `skills.go` | `embed.FS`; `skills list`/`get`/`install` subcommands; `acme-shared` is the agent front door, with `skills get` as a runtime alternative |
| Config commands | `internal/cmd/config_cmd.go` | `init`; `path`; `show --unmasked` for headless export |
| Build | `Makefile` | `spec` → `build` → `install` (with binary alias) |

## Common mistakes

- **Adding an HTTP library** (resty, go-resty, anything fancy). `net/http` is enough.
- **Generating Go structs from the spec.** Don't. Agents pass `--json`.
- **Polymorphic placeholders in path templates** (same `{id}` meaning different things). Disambiguate.
- **Skipping the operation-mapping test.** Cheapest insurance against silent API drift.
- **Inlining `confirmDelete` in every delete command.** Centralize in `helpers.go`.
- **Forgetting the `cp <mod> <name>` build quirk.** Users run an old binary, blame the CLI.
- **Mixing logger output with response output on stdout.** Stderr for diagnostics, stdout for response.
- **Using `MarkFlagRequired`.** Bad error messages, short-circuits before `--dry-run` can preview.
- **Forgetting the offline-command skip in `PersistentPreRunE`.** `schema`, `skills`, `config init`, `help` must run without a token.
- **Forgetting `SilenceUsage: true` on the root.** Cobra's usage dump on every error blows the agent's context window.
- **Printing the error in both `main.go` and cobra.** With `SilenceErrors: false` (default), cobra already prints. If `main.go` also does `fmt.Fprintf(os.Stderr, "%v\n", err)`, every error appears twice. `main.go` should only map the type to an exit code (see §2).
- **Wrapping `validate.*` errors at every callsite.** Validators return `*cliexit.ValidationError` directly (§6); the wrap-at-callsite pattern is a rule the future agent will forget.
- **`config.Load` returning empty `Config` without seeding `BaseURL`.** First request hits an empty host.
- **Returning `error` from `format.Write` on non-JSON responses.** Fall back to byte passthrough.
- **Letting `doMutate` reject empty bodies.** Action endpoints (POST `/thing/apply`) legitimately have no body.
- **Skipping `--dry-run` in helpers.** Every helper must check the flag *before* the HTTP call.
- **Shipping `CONTEXT.md` alongside an `acme-shared` skill.** Pick one (the skill).
- **Inlining binary downloads, multipart uploads, or POST-with-body deletes.** Add a `doDownload` / `doUpload` / `doPostDelete` helper next to the workhorses (see §8).
