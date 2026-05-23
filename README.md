# builder-toolkit

A [Claude Code](https://docs.claude.com/en/docs/claude-code) plugin marketplace
of guardrails and discipline aids for agentic coding — currently one plugin
(`git-history-rewrite`) that blocks unsafe forms of destructive git commands
before they run.

Status: early. One plugin, single maintainer, `0.x` line.

## Requirements

- Claude Code with plugin support.
- `jq` on `PATH` — the `git-history-rewrite` plugin's PreToolUse hook parses tool-input JSON with it. macOS: `brew install jq`. Debian/Ubuntu: `apt install jq`.

## Install

Add this marketplace to Claude Code:

```
/plugin marketplace add glebmish/builder-toolkit
```

Then browse and install plugins:

```
/plugin
```

## Plugins

| Plugin | Description |
|--------|-------------|
| [`git-history-rewrite`](plugins/git-history-rewrite) | Safety net for destructive git operations — bundles a `PreToolUse` hook that blocks unsafe forms of `git filter-repo`, the `git push --force` family, and non-HEAD `git reset --hard`. The skill body covers the discipline items (sibling backup ref, off-repo bundle, post-rewrite verification) the hook can't enforce mechanically. |

## Scope and limitations

The hook is a guard against typical LLM-emitted command idioms, not an
adversarial sandbox: it inspects the command Claude is about to run, not files
that command references, and intentionally over-blocks rather than try to
parse arbitrary shell. See [`SKILL.md` → Scope of the hook](plugins/git-history-rewrite/skills/git-history-rewrite/SKILL.md#scope-of-the-hook)
for the full set of edges (wrapper handling, the literal-string trade-off,
script-indirection gap).

## Releases

Versioned releases with auto-generated notes are published at
[GitHub Releases](https://github.com/glebmish/builder-toolkit/releases).

## License

[MIT](LICENSE)
