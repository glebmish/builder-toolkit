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

## Requirements

The hook is a bash script and requires `jq` on `PATH` at hook-fire time to parse the tool-input JSON. On macOS: `brew install jq`. On Debian/Ubuntu: `apt install jq`. If `jq` is missing the hook exits 1 with a clear error and the command proceeds (the harness treats hook errors as non-blocking) — install `jq` or remove the plugin if you don't want the hook running.

## Scope of the hook

The hook is a guard against typical LLM-emitted command idioms, not an adversarial sandbox. It scans the command string for every `git` token at a word boundary, follows leading `cd <path>` (and `KEY=val` env-var) prefixes to land in the right repo, strips git's known global options (`-C`, `-c`, `--git-dir=`, `--work-tree=`, `--no-pager`, etc.), and dispatches each found invocation to a per-subcommand gate. It matches both bare `git` and absolute / path-suffixed forms like `/usr/bin/git`.

Because the matcher finds `git` tokens anywhere in the command, the hook now also catches invocations that previously slipped through wrappers: inside `bash -c "..."`, `sh -c '...'`, `eval "..."`, `$(...)`, `<(...)`, in-line heredocs, function bodies (`f() { git push --force; }; f`), and lone-`&` chains. Aliases are not resolved (Claude Code's non-interactive shells don't apply user aliases anyway).

Trade-off — intentional over-blocking: a literal `git push --force ...` (or other dangerous form) appearing as data inside a quoted string also blocks. If you need to print such a string, rephrase or split it:

```bash
echo "Use git push with --force-with-lease"   # safe (no `git push --force` substring)
echo 'git push'; echo '  --force...'          # split across statements
```

Known limitation: the hook scans the command Claude is about to run, not files it references. `bash /path/to/script.sh` will not be inspected — if the script itself contains dangerous commands, the hook on the outer `bash` invocation doesn't see them. A model that wants to bypass the hook specifically can still do so by hiding the command in a file or by emitting it through indirection the hook can't statically resolve.

## What the hook enforces

- **History rewrites stay scoped to one named ref.** Other refs (sibling branches, tags, remote-tracking refs, ad-hoc backup refs) survive the operation untouched and serve as recovery points. `git filter-branch` is blocked outright — it predates safe scoping and git's own documentation now redirects to `filter-repo`. `git filter-repo` is allowed only with `--refs <ref>`; `git filter-repo --analyze` is exempted as a read-only planning aid.
- **One transformation per rewrite pass.** Path strips, message edits, commit drops, content replacements, and mailmap remaps each have their own behavior; combined in one filter-repo invocation they interact in ways that are not obvious from the manual. They run as separate passes with a verification step between each.
- **Tracked files in the working tree are clean before the rewrite begins.** Rewrites align the working tree to the new HEAD; uncommitted work to tracked files is overwritten without warning. Untracked files are left alone and don't block the operation.
- **Force pushes assume both a known remote state and a known local state.** `--force-with-lease --force-if-includes` is the only form allowed: the lease catches stale-remote clobbers, and the includes check catches stale-local clobbers where a recent fetch silently made the lease tautological.
- **Hard resets to a non-HEAD target are explicit, scoped to an unambiguous ref, and never silently orphan commits.** Bare `git reset --hard` (no target) is blocked as ambiguous. Relative refs (`HEAD~N`, `HEAD^`, `branch~N`) are blocked — off-by-one is too easy here; resolve with `git rev-parse` and pass the SHA. Unrecognized reset flags are treated as a parse failure and blocked, so the hook never has to guess what target you mean. For a non-HEAD target, the tracked working tree must also be clean (untracked files don't block). Finally, a target that would leave commits unreachable from any other ref is blocked; the block message names the orphan candidates and points to the backup-ref discipline below. `git reset --hard HEAD` and non-`--hard` resets are not gated.

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
