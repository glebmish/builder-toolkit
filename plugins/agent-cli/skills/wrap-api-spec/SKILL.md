---
name: wrap-api-spec
description: "Use when the CLI you're designing wraps an OpenAPI spec, on top of design-cli — adds spec-anchored decisions to the same design doc: embed the spec, derive `schema` from it, enforce full coverage via an operation-mapping gate, drive action verbs from operationIds, scan response schemas for the envelope decision, disambiguate reused path placeholders, classify pagination scheme from parameter shapes, and find multipart operations."
---

# wrap-api-spec

## Overview

This skill is an **overlay** on `design-cli`. `design-cli` covers the chassis any agent CLI needs — output contract, global flags, exit codes, error hints, input hardening, response sanitization, bundled skills. `wrap-api-spec` covers what changes when an OpenAPI spec exists for the API the CLI is wrapping.

Read `design-cli` first. Walk this overlay second. Record the spec-driven decisions in the same `docs/design.md`.

## Why the spec changes the shape

A spec gives you four things `design-cli` otherwise leaves to judgement:

- **A full enumeration of operations.** Coverage becomes a gate, not a guess.
- **A canonical name per operation** (`operationId`). Action verb naming stops being a design problem.
- **A schema per request and response.** `schema <op>` becomes a real introspection surface and the envelope decision becomes a procedure instead of speculation.
- **A canonical parameter shape per operation.** Pagination scheme detection and multipart-operation discovery become spec scans.

In return, the CLI takes on one obligation: stay in sync with the spec — checked by the build, not by review.

## Design checklist

Walk through these in order. Each section produces a decision recorded in `docs/design.md` (alongside the chassis decisions from `design-cli`).

### 1. Spec embedding

The spec ships **inside the binary** — not as a sibling file the agent has to locate at runtime. Two reasons: agents can't be relied on to keep an external file in sync with the binary, and the `schema` command has to work without network access.

Required:

- The binary carries a snapshot of the spec at build time.
- A reproducible refresh step (typically `make spec`) regenerates the embedded copy from upstream and runs the operation-mapping test (§4).
- The embedded format is whichever the runtime reads cheapest. JSON beats YAML at runtime in most stacks.

Record in `docs/design.md`:

- Spec path (`openapi.yaml`, `<api>-api.json`).
- Refresh command.

### 2. Action verbs from operationIds

With a spec, action names are no longer a judgement call — they come from `operationId`, normalized to the CLI's separator convention (kebab-case in most cases).

**Gate:** every action verb in the CLI traces back to exactly one `operationId`. No invented verbs, no merged verbs, no shortened verbs.

If the spec's `operationId`s are bad (camelCase, abbreviated, inconsistent), normalize them once in a mapping table — don't paper over them at every callsite.

### 3. Path placeholder disambiguation

Placeholders must be unambiguous within your CLI's scope. With a spec, the gate is mechanical: scan every path template. If the same placeholder name (`{id}`) appears across different resources, rename to disambiguated forms (`{accountId}`, `{projectId}`) in the hand-typed templates the command code passes to the client.

The client stays dumb (one tenant placeholder substitution). The command code, which knows the API, does the rename. Bare `{id}` is fine when the spec uses it for one resource only.

### 4. Schema command and operation-mapping gate

With a spec, `schema` is the **canonical agent surface** — the spec is in the binary, so introspection is free, and there's no other source of truth that can drift from the CLI.

Required surface:

- `<cli> schema --list` — every operation in command form (`<resource>.<action>` or `<service>.<resource>.<action>`).
- `<cli> schema <op>` — request body shape, response shape, required scopes/permissions, HTTP method, path.
- `<cli> schema <TypeName>` — type definitions, with `--resolve-refs` to inline `$ref`s.

This must work offline and without credentials.

**Operation-mapping gate.** The spec enumerates operations. The CLI exposes commands. The gate:

> Every `operationId` resolves to exactly one CLI command, and every CLI command resolves to exactly one `operationId`.

This is a **test**, not a convention. Run it in CI; fail the build on drift. Without it, "full coverage" decays the first time someone refreshes the spec and forgets a new operation.

If you genuinely want to exclude an operation (deprecated, scope-restricted, server-only), the exclusion is an explicit named list inside the test — not silence.

### 5. Envelope analysis from response schemas

The envelope decision (`no`, `yes (key="X")`, `yes (key="X", recursive=true)`, `mixed`) is part of the chassis output contract. With a spec, the decision is a procedure, not a guess.

