# builder-toolkit

[![CI](https://github.com/glebmish/builder-toolkit/actions/workflows/ci.yml/badge.svg)](https://github.com/glebmish/builder-toolkit/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/glebmish/builder-toolkit)](https://github.com/glebmish/builder-toolkit/releases)
[![License: MIT](https://img.shields.io/github/license/glebmish/builder-toolkit)](LICENSE)

A collection of skills and plugins loosely related to building projects.

Each entry below ships as a Claude Code plugin (via this marketplace) and
as a standalone skill installable with [`npx skills`](https://github.com/vercel-labs/skills).
Bundled hooks (where present) are only registered on the plugin-install
path; the skill-only install gives you the SKILL.md without hook
enforcement.

```
claude plugin marketplace add glebmish/builder-toolkit
npx skills add glebmish/builder-toolkit
```

## Plugins

### git-history-rewrite

Safety net for destructive git operations — bundles a `PreToolUse` hook that
blocks unsafe forms of `git` history rewrite commands and steers toward creating
backups before running those commands.

![Hook blocking `git filter-repo` without `--refs`, with the agent explaining why the block protects the recovery net](plugins/git-history-rewrite/docs/images/hook-block.png)

[Full details →](plugins/git-history-rewrite/README.md)

**Install**

As a Claude Code plugin (recommended — enables the hook):

```
claude plugin marketplace add glebmish/builder-toolkit
claude plugin install git-history-rewrite@builder-toolkit
```

As a standalone skill (skill body only, no hook):

```
npx skills add glebmish/builder-toolkit --skill git-history-rewrite
```

**Prerequisites**

`jq` on `PATH` — the PreToolUse hook parses tool-input JSON with it. Only
required for the plugin install path.

### share-artifact

Share a finished image or GIF with the user as a viewable, signed URL —
uploads it to Cloudinary and pastes back a link that works on mobile, a chat
client, or any remote control where a local file path is useless. Scoped to
images and GIFs; it does not produce or edit artifacts.

[Full details →](plugins/share-artifact/README.md)

**Install**

As a Claude Code plugin:

```
claude plugin marketplace add glebmish/builder-toolkit
claude plugin install share-artifact@builder-toolkit
```

As a standalone skill:

```
npx skills add glebmish/builder-toolkit --skill share-artifact
```

**Prerequisites**

`cld` on `PATH` (`pipx install cloudinary-cli`) and a `CLOUDINARY_URL`
exported for the target Cloudinary account.

## Releases

Versioned releases are published at [GitHub Releases](https://github.com/glebmish/builder-toolkit/releases).

## License

[MIT](LICENSE)
