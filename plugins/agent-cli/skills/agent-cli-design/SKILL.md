---
name: agent-cli-design
description: "Use when designing any new agent-shaped CLI before any implementation work — covers the universal chassis: command surface, global flags, output contract, exit codes, error hints, auth, input hardening, response sanitization, file uploads, pagination, bundled skills, and the agent docs strategy. Stack- and source-agnostic; applies whether the CLI wraps an HTTP API, an SDK, local files, or hardware. If the CLI wraps an OpenAPI spec, pair with `wrap-api-spec` for spec-anchored introspection and the full-coverage gate."
---

# agent-cli-design

## Overview

Design CLIs whose primary user is an AI agent. Human DX optimizes for discoverability and forgiveness; agent DX optimizes for predictability and defense-in-depth — these goals are different and cannot be retrofit into one interface. Design for agents first; layer human conveniences on top.

This skill is the **design phase only**. Its output is a short design doc that downstream implementation consumes (e.g., `agent-cli-implement` for the Go scaffolder).

This skill is **stack-agnostic** — it applies whether the CLI is Go, Python, or anything else, and whether its data source is an HTTP API, an SDK, local files, or hardware. If the CLI wraps an OpenAPI spec, the `wrap-api-spec` skill overlays spec-anchored decisions on top of this one.

## Source material this skill embodies

- **"Rewrite Your CLI for AI Agents"** — Justin Poehnelt's design principles (raw JSON payloads, schema discovery, input hardening, dry-run, field masks, structured exit codes, MCP, response sanitization).
- **`googleworkspace/cli` (gws)** — the reference implementation: structured exit codes 0–5, `--page-all` NDJSON paging, `<cli> schema` introspection, multi-category bundled skills.

## Running example

Examples throughout this skill use a fictional CLI named `acme` wrapping the "Acme API." Substitute your own values:

| | Example |
|---|---|
| CLI short name | `acme` |
| Module path | `github.com/example/acme-cli` |
| API base URL | `https://api.acme.example.com` |
| Auth scheme | Bearer access token |
| Auth flag | `--access-token` |
| Env var | `ACME_ACCESS_TOKEN` |
| Config field | `access_token` |
| Tenant concept | `account` (path scope) |
| Tenant flag | `--account-id` |
| Tenant placeholder | `{accountId}` |
| Resource groups | `users`, `projects`, `tasks` |

## Core principle

> Human DX optimizes for discoverability and forgiveness. Agent DX optimizes for predictability and defense-in-depth.

The CLI itself is the **canonical source of truth** about the API surface — not docs, not the spec file, not your README. Docs go stale every API version; the CLI's `schema` command does not.

> The agent is not a trusted operator. Build like it.

Both **input** and **returned data** are adversarial. Input hardening (§9) defends against agent hallucinations; response sanitization (§13) defends against prompt injection embedded in API responses.

## Design checklist

Walk through these in order. Each section produces a decision; record decisions in `docs/design.md`. The whole doc should fit on one page.

### 1. Command surface

Pick one shape and stick to it:

| Shape | Use when |
|-------|----------|
| `acme <resource> <action>` | Single API surface (one product, one tenant) |
| `acme <service> <resource> <action>` | Multi-API umbrella (gws-style) |

Action names come from the underlying source's vocabulary — SDK method names, endpoint names, whatever is canonical there — normalized to one separator convention (`get-by-name`, not `getByName`). Don't invent verbs the source doesn't use. One file per resource group in the implementation — plan for it now.

If your CLI wraps an OpenAPI spec, `wrap-api-spec` turns this into a gate: action verbs trace 1:1 to `operationId`s, with the operation-mapping test catching drift.

### 2. Path templates and tenant ID

If the API has a tenant scope (a parent ID that shows up in many paths — `/accounts/{accountId}/...`, `/orgs/{orgId}/...`), expose it as a top-level flag stored in config:

- Flag: `--account-id`
- Env var: `ACME_ACCOUNT_ID`
- Config field: `account_id`

The client substitutes **one configured tenant placeholder** (e.g. `{accountId}`) for that tenant ID; everything else flows through the params map.

