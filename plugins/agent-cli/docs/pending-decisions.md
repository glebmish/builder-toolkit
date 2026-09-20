# Pending decisions

Design choices considered for the `agent-cli-design` / `agent-cli-implement`
skills and deliberately **not** adopted as prescriptions, recorded so the
reasoning is not lost and so the absence reads as a decision rather than an
oversight.

Everything here describes the state of *this template*. Each entry says what
would have to become true for the decision to be revisited.

## Skill taxonomy beyond shared/services/recipes

The `gws` CLI ([`googleworkspace/cli`](https://github.com/googleworkspace/cli)) ships SKILL.md files in five categories:

| Category | Purpose | Status in our skill |
|----------|---------|---------------------|
| **Shared** | "Front door" — global flags, schema discovery, exit codes, error hints | Required (1 per CLI) |
| **Services** | One per API resource group | Required (≥1 per CLI) |
| **Recipes** | Multi-step workflows | Recommended (≥1 per CLI) |
| **Helpers** | One per `+command` compound helper | **Parked** |
| **Personas** | Role bundles ("personal-finance-analyst", "training-coach") that pull from multiple services | **Parked** |

The template prescribes Shared + Services + Recipes only; Helpers and Personas have not been needed.

**Decision deferred until:** we have a real use case where a multi-step compound command (`+command`) earns its own skill, or a role bundle would meaningfully reduce duplication across services.

**If/when adopted:**
- Helpers: one `SKILL.md` per `+command`, named `<cli>-<command-name>` (e.g. `<cli>-monthly-close`).
- Personas: name them `persona-<role>` (e.g. `persona-personal-finance-analyst`).

## `+command` prefix for hand-crafted compound commands

gws uses `+` (e.g. `gws gmail +send`, `gws sheets +append`) to mark commands that aren't auto-generated from the API spec — they're hand-crafted multi-call workflows. The prefix sorts them separately from generated commands in `--help`.

The template doesn't use this. **Parked** until a CLI in this template grows enough hand-crafted workflows to justify the visual separation.

## A reference implementation of `<cli> mcp`

The MCP surface itself is **not** parked — `agent-cli-design` §18 recommends it, §17 places it at step 8 of the retrofit order, and `wrap-api-spec` §8 covers generating tool definitions from the spec.

What's parked is a *snippet* for it. `agent-cli-implement` ships no `mcp.go`, so the design prescribes the surface while the implementation skill leaves you to build it. The cost is non-trivial (a stdio server, tool registration, type marshaling) and it would roughly double the snippet set.

**Revisit when:** the snippet set has a stable enough shape that an MCP server can be generated from the same `operationIDToCommand` map rather than hand-written alongside it.

## Discovery-driven dynamic command surface

gws builds its command tree at runtime by querying Google's Discovery Service. New endpoints appear without rebuilding the binary.

**Skipped, not just parked.** The reference template is embedded-spec only — `go:embed openapi-spec.json` at build time. Discovery-driven would require a different runtime architecture (cobra's command tree built dynamically, not from `init()`) and the upside doesn't apply to most APIs (which version slowly).

## Encrypted credential store in auth precedence

The auth precedence chain in `agent-cli-design` §6 lists "encrypted store" as an optional step between config file and interactive login. The template does not implement it — `agent-cli-implement` ships a plaintext `0600` YAML file and nothing else.

**Parked** until a CLI in this template needs to ship to environments that disallow plaintext config files (enterprise, regulated industries).