Walk every 2xx response across every operation and answer in order:

1. Is there a single constant top-level key wrapping the real payload (`data`, `result`, `payload`, `response`)?
   - All operations wrap the same key → candidate for unwrap.
   - No wrapping anywhere → `Envelope: no`. Done.
   - Some wrap, some don't → `Envelope: mixed`. Don't auto-unwrap; document the inconsistency in the shared skill.

2. If there's a constant wrapper, does it carry useful metadata, or is it pure namespacing?
   - `{"data": <payload>}` alone → pure namespacing, worth unwrapping.
   - `{"data": <payload>, "request_id": "...", "rate_limit": {...}}` → not worth unwrapping; agents may want the metadata for debugging or rate-limit awareness.
   - `{"data": <payload>, "pagination": {"next": "..."}}` → depends. If auto-paginating, the client extracts pagination internally and unwrap is fine; otherwise keep wrapped so the agent can read the cursor.
   - `{"data": {"<resource>": [...]}}` per-operation inner key → recursive unwrap.

3. Would unwrapping break `--fields` ergonomics? Whichever way you go, agents must consistently type either `--fields id,name` (unwrapped) or `--fields data.id,data.name` (wrapped). Pick once; document in the shared skill.

The agent doesn't decide unilaterally — present the analysis to the human and confirm before recording in `docs/design.md`.

### 6. Pagination scheme detection

The chassis specifies the agent-facing flag set (`--page-all`, `--page-limit`, `--page-delay`, `--page-size`). The spec tells you which underlying scheme to translate to:

| Spec signal | Scheme |
|---|---|
| Operations with `page` + `page_size` query params | page-number |
| Operations with `offset` + `limit` query params | offset-limit |
| Responses carrying `next_cursor` / `next_page_token` | cursor |
| POST list operations with `{offset, pageSize}` in request body | **out of scope** for `--page-all` (pass `--json`, page manually) |
| Knowledge-token endpoints (e.g. YNAB's `last_knowledge_of_server`) | **not pagination** — delta-sync; expose as a per-command cursor flag, document in the resource skill |

Record the scheme in `docs/design.md`. If the spec mixes schemes across operations, that's a per-operation note, not a global decision.

### 7. Multipart-operation scan

The chassis defines `--upload <path>`. With a spec, the gate is: scan operations for `requestBody.content."multipart/form-data"`, list them, note the multipart field name per operation (server-specific — not derivable without inspection), and surface those names in the bundled skills so the agent doesn't have to guess.

### 8. MCP from the same spec

If you're exposing an MCP surface, the marginal cost drops with a spec: the MCP server reads the same embedded spec (§1) and generates JSON-RPC tool definitions, so one source feeds both the CLI surface and the MCP surface. This is the cheapest moment to add MCP.

## Sections to append to `docs/design.md`

On top of `design-cli`'s template:

```markdown
## Spec
- Path: openapi.yaml
- Refresh: make spec
- Operation count: N
- Excluded operations (with reason): ...

## Operation-mapping
- Test location: <path/to/test>
- Drift policy: build fails on mismatch
- Explicit exclusions: [<operationId> — reason]

## Envelope (from spec scan)
- Decision: no | yes (key="X") | yes (key="X", recursive=true) | mixed
- Reasoning: <one line>

## Path placeholders
- Spec-reused names renamed: {id} → {accountId} (accounts/*), {id} → {projectId} (projects/*)

## Pagination scheme
- Scheme: page-number | offset-limit | cursor | delta-sync (per-command cursor) | mixed
- Translation notes: <how the four walking flags map>

## Multipart operations
- <operationId> — field name: <name>
```

Hand `docs/design.md` + the OpenAPI spec to `implement-cli`.

## Common mistakes specific to spec-wrapping

- **Treating the spec as a build-time-only artifact** and shipping it as a sibling file. Embed it; the agent has to be able to call `schema` without internet.
- **Hand-binding flags to every spec field** for every operation. `--json` plus the spec is the schema; convenience flags are sugar (`design-cli` §3).
- **Letting the operation-mapping test be a soft warning.** Either it fails the build or it'll silently rot.
- **Auto-unwrapping the envelope silently** without recording the decision in the design doc — breaks `--fields` paths and the agent's mental model.
- **Generating CLI code from the spec via codegen.** The spec is the *schema*; the command code is hand-written, thin, and reviewable. Codegen reintroduces the drift the operation-mapping test was designed to catch.
