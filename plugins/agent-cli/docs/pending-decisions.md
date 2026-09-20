# pending decisions

Decisions parked from the design-cli / implement-cli skills — not yet adopted as prescriptions, kept here so they're not lost.

## Skill taxonomy beyond shared/services/recipes

The gws CLI ships SKILL.md files in five categories:

| Category | Purpose | Status in our skill |
|----------|---------|---------------------|
| **Shared** | "Front door" — global flags, schema discovery, exit codes, error hints | Required (1 per CLI) |
| **Services** | One per API resource group | Required (≥1 per CLI) |
| **Recipes** | Multi-step workflows | Recommended (≥1 per CLI) |
| **Helpers** | One per `+command` compound helper | **Parked** |
| **Personas** | Role bundles ("personal-finance-analyst", "training-coach") that pull from multiple services | **Parked** |

The three reference CLIs (intervals/intelinvest/ynab) ship Shared + Services + Recipes only. Helpers and Personas haven't been needed yet.

**Decision deferred until:** we have a real use case where a multi-step compound command (`+command`) earns its own skill, or a role bundle would meaningfully reduce duplication across services.

**If/when adopted:**
- Helpers: one `SKILL.md` per `+command`, named `<cli>-<command-name>` (e.g. `<cli>-monthly-close`).
- Personas: name them `persona-<role>` (e.g. `persona-personal-finance-analyst`).

## `+command` prefix for hand-crafted compound commands

gws uses `+` (e.g. `gws gmail +send`, `gws sheets +append`) to mark commands that aren't auto-generated from the API spec — they're hand-crafted multi-call workflows. The prefix sorts them separately from generated commands in `--help`.

The reference fleet doesn't use this. **Parked** until a CLI in this template grows enough hand-crafted workflows to justify the visual separation.

## MCP exposure

`<cli> mcp` — an MCP server over stdio that exposes the same operations as JSON-RPC tools, built from the same embedded spec. The win is no shell escaping; agents call structured tool invocations.

**Parked** because none of the reference CLIs ships it yet, and the implementation cost (a separate stdio server, tool registration, type marshaling) is non-trivial. Promote to a standalone skill (`mcp-cli`?) when needed.

## Discovery-driven dynamic command surface

gws builds its command tree at runtime by querying Google's Discovery Service. New endpoints appear without rebuilding the binary.

**Skipped, not just parked.** The reference template is embedded-spec only — `go:embed openapi-spec.json` at build time. Discovery-driven would require a different runtime architecture (cobra's command tree built dynamically, not from `init()`) and the upside doesn't apply to most APIs (which version slowly).

## Encrypted credential store in auth precedence

The auth precedence chain in design-cli §6 lists "encrypted store" as an optional step between config file and interactive login. None of the reference CLIs implements it.

**Parked** until a CLI in this template needs to ship to environments that disallow plaintext config files (enterprise, regulated industries).

## Structured exit codes 0–5

design-cli §7 prescribes the gws codes as a target. The reference CLIs all exit 1 uniformly.

**Path forward:** when the next new CLI in this template is built from `implement-cli`, adopt the full 0–5 split; backport to the existing three on a separate task.

## `--page-all` / `--page-limit` / `--page-delay` walking flags

design-cli §10 prescribes them. None of the reference CLIs implements them yet — they expose per-command `--offset` / `--limit` typed flags and require the agent to paginate manually.

**Path forward:** same as exit codes — adopt for the next new CLI; backport later.
