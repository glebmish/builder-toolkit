# builder-toolkit

[![CI](https://github.com/glebmish/builder-toolkit/actions/workflows/ci.yml/badge.svg)](https://github.com/glebmish/builder-toolkit/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/glebmish/builder-toolkit)](https://github.com/glebmish/builder-toolkit/releases)
[![License: MIT](https://img.shields.io/github/license/glebmish/builder-toolkit)](LICENSE)

Claude Code plugins and skills, loosely related to building projects.

Each entry below ships as a Claude Code plugin (via this marketplace) and
as standalone skills installable with [`npx skills`](https://github.com/vercel-labs/skills).
Bundled hooks (where present) are only registered on the plugin-install
path; the skill-only install gives you the SKILL.md without hook
enforcement.

```
claude plugin marketplace add glebmish/builder-toolkit
npx skills add glebmish/builder-toolkit
```

The first command adds the marketplace but installs nothing — each plugin
README has its `claude plugin install` line. The second offers every skill
in the repo for selection.

## Plugins

### agent-cli

Design and implement CLIs whose primary user is an AI agent rather than a
person — the stack-agnostic chassis (`agent-cli-design`), an OpenAPI
overlay (`wrap-api-spec`), and a Go scaffolder with tested reference
snippets (`agent-cli-implement`). Ships guidance and code to copy; it
installs no binary.

The three skills, and the full design stance behind them, are in the
[plugin README →](plugins/agent-cli/README.md).

### git-history-rewrite

Safety net for destructive git operations — bundles a `PreToolUse` hook that
blocks unsafe forms of `git` history rewrite commands and steers toward creating
backups before running those commands.

![Hook blocking `git filter-repo` without `--refs`, with the agent explaining why the block protects the recovery net](plugins/git-history-rewrite/docs/images/hook-block.png)

Install, prerequisites, and the full command reference are in the
[plugin README →](plugins/git-history-rewrite/README.md).

### share-artifact

Share a finished image or GIF with the user as a viewable, signed URL —
uploads it to Cloudinary and pastes back a link that works on mobile, a chat
client, or any remote control where a local file path is useless. Scoped to
images and GIFs; it does not produce or edit artifacts.

Install, prerequisites, and the privacy/cleanup model are in the
[plugin README →](plugins/share-artifact/README.md).

## Releases

Versioned releases are published at [GitHub Releases](https://github.com/glebmish/builder-toolkit/releases).

Both install commands above resolve the default branch: neither accepts a
ref, so an install does not track `vX.Y.Z`. Treat a release as a changelog
anchor and a point to diff against rather than an install pin. (You can
still pin by hand — clone at a tag and add the marketplace from that local
path.)

To pick up changes: re-run `npx skills add` for the skills install, which
overwrites in place. The plugin install needs `claude plugin marketplace
update builder-toolkit` followed by `claude plugin update <plugin>` —
re-running `marketplace add` is a no-op once the marketplace is
configured.

## License

[MIT](LICENSE)