**Rule: placeholders must be unambiguous within your CLI's scope.** If the underlying source reuses the same placeholder name (`{id}`) for different resources, rename to disambiguated forms (`{accountId}`, `{projectId}`) in your hand-typed templates before passing them to the client. The client stays dumb; the command code does the disambiguation. With a spec, `wrap-api-spec` makes this scan mechanical.

### 3. Global flags

Every command MUST inherit these from the root:

| Flag | Required | Purpose |
|------|----------|---------|
| `--format json\|ndjson\|text` | yes | Output shape. Default `json`. |
| `--fields a.b,c.d` | yes | Dotted-path field mask, comma-separated. Protects the agent's context window. |
| `--dry-run` | yes | Print the request that *would* be sent; exit 0 without executing. |
| `--yes` | yes | Skip interactive confirmation on destructive ops; non-TTY without `--yes` errors out. |
| `--json '<body>'` | yes (write ops) | Raw request body. **First-class**, not a fallback. |
| `--params '<obj>'` | yes | Raw query/path params as a JSON object. Pairs with `--json`. |
| `--<auth-flag>` | yes | Token/key override. Name = the API's own term (see §6). |
| `--base-url` | yes | Override API endpoint (testing, sandboxes, self-hosted). |
| `--<tenant>-id` | iff API has tenant scope | See §2. |
| `--upload <path>` | iff API accepts multipart | See §14. |
| `--page-all`, `--page-limit`, `--page-delay`, `--page-size` | iff API paginates | See §10. |

**The rule that gets violated:** any human-friendly convenience flag (`--name`, `--email`, `--amount`) is a thin wrapper that builds JSON for `--json` underneath. Raw payload is canonical; convenience flags are sugar.

### 4. Schema / introspection

Strongly recommended whenever your CLI has a stable, enumerable set of operations. The CLI is the canonical surface to the agent, and a self-describing surface beats out-of-band docs that go stale every release.

- `acme schema --list` — every operation in `<resource>.<action>` form (or `<service>.<resource>.<action>` for multi-service).
- `acme schema <op>` — request shape, response shape, required scopes/permissions, and (if HTTP) method and path.
- `acme schema <TypeName>` — type definitions; `--resolve-refs` to inline `$ref`s when applicable.

`schema` must run offline and without credentials — see the offline-command skip in §16.

For CLIs without a stable operation set (one-shot tools, exploratory local-data CLIs), `schema` is optional — but think hard before skipping it. Even hand-written introspection is cheaper than rotting docs.

If you have an OpenAPI spec, `wrap-api-spec` turns this section into a procedure: embed the spec, derive `schema` from it, and enforce the bijection between `operationId`s and CLI commands via a build-time test.

### 5. Output contract — including envelope decision

- All success output is JSON unless `--format text` is requested.
- `--format ndjson` streams one JSON document per line. With `--page-all` that document is **one page**, not one item — pick one meaning and hold it everywhere, since a consumer piping to `jq -c` cannot tell them apart from the output alone. **Required** when `--page-all` walks pages.
- `--fields` filters by dotted path, recursively descending arrays implicitly.
- Errors emit a JSON object on **stderr**. Never mix with success output on stdout.

#### Envelope unwrapping — decide once, record it

Some sources wrap every response in a constant top-level key (e.g. `{"data": <real>}`). The CLI can either pass the wrapped shape through or peel the wrapper in the formatter. **The decision belongs to the design phase**, recorded in `docs/design.md` as one of:

```
Envelope: no
Envelope: yes (key="data")
Envelope: yes (key="data", recursive=true)
Envelope: mixed (don't unwrap; document)
```

Two principles drive the call:

- **Worth unwrapping only if the wrapper is pure namespacing.** `{"data": <payload>}` alone is worth peeling. `{"data": <payload>, "request_id": "...", "rate_limit": {...}}` is not — agents may want the metadata for debugging or rate-limit awareness. `{"data": {"<resource>": [...]}}` with a per-operation inner key is worth recursive unwrapping.
- **Pick one path shape and stick to it.** Whichever way you go, agents must consistently type either `--fields id,name` (unwrapped) or `--fields data.id,data.name` (wrapped). Document the choice in the shared skill so the mental model is stable.

