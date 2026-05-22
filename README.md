# builder-toolkit

A [Claude Code](https://docs.claude.com/en/docs/claude-code) plugin marketplace
by glebmish.

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

## License

[MIT](LICENSE)
