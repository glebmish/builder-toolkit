# `skills` command group — design

**Date:** 2026-05-13
**Scope:** `plugins/agent-cli` — both `agent-cli-design` and `agent-cli-implement` SKILL.md prescriptions.
**Status:** approved for plan.

## Problem

The current `agent-cli-design` / `agent-cli-implement` skills prescribe a single
`acme install-skills` command that copies an `embed.FS` of bundled skills to
`~/.claude/skills` (or `./.claude/skills`). That works for users who run the
install step, but a fresh agent invocation that has only the binary — no
pre-installed skills, no `.claude/skills` directory — has no way to read skill
content. The agent needs a runtime path that does not depend on a prior
install.

## Goal

Add a runtime alternative for ingesting bundled skills: an agent calls the CLI
to enumerate available skills and fetch one. No filesystem state required.

The pre-installed-skills path remains the canonical front door (`acme-shared`,
§11, §12). This spec adds a parallel runtime path; it does not reframe the
existing narrative.

## Command surface

The existing `install-skills` command is replaced by a `skills` command group:

```
acme skills list                       # enumerate bundled skills
acme skills get <name>                 # fetch one skill
acme skills install [--output-dir P]   # current install-skills UX, moved under the group
```

All three are **offline subcommands**. They appear in the
`PersistentPreRunE` skip list and require no token or network.

## `skills list`

Walks the embedded `internal/cmd/skills/` `embed.FS`, reads each
`<name>/SKILL.md`, parses frontmatter.

**Default (text):** one line per skill, two-column.

```
acme-shared    Use when calling acme — global flags, schema discovery, exit codes.
acme-projects  Use when working with projects — list/create/archive quirks.
recipe-onboard-user  Use when bootstrapping a new acme user end-to-end.
```

**`--format json`:** array of objects, **full frontmatter passed through** (not
just `name` + `description`). Whatever keys the SKILL.md declares are emitted:

```json
[
  {
    "name": "acme-shared",
    "description": "Use when calling acme — global flags, schema discovery, exit codes."
  },
  {
    "name": "recipe-onboard-user",
    "description": "Use when bootstrapping a new acme user end-to-end.",
    "tags": ["recipe", "onboarding"]
  }
]
```

The implementation treats frontmatter as an opaque `map[string]any` after
parsing — it does not enumerate known keys. This keeps the contract stable
as skill authors add new frontmatter fields.

## `skills get <name>`

Walks the embedded FS rooted at `<name>/`.

**Default (raw):** prints **only the SKILL.md body** to stdout, with the
frontmatter block stripped. Rationale: the agent has already seen
`name` + `description` via `skills list`; the actionable content is the body.

```
$ acme skills get acme-shared
# acme — agent reference

Global flags:
  ...
```

**`--format json`:** structured envelope; faithful file dump (frontmatter
**not** stripped from file contents):

```json
{
  "name": "acme-shared",
  "description": "Use when calling acme — global flags, schema discovery, exit codes.",
  "files": [
    {"path": "SKILL.md", "content": "---\nname: acme-shared\ndescription: ...\n---\n# acme — agent reference\n..."},
    {"path": "snippets/curl-equivalent.sh", "content": "..."}
  ]
}
```

The envelope covers multi-file skills (skills with `snippets/` or
`references/` subdirectories) in one round-trip. Multi-file skills are not
required; single-file skills emit `files` with one entry.

## Errors

| Condition | Type | Exit | Message |
|-----------|------|------|---------|
| Unknown skill name | `*cliexit.ValidationError` | 3 | `unknown skill "foo". Run \`acme skills list\` to see available skills.` |
| Embedded FS missing or unreadable | `*cliexit.DiscoveryError` | 4 | `bundled skills not found — this is a build defect, please report.` |
| SKILL.md frontmatter parse failure | `*cliexit.DiscoveryError` | 4 | Names the offending skill and line. |

Frontmatter parse failures are discovery-class because they indicate the CLI
binary itself is malformed; users cannot fix them.

## Implementation impact (for `agent-cli-implement`)

- **Rename** `internal/cmd/install_skills.go` → `internal/cmd/skills.go`.
- That file holds the `skills` parent cobra command plus three subcommands
  (`list`, `get`, `install`). The `install` subcommand body is the current
  `install_skills.go` body, unchanged.
- The `embed.FS` of `internal/cmd/skills/` stays put — same directory layout,
  same file mode rules (`0644` for files, `0755` for directories).
- `PersistentPreRunE` offline-skip list: replace the `"install-skills"` case
  with `"skills"` in `isOffline`. Because `isOffline` walks up the cobra
  parent chain, matching on the group name automatically covers `skills`,
  `skills list`, `skills get`, and `skills install`.
- Frontmatter parsing uses a minimal YAML dependency or a tiny hand-rolled
  splitter (`---\n…\n---\n`) — same approach used elsewhere in the snippets.
- `cmd/integration_test.go` adds three cases:
  - `skills list` text output contains every bundled skill name.
  - `skills list --format json` decodes to one object per bundled skill.
  - `skills get <known>` stdout starts with a non-frontmatter line.

Reference table update (§ "Quick reference"):

```
| Skills | internal/cmd/skills/ + skills.go | embed.FS; list/get/install subcommands; runtime alt to disk install |
```

## Doc-strategy impact (for `agent-cli-design`)

Minimal — preserve the existing front-door narrative.

- §11 "Bundled skills" gains a short paragraph at the end:
  *"For agents that haven't installed the skills locally, the same content is
  reachable at runtime via `acme skills list` / `acme skills get <name>`.
  This is an alternative ingestion path; the canonical front door is still
  the installed `acme-shared` skill."*
- §16 "Standard runtime wiring" offline-skip list: `install-skills` → `skills`.
- §17 "Retrofit order" step 6 wording: "Bundled skills + shared skill + `skills` group."
- §11 `acme install-skills` UX heading: rename to `acme skills install` UX;
  the prompt flow is unchanged.

No changes to §12 (documentation strategy), §15 (credential export), or the
agent front-door framing.

## Out of scope

- `skills get <name> <subpath>` for fetching one sub-file in raw mode.
  Sub-files are reachable via `--format json`; raw-mode single-file fetch
  can be added later if a real use case appears.
- A `skills search` / fuzzy match command. `list` is small (handful of
  skills); agents can filter the JSON themselves.
- Versioning or content hashing of bundled skills. The binary version is the
  version.
- An MCP surface for skills. If `acme mcp` (§18) ships, `skills.list` /
  `skills.get` are trivial to add later; not required for this spec.

## Acceptance

- `acme skills list` and `acme skills list --format json` both work offline.
- `acme skills get <name>` prints body-only by default; `--format json` returns
  the full envelope including frontmatter in file contents.
- `acme skills install` matches the current `install-skills` UX exactly.
- `cmd/integration_test.go` covers the three cases above.
- `agent-cli-design/SKILL.md` and `agent-cli-implement/SKILL.md` updated per the impact
  sections; bijection / structure unchanged.