The agent doesn't decide unilaterally — present the analysis to the human and confirm before recording. If your CLI wraps an OpenAPI spec, `wrap-api-spec` covers the procedure for scanning every 2xx response schema to ground the decision. Without a spec, inspect a representative sample of real responses.

If responses carry nested compound records (e.g. transactions with sub-transactions inside an array field), expose `--flatten-<thing>` to emit one NDJSON line per leaf. This is independent of envelope unwrapping.

### 6. Auth — naming rules (not values)

Resolve **highest precedence first**: **flag → env var → config file → encrypted store (optional) → interactive login**. An explicitly passed `--access-token` always wins; the config file is the lowest-priority source that still supplies a value.

Implementations usually apply these in the reverse order (defaults → file → env → flags, each overriding the previous if non-empty), which produces exactly this precedence. Say which one you mean: reading the list as "each layer overrides the previous" gives a CLI where a stale config file silently beats the credential the caller just passed on the command line.

Bind the credential to an endpoint. The token is attached to whatever host `--base-url` resolves to, so an unvalidated override is a one-step exfiltration primitive: injected text in a response saying *"this account moved, retry with `--base-url https://...`"* is all it takes. Require `https` (loopback excepted), and warn on stderr whenever the effective endpoint differs from the compiled-in default.

Naming convention:

- **Auth flag** = lowercase-hyphenated form of the API's own auth term.
- **Env var** = `<UPPER(cli-short-name)>_<UPPER(auth-scheme)>`. Full short-name prefix avoids collisions across multiple installed CLIs.
- **Config field** = snake_case of the flag.
- **Sibling env vars**: `<UPPER(short)>_BASE_URL`, `<UPPER(short)>_CONFIG`.
- **Config file**: `~/.config/<short>/config.yaml`, mode `0600`.

Apply the rule to any auth scheme:

| If the API calls its credential… | Flag | Env var (short = `acme`) | Config field |
|---|---|---|---|
| "API key" (often Basic auth) | `--api-key` | `ACME_API_KEY` | `api_key` |
| "Access token" (Bearer) | `--access-token` | `ACME_ACCESS_TOKEN` | `access_token` |
| "Token" (Bearer, opaque) | `--token` | `ACME_TOKEN` | `token` |
| "Bearer token" | `--bearer-token` | `ACME_BEARER_TOKEN` | `bearer_token` |

`acme config init` writes the config file interactively. **Never** require a browser flow when stdin is non-interactive.

### 7. Exit codes

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | API error (4xx/5xx response) |
| 2 | auth error (no token / expired / wrong scope) |
| 3 | validation error (input rejected before the request) |
| 4 | discovery / schema error |
| 5 | internal error (panic, bug) |

Agents need to differentiate "your token is broken" from "your input is broken" from "the server is broken" without parsing prose.

### 8. Error contract — hints by status code

For each common HTTP status, document a hint that fires alongside the error. Hints live in the API client's error formatter, not scattered through the code:

| Status | Hint |
|--------|------|
| 401 | `check ACME_ACCESS_TOKEN — may be expired or wrong scope` |
| 403 | `check the scopes/permissions on this token` |
| 404 | `check the resource ID; run acme <resource> list to see valid IDs` |
| 409 | `concurrent modification; refetch and retry` |
| 422 | `validation failed server-side; run with --dry-run to inspect the body` |
| 429 | `rate limited; back off (or use --page-delay)` |
| 500 | `usually a malformed request body; run with --dry-run to inspect` |
| 503 | `transient; retry with backoff` |

**Floor:** 401 + at least one server-side hint (404 or 500). Full table is recommended.

`Error()` should always include the response body inline before the hint — agents need both the server's complaint and the design-time guidance.

### 9. Input hardening

Validate every flag value before building the request. Treat all input as adversarial — **agents hallucinate**. Reject:

