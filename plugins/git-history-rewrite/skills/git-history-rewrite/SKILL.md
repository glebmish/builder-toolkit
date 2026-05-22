---
name: git-history-rewrite
description: Use when rewriting git history — running `git filter-repo`, `git push --force`, `git reset --hard`, or anything else that overwrites commits, refs, or remote state. Covers the correct form for each operation and bundles a hook that enforces those forms. Don't use for forward-only work (commit, merge, rebase against current HEAD), for branch creation/deletion, or for working-tree cleanup.
hooks:
  PreToolUse:
    - matcher: "Bash"
      hooks:
        - type: command
          command: "${CLAUDE_PLUGIN_ROOT}/skills/git-history-rewrite/hooks/preflight.sh"
---

# git-history-rewrite

## Overview

The skill's purpose is not to discourage git history override operations — sometimes they are exactly the right tool — but to ensure each is done in a form that preserves recovery surface. Each in-scope operation has a known failure mode that makes the rewrite irrecoverable; each also has a correct form that prevents that failure mode. The bundled hook fires on the matching commands and blocks them when the correct form is missing, with a block message that names the missing gate and shows the corrected form. This body covers the reasoning behind each gate and the discipline items the hook cannot verify mechanically.

## What the hook enforces

- **History rewrites stay scoped to one named ref.** Other refs (sibling branches, tags, remote-tracking refs, ad-hoc backup refs) survive the operation untouched and serve as recovery points. `git filter-branch` is blocked outright — it predates safe scoping and git's own documentation now redirects to `filter-repo`. `git filter-repo` is allowed only with `--refs <ref>`.
- **One transformation per rewrite pass.** Path strips, message edits, commit drops, content replacements, and mailmap remaps each have their own behavior; combined in one filter-repo invocation they interact in ways that are not obvious from the manual. They run as separate passes with a verification step between each.
- **The working tree is clean before the rewrite begins.** Rewrites align the working tree to the new HEAD; uncommitted work is overwritten without warning.
- **Force pushes assume both a known remote state and a known local state.** `--force-with-lease --force-if-includes` is the only form allowed: the lease catches stale-remote clobbers, and the includes check catches stale-local clobbers where a recent fetch silently made the lease tautological.
- **Hard resets never silently orphan commits.** A `git reset --hard` whose target would leave commits unreachable from any other ref is blocked. The block message names the orphan candidates and points to the backup-ref discipline below.

## What the agent enforces

- **Before any rewrite — create a sibling backup ref.** `git branch backup/pre-<topic> HEAD` (or `git tag backup/pre-<topic>`). Under the `--refs <target>` scoping the hook enforces, this ref is unaffected by the rewrite, and the pre-rewrite tip stays reachable through it.
- **Before `git filter-repo` specifically — also create an off-repo bundle.** `git bundle create /tmp/<repo>.bundle --all`. This survives in-repo failure modes the sibling ref cannot: a misconfigured `--refs` flag, an accidental backup-ref inclusion in the scope, the post-rewrite `gc --prune=now` that filter-repo runs automatically, or destruction of `.git` itself. Restoration is `git clone /tmp/<repo>.bundle <restored-dir>/`.
- **After verification AND explicit user acceptance — clean up the backups.** Delete the sibling ref (`git branch -d backup/pre-<topic>` / `git tag -d backup/pre-<topic>`) and the bundle (`rm /tmp/<repo>.bundle`). Premature cleanup defeats the entire safety net. Acceptance means the user has said the rewrite is good; absence of a complaint is not acceptance.
- **Authorization scope is exact.** A user's permission to drop commit X is not permission to drop commit Y. Permission to scrub line L from a commit message is not permission to drop the commit. When an operation would extend beyond the literal authorization, ask before acting.
- **Choose the smallest tool that does the job.** For dropping a small contiguous range of commits, `git rebase --onto` is reversible and the sibling-ref backup actually protects the original. Reach for `filter-repo` only when the operation is genuinely path-based across many commits, or when message-callbacks or mailmaps are the goal.

## Verification after rewrite

Required before declaring the rewrite done:

1. **Commit count delta matches operation intent.** Capture `git rev-list <branch> --count` pre and post. A fixup+autosquash leaves the count unchanged; a `commit.skip()` drops by N. If the delta surprises, stop and investigate before continuing — including before reporting status back.
2. **File count at HEAD matches.** `git ls-files | wc -l` pre and post. A deliberate path-strip can reduce this; an unintended root-orphan reduces it catastrophically. The delta must match what the operation was for.
3. **Project verification suite produces the same result counts.** Run the project's tests, typecheck, or build pre-rewrite and capture pass/fail/skip/total. Re-run post-rewrite — counts must match. Then check the same suite at each amended historical commit: a rewrite that breaks an intermediate tree is still a broken rewrite even if HEAD passes.
4. **Topology accounts for the operation.** Whatever the operation was meant to do (drop N commits, strip a path, replace a string), the post-rewrite topology must reflect that. Unexplained extra changes — fewer commits than asked for, files missing that weren't part of the operation — are the signal to stop.

Hold the sibling backup ref and the off-repo bundle until the user explicitly accepts the rewrite. The hook's block messages already point to creating these before the operation; verification is where you confirm they are still safe to remove.
