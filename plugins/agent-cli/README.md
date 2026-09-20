# agent-cli

Skills for designing and implementing CLIs whose primary user is an AI
agent rather than a person.

Human CLI design optimizes for discoverability and forgiveness. Agent CLI
design optimizes for predictability and defense in depth — the agent
cannot read a man page, cannot ask what a flag meant, and will happily
retry a destructive call it misread. Those goals pull in different
directions and cannot be retrofit into one interface, so these skills
design for the agent first and layer human conveniences on top.

Despite the name, this plugin does not install a CLI and is unrelated to
any tool named `agent-cli`. It ships guidance and reference code you copy
into a CLI of your own.

## Install

Claude Code:
```
claude plugin marketplace add glebmish/builder-toolkit
claude plugin install agent-cli@builder-toolkit
```

Standalone skills:
```
npx skills add glebmish/builder-toolkit --skill agent-cli-design
npx skills add glebmish/builder-toolkit --skill wrap-api-spec
npx skills add glebmish/builder-toolkit --skill agent-cli-implement
```

**Prerequisites** — only for `agent-cli-implement`; the two design skills
need nothing.

- Go ≥ 1.22.
- `yq` on `PATH`, to convert an OpenAPI spec to the embedded JSON form.
  Install: `brew install yq` or `snap install yq`.

## The three skills

| Skill | Phase | Use it when |
|---|---|---|
| `agent-cli-design` | Design | Starting any agent-facing CLI, before implementation |
| `wrap-api-spec` | Design overlay | The CLI wraps an OpenAPI spec |
| `agent-cli-implement` | Implementation | Building the designed CLI in Go |

They compose in that order and are deliberately separable.

**`agent-cli-design`** is stack- and source-agnostic: it applies whether
the CLI wraps an HTTP API, an SDK, local files, or hardware. It covers the
chassis every agent CLI needs — command surface, global flags, the output
contract and envelope decision, structured exit codes, error hints keyed
by status, auth precedence, input hardening, response sanitization,
pagination, file uploads, and the skills the CLI should ship with itself.
Its output is a short design doc.

**`wrap-api-spec`** is an overlay on the design phase, not a replacement.
When an OpenAPI spec exists, it adds the decisions the spec can answer:
embedding the spec, deriving a `schema` introspection command from it,
enforcing full endpoint coverage through an operation-mapping gate,
driving action verbs from `operationId`s, disambiguating reused path
placeholders, and classifying the pagination scheme from parameter shapes.

**`agent-cli-implement`** turns the design doc into Go, built on Cobra plus
plain `net/http`. Intentionally minimal: no codegen, no HTTP framework, no
struct-binding libraries. Every operation is a thin wrapper over three
shared helpers.

## Working code, not pseudocode

`agent-cli-implement` keeps its reference implementation in
[`skills/agent-cli-implement/snippets/`](skills/agent-cli-implement/snippets)
rather than inline in the skill body, so what you copy is what was tested.
The snippets are a coherent set — client, config cascade, output
formatting and sanitization, input validation, typed exit errors, the
cobra root, the workhorse helpers, and the spec-bijection test.

They are **reference material, not a module**: there is no `go.mod`, and
they import a deliberately fictional `github.com/example/acme-cli` that
you replace with your own path. They were checked by assembling them into
a throwaway module and running `go build`, `go vet` and `go test`.

That assembly needs two things the snippets deliberately omit, because
both are per-project: the embedded `openapi-spec.json`, and the
per-resource command files. Until you supply them, the bijection test in
`internal/cmd` fails by design — it is reporting that the spec, the
`operationIDToCommand` map and the command tree do not yet agree, which is
exactly its job. The other four packages pass on the snippets alone. CI
here keeps them honest with `gofmt` and a parse check; see
[`.github/workflows/ci.yml`](../../.github/workflows/ci.yml).

Unit tests ship alongside the sources. They earn their place: the defects
they cover — integer IDs corrupted by a `float64` round-trip, query params
rendered in scientific notation, sanitization quietly skipped on the
paginated path — produce no compiler error, no test failure and no runtime
error. They are only visible if something asserts on them.

## Design stance

A few positions these skills take, so you can disagree with them
deliberately rather than by accident:

- **Treat the agent as untrusted.** It hallucinates IDs, retries blindly,
  and pastes values it did not read. Validate at the edge for a good error
  message, and escape at the chokepoint so a missed callsite is not
  exploitable.
- **Treat API responses as adversarial.** Anything a third party can write
  into a field reaches the agent's context. Sanitization is the default
  minimum, and it stops accidents rather than adversaries — the real
  mitigation is the agent treating CLI output as data.
- **Exit codes are a contract.** An agent branches on them, so a panic
  that exits with the code reserved for auth failure sends it off to
  re-authenticate instead of reporting a bug.
- **Silence is a lie.** A pagination walk that stops early, or a field
  mask that drops everything, must say so on stderr — to an agent, a quiet
  stop is indistinguishable from "that was all the data".

## Source material

- [Rewrite Your CLI for AI Agents](https://justin.poehnelt.com/posts/rewrite-your-cli-for-ai-agents/)
  — Justin Poehnelt's design principles.
- [`googleworkspace/cli`](https://github.com/googleworkspace/cli) — the
  reference implementation for structured exit codes, NDJSON paging, and
  schema introspection.

Design choices considered and deliberately parked are recorded in
[`docs/pending-decisions.md`](docs/pending-decisions.md); design records
for individual changes live in [`docs/specs/`](docs/specs).