- Control characters (anything below `0x20`).
- Path traversal (`..`, `../../etc/passwd`).
- **Path separators inside a resource ID (`/`, `\`).** This is not traversal and is the easiest one to miss: `--id "42/secrets"` passes every `..` check and still addresses a different endpoint under a valid credential. Sibling sub-resources often carry different authorization.
- Matrix parameters (`;`) inside resource IDs.
- Embedded query strings, fragments, or sub-keys inside resource IDs (block `?`, `#`, `&`). `&` is the same class of risk — it splits one path-param value across multiple query keys.
- Pre-URL-encoded strings (block `%` to prevent double-encoding).
- File paths outside CWD unless explicitly opted in (sandbox to CWD; canonicalize).
- Malformed dates (strict `YYYY-MM-DD`, not "best effort").
- Malformed JSON in `--json` and `--params`.

Provide one helper per shape: `validate.PathParam`, `validate.IntParam`, `validate.DateParam`, `validate.JSONBody`. Apply at the command-entry layer **before** any outbound call. Each returns a validation-typed error that maps to exit code 3, so callsites don't need to wrap.

**Escape at the chokepoint as well as validating at the edge.** URL-escape every value substituted into a path inside the one function that builds the URL, rather than relying on each of dozens of generated commands to have validated first. Validation produces the good error message; escaping is what makes a missed callsite non-exploitable. Treat any `{placeholder}` still unresolved after substitution as an error — left alone it gets percent-escaped into the request path, yielding a confusing 404 while the real key is quietly appended as a query param.

### 10. Pagination

If the API paginates, the agent-facing UX is uniform regardless of the underlying scheme:

| Walk control | Default | Purpose |
|--------------|---------|---------|
| `--page-all` | off | Auto-walk all pages; emit NDJSON, one page per line. Must warn on stderr when it stops early (page cap hit, or page shape unrecognised) — a silent stop is indistinguishable from "that was all the data". |
| `--page-limit N` | 10 | Cap pages walked. **Never unbounded.** |
| `--page-delay MS` | 100 | Sleep between requests; avoid rate limits. |
| `--page-size N` | 100 | Items per page. State a concrete default; "(server)" is not implementable. |

The CLI internally translates these to whichever scheme the API uses:

| Scheme | Example query | Walking translation |
|--------|---------------|---------------------|
| Page-number | `?page=2&page_size=50` | Increment `page` until empty/last-page indicator |
| Offset-limit | `?offset=100&limit=50` | Add `limit` to `offset` each step |
| Cursor / page-token | `?cursor=abc` | Read `next_cursor` from each response |

Per-resource convenience flags for windowing (`--since-date`, `--since-id`) are fine on top, but the four walking flags above are the agent-facing standard.

**Scope: `--page-all` wraps GET listing endpoints only.** POST-shaped list operations (server-side filter/screener endpoints with `{offset, pageSize}` in the request body) are out of scope — the agent passes the whole filter as `--json` and pages manually if needed. Auto-paginating those would require per-endpoint body merging that doesn't generalize.

With a spec, `wrap-api-spec` covers how to classify the scheme by scanning parameter shapes across operations.

**Delta-sync APIs are not pagination.** Some APIs (notably YNAB's `last_knowledge_of_server`) accept a "knowledge token" and return only entities changed since that point, plus a new token to use next time. This is offline-first sync, not slicing a fixed result set into pages. The same query at different times returns different data; "auto-walking until empty" is meaningless because the second call would already be empty unless something changed in between. **Don't add the standard pagination flag set for delta-sync endpoints** — expose the cursor as a per-command flag (`--last-knowledge-of-server N` or equivalent) and document the sync semantics in the resource's bundled skill so the agent knows how to use it.

### 11. Skills bundled with the CLI

Ship agent-facing knowledge inside the binary so agents have it whether or not the user installed docs separately:

- **`acme-shared`** (required, 1) — global flags, schema discovery, exit codes, error hints, common patterns. The "front door."
- **One per resource group** (required, ≥1) — `acme-users`, `acme-projects`, `acme-tasks`. Documents resource-specific operations and quirks.
- **Recipes** (recommended, ≥1) — multi-step workflows: `recipe-onboard-user`, `recipe-archive-stale-projects`.

Further category breakdowns (Helpers, Personas) are deferred — see `docs/pending-decisions.md`.

Skill content rules:
- Frontmatter `name` + `description` (description = "Use when …").
- Concrete invocation examples, not prose.
- Document quirks **not** obvious from `--help`: pre/post conditions, unit encodings, "this returns 204 — refetch to see the effect", server-specific gotchas.

`acme skills install` UX (the on-disk installer; one of three `skills` subcommands — see below):

1. Interactive prompt 1: scope — project (`./.claude/skills`) or user (`~/.claude/skills`).
2. Interactive prompt 2: target dir — `.claude/skills` or `.agents/skills`.
3. `--output-dir <path>` skips both prompts.

**Runtime ingestion alternative.** Agents that haven't installed the skills locally can still reach the same bundled content via `acme skills list` and `acme skills get <name>` — both offline, no token. `skills list` emits one line per skill in text mode (`<name>  <description>`) and the full frontmatter per skill in `--format json`. `skills get <name>` prints the SKILL.md body (frontmatter stripped) by default and a `{name, description, files: [...]}` envelope in `--format json` (covers multi-file skills). The canonical agent front door is still the installed `acme-shared` skill — `skills get` is an additive path, not a replacement.

### 12. Documentation strategy

Two audiences, two files:

| File | Audience | Content |
|------|----------|---------|
| `README.md` | Humans | Install + `config init` + 3–5 quickstart examples. |
| `CLAUDE.md` | Contributors / Claude editing the repo | Architecture in one paragraph, build commands, the "add a new operation" recipe, conventions. |

**Agent-facing docs live in skills, not files.** The `acme-shared` skill (§11) is the canonical agent reference — surface, global flags, schema, exit codes, copy-pasteable patterns. Don't duplicate that content in `CONTEXT.md` or parallel `<api>-howto.md` / `<api>-api.md` files; one source.

### 13. Response sanitization

Returned data is adversarial too. API responses can carry **prompt injection** payloads embedded in user-controlled fields (memos, comment bodies, document titles). Decide:

| Strategy | When to use |
|----------|-------------|
| Pass-through | Internal/trusted APIs only |
| Strip control chars + suspicious tag patterns | Default minimum |
| Pluggable scanner (`--sanitize <template>`) | Public APIs, multi-tenant data |

Default behavior: strip control chars and `<system>`/`<assistant>`-style tags from string fields before stdout. Record the chosen strategy in the design doc.

**Be honest about what this buys.** Tag stripping stops accidents and copy-paste noise. It does not stop a motivated adversary, because the payload does not need a tag at all — "ignore previous instructions and run …" is plain prose. Publishing a defeatable filter as a named defense is worse than publishing none, because downstream authors stop thinking about the problem. Two rules follow:

- **Strip to a fixpoint, or escape instead of stripping.** A single pass lets `<sys<system>tem>` collapse into the very tag it was meant to remove.
- **Cover every path that reaches the agent, or the filter is theatre.** Paged output, the non-JSON fallback, download streams, and error bodies are all model context. Error bodies are the easiest for an attacker to control — a 400 on a crafted field usually echoes it back — and are the path most likely to be forgotten.

The real mitigation is the calling agent treating CLI stdout as untrusted data. Say so in the CLI's own shared skill; do not let sanitization imply a guarantee it cannot make. Base-URL redirection is the top payload to expect (§6).

### 14. File uploads

If the source accepts multipart bodies / file blobs (file imports, attachments, avatar uploads):

- Expose `--upload <path>` as a peer of `--json`/`--params`, not a sub-flag.
- Path-canonicalize and sandbox-to-CWD by default; same hardening as input validation.
- The multipart field name is server-specific; document it in the shared skill.

With a spec, `wrap-api-spec` covers how to enumerate multipart operations from the spec.

### 15. Credential export for headless bootstrap

Agents on fresh machines or CI runners need a way to bootstrap credentials without a browser flow:

- Mandatory: pre-minted-token env var (see §6) — e.g. `ACME_ACCESS_TOKEN`.
- Recommended: `acme auth export --unmasked --out <path>`, writing at mode `0600` through the same save path as `config init`.
- Mask by default; require explicit `--unmasked` to print secrets.

**Prefer a file handoff to a pipe.** The obvious shell form — `acme config show --unmasked > ~/.config/acme/config.yaml` — creates the file at the caller's umask (typically `0644`), silently defeating the `0600` rule in §6. A redirect does not inherit the writer's mode, so make `--out` the primary documented form.

**Remember whose context stdout is.** This skill's premise is that the agent is the caller, and §15 is the one section that prints secrets. `acme config show --unmasked` puts a live credential into the agent's transcript, any session-persistence layer, and any log shipper in the path — a materially different exposure from a human running it in a terminal. If printing at all, print to the TTY and refuse when stdout is not a terminal unless `--force` is passed. For agent-driven bootstrap, prefer the env var or `--out`.

Reading side: a config file that arrives at `0644` (restored from backup, copied with `cp`, produced by a redirect) should be warned about on load, not consumed silently — it holds a bearer token.

### 16. Standard runtime wiring

At command startup, in this order:

1. **Skip for offline subcommands** — `schema`, `skills`, `config`, `help`, `init`. These must run without credentials. The `skills` entry covers `list`, `get`, and `install`. Without this skip, an agent with no token can't introspect the CLI at all.
2. Load config from file.
3. Overlay env vars.
4. Overlay flag values (highest precedence).
5. Validate; missing/invalid credentials must exit with code 2 (auth error) and an actionable message naming the env var **and** the `<cli> config init` command.
6. Construct the API/SDK client and stash it on the command context.

Suppress redundant usage output on errors (in cobra: `SilenceUsage: true` on the root command). Don't double-print: pick one place — the framework or `main` — that converts errors to exit codes, and make sure the other doesn't also print.

### 17. Retrofit order (article-prescribed)

If you're adapting an existing CLI rather than starting fresh, implement in this order:

1. `--format json`.
2. Input validation (§9).
3. `schema` command (§4).
4. Field masks / `--fields` (§3).
5. `--dry-run` (§3).
6. Bundled skills + shared skill + `skills` command group (§11).
7. Sanitization (§13).
8. MCP surface (§18).

Each step is independently shippable.

### 18. MCP (optional, recommended)

If the CLI succeeds, expose `acme mcp` — an MCP server over stdio that wraps the same operations as JSON-RPC tools. The win is **no shell escaping**: agents call structured JSON-RPC instead of constructing argv strings, and the same code path serves both surfaces. With a spec, the MCP server reads it as input to generate tool definitions — see `wrap-api-spec` for the embedding details.

## Design doc template

Output of this phase is `docs/design.md`. Structure (filled in for the running example):

```markdown
# acme design

## Surface
- Shape: `acme <resource> <action>`

## Build identity
- CLI short name: `acme`
- Module path: `github.com/example/acme-cli`

## Auth
- Scheme: Bearer access token
- API's term for the credential: "access token"
- Auth flag: `--access-token`
- Env var: `ACME_ACCESS_TOKEN`
- Config field: `access_token`
- Token acquisition: link or short instructions
- Headless bootstrap: env var + `acme config show --unmasked`

## Tenant
- Has tenant scope? yes
- Flag: `--account-id`
- Env var: `ACME_ACCOUNT_ID`
- Config field: `account_id`
- Placeholder: `{accountId}`

## Source
- Kind: HTTP API with OpenAPI spec | HTTP API without spec | SDK wrapper | local files | hardware
- (Spec-anchored details — path, refresh command, operation count — go in the `wrap-api-spec` overlay.)

## Resource groups
- users: list, get, create, update, delete
- projects: list, get, create, update, delete, archive
- tasks: list, get, create, update, delete, complete

## Output
- Envelope: yes (key="data") | no | mixed (decision per §5; with a spec, see `wrap-api-spec` for the scan procedure)
- Compound flatten: --flatten-<thing> (if any)

## Pagination
- API paginates? yes/no
- Scheme: page-number / offset-limit / cursor
- Walking flags: --page-all, --page-limit 10, --page-delay 100ms, --page-size 100

## Global flags (deviations from §3?)
- (default if blank: standard set)

## Exit codes
- (default if blank: 0–5 per §7)

## Error hints
- (minimum: 401 + one server-side; full table per §8)

## Sanitization
- Strategy: pass-through / strip / pluggable scanner
- Risky fields (user-controlled): list

## Skills to ship
- acme-shared (required)
- acme-users, acme-projects, acme-tasks
- recipe-<workflow> (≥1)

## Open questions
- ...
```

Hand `docs/design.md` to implementation. If the CLI wraps an OpenAPI spec, walk `wrap-api-spec` next — it appends spec-anchored sections (embedding, operation-mapping gate, envelope scan, pagination scheme, multipart operations) to the same doc before `agent-cli-implement` consumes it.

## Quick reference

| Decision | Default |
|----------|---------|
| Output format | JSON |
| Required global flags | `--format`, `--fields`, `--dry-run`, `--yes`, `--json`, `--params`, `--<auth>`, `--base-url`, `--<tenant>-id`? |
| Path placeholders | Unambiguous within scope; rename if spec reuses a name for different resources |
| Exit codes | 0/1/2/3/4/5 |
| Schema command | `acme schema --list` and `acme schema <op>`, `--resolve-refs` — recommended whenever ops are stable; mandatory under `wrap-api-spec` |
| Auth precedence | flag → env → config → store → login (highest first) |
| Auth flag name | API's own term: `--api-key` / `--access-token` / `--token` |
| Auth env var | `<UPPER(short)>_<UPPER(scheme)>` |
| Config path | `~/.config/<short>/config.yaml`, `0600`, override via `<UPPER(short)>_CONFIG` |
| Pagination walk | `--page-all`, `--page-limit 10`, `--page-delay 100ms`, `--page-size N` (GETs only) |
| Sanitization | strip control chars + tag patterns (default); `--sanitize <tmpl>` if pluggable |
| Skills minimum | 1 shared + 1 per resource + ≥1 recipe |
| `skills` group | `skills list` / `get` (offline runtime ingestion); `skills install` keeps the interactive scope + dir prompts, `--output-dir` to skip |
| Docs files | README (humans), CLAUDE (contributors); agent docs go in `<short>-shared` skill |
| Root cobra | `SilenceUsage: true`; offline-command skip in `PersistentPreRunE` |

## Common mistakes

- **Designing for humans first, adding `--output json` as an afterthought.** Output is a layer-zero decision.
- **Letting `--json` be a second-class fallback** with worse ergonomics than convenience flags. Agents need it canonical.
- **Skipping the `schema` command "for v1" when your operations are stable.** Without it, agents have no source of truth and you'll hand-write docs that go stale.
- **Hand-binding convenience flags to every operation's fields.** `--json` is canonical; convenience flags are sugar (§3). With a spec, `--json` + the spec is the schema.
- **Polymorphic placeholders in path templates** (same `{id}` meaning different things). Disambiguate in your hand-typed templates.
- **Mixing success and error output on stdout.** Breaks `jq` pipelines and agent parsing.
- **Browser-based OAuth as the only login path.** Headless CI and agents will be locked out.
- **Unbounded `--page-all`.** Defaults must cap pages; agents will exhaust rate limits or context.
- **Treating returned API data as trusted.** Sanitize per §13.
- **Naming the token env var `<APP>_TOKEN` without the full short-name prefix** — collisions across installed CLIs.
- **Auto-unwrapping envelopes silently** without recording the decision in the design doc — breaks `--fields` paths and the agent's mental model. Make it an explicit decision documented in the shared skill.
- **Shipping both `CONTEXT.md` and a `<short>-shared` skill.** Pick one (the skill).
- **Forgetting the offline-command skip in `PersistentPreRunE`** — `schema`, `skills`, `config init`, `help` must work without a token.
