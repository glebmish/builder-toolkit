#!/usr/bin/env bash
# PreToolUse hook bundled with the git-history-rewrite skill.
# Reads tool-input JSON on stdin; exits 2 to block dangerous git history
# operations that miss required correct-form flags. Other exit codes:
#   0  — command allowed (or not in scope)
#   1  — hook script error (harness lets the command proceed, logs the error)

set -uo pipefail

input=$(cat)

extract_command() {
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$input" | jq -r '.tool_input.command // empty'
  elif command -v python3 >/dev/null 2>&1; then
    printf '%s' "$input" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("command",""))'
  else
    printf '%s' "$input" | python -c 'import json,sys; print(json.load(sys.stdin).get("tool_input",{}).get("command",""))'
  fi
}

command=$(extract_command) || exit 1
[[ -z "$command" ]] && exit 0

block() {
  printf 'git-history-rewrite: blocked by safety hook.\n\n%s\n' "$1" >&2
  exit 2
}

# Strip a leading "cd <path> && " so subsequent git checks operate in the
# directory where the command would have run.
stripped="$command"
if [[ "$stripped" =~ ^[[:space:]]*cd[[:space:]]+([^[:space:]\&\|\;]+)[[:space:]]*\&\&[[:space:]]*(.*)$ ]]; then
  cd "${BASH_REMATCH[1]}" 2>/dev/null || true
  stripped="${BASH_REMATCH[2]}"
fi

# Bail out cleanly if we're not in a git repo — nothing to gate.
git rev-parse --git-dir >/dev/null 2>&1 || exit 0

padded=" ${stripped} "

has_flag() {
  local f="$1"
  [[ "$padded" == *" ${f} "* ]] || [[ "$padded" == *" ${f}="* ]]
}

starts_with_git() {
  local sub="$1"
  [[ "$stripped" =~ ^[[:space:]]*git[[:space:]]+${sub}([[:space:]]|$) ]]
}

# ---- git filter-branch — block outright ------------------------------------

if starts_with_git "filter-branch"; then
  block "git filter-branch is unsafe by default and deprecated upstream.

Use git filter-repo with --refs <ref> instead. Before invoking it, create:
  - a sibling backup ref:  git branch backup/pre-<topic> HEAD
  - an off-repo bundle:    git bundle create /tmp/<repo>.bundle --all

See the git-history-rewrite skill body for the full verification step."
fi

# ---- git filter-repo -------------------------------------------------------

if starts_with_git "filter-repo"; then
  if ! has_flag "--refs"; then
    block "git filter-repo without --refs rewrites ALL refs, including any backup branches you created.

Scope to a single ref so siblings (backup/*, tags, remote-tracking) survive:
  git filter-repo --refs <ref> ..."
  fi

  if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
    block "Working tree must be clean before filter-repo — it overwrites the tree to match the new HEAD.

Commit or stash uncommitted changes first."
  fi

  # Detect distinct transformation kinds. Booleans are bash-3.2-compatible
  # (macOS system bash); associative arrays would not be.
  k_path=0; k_replace=0; k_callback=0; k_mailmap=0; k_blob_strip=0
  for f in '--path' '--path-glob' '--path-regex' '--paths-from-file' '--invert-paths'; do
    has_flag "$f" && k_path=1
  done
  for f in '--replace-text' '--replace-message'; do
    has_flag "$f" && k_replace=1
  done
  for f in '--commit-callback' '--message-callback' '--blob-callback' \
           '--name-callback' '--email-callback' '--filename-callback' \
           '--refname-callback' '--tag-callback' '--reset-callback'; do
    has_flag "$f" && k_callback=1
  done
  has_flag "--mailmap" && k_mailmap=1
  has_flag "--strip-blobs-bigger-than" && k_blob_strip=1
  has_flag "--strip-blobs-with-ids" && k_blob_strip=1

  total=$(( k_path + k_replace + k_callback + k_mailmap + k_blob_strip ))
  if (( total > 1 )); then
    names=""
    (( k_path ))       && names="${names} path"
    (( k_replace ))    && names="${names} replace"
    (( k_callback ))   && names="${names} callback"
    (( k_mailmap ))    && names="${names} mailmap"
    (( k_blob_strip )) && names="${names} blob_strip"
    block "filter-repo with multiple transformation kinds (${names# }) in one pass interacts unpredictably — one kind's output can become another's input in an order you did not intend.

Run as separate filter-repo invocations with a verification step between each."
  fi
fi

# ---- git push --force family ----------------------------------------------

if starts_with_git "push"; then
  has_force=0
  if has_flag "--force" || has_flag "-f"; then
    has_force=1
  fi
  # --force-with-lease may carry an =<ref>:<sha> suffix
  if [[ "$padded" == *" --force-with-lease"* ]]; then
    has_force=1
  fi

  if (( has_force == 1 )) && ! has_flag "--force-if-includes"; then
    block "Force-pushing without --force-if-includes allows clobbering remote state you haven't fetched.

Use the strictest safe form:
  git push --force-with-lease --force-if-includes <remote> <ref>"
  fi
fi

# ---- git reset --hard ------------------------------------------------------

if starts_with_git "reset"; then
  if ! has_flag "--hard"; then
    exit 0
  fi

  # Strip "git reset" and known reset-side flags so the first remaining token
  # is the target.
  args="$stripped"
  args="${args#"${args%%[![:space:]]*}"}"   # ltrim
  args="${args#git}"
  args="${args#"${args%%[![:space:]]*}"}"
  args="${args#reset}"
  args="${args#"${args%%[![:space:]]*}"}"

  # Remove flags we recognize; leave the target and any pathspecs.
  for f in '--hard' '--quiet' '-q' '--no-refresh' '--refresh' '--pathspec-from-file' '--pathspec-file-nul'; do
    args="${args// ${f}=*[[:space:]]/ }"
    args="${args// ${f} / }"
    [[ "$args" == "${f}" ]] && args=""
    [[ "$args" == "${f} "* ]] && args="${args#${f} }"
    [[ "$args" == *" ${f}" ]] && args="${args% ${f}}"
  done

  # Trim, then take the first token as the target.
  args="$(echo "$args" | awk '{$1=$1; print}')"
  target="${args%% *}"

  if [[ -z "$target" ]]; then
    block "Bare 'git reset --hard' is ambiguous about the target.

Specify it explicitly:
  git reset --hard HEAD"
  fi

  # Reject relative refs (HEAD~N, HEAD^, branch~N, branch^N).
  if [[ "$target" == *"~"* ]] || [[ "$target" == *"^"* ]]; then
    resolved=$(git rev-parse "$target" 2>/dev/null || echo "<unresolved>")
    block "Reset target must be a SHA or an unambiguous named ref, not a relative ref like '$target' — off-by-one is too easy here.

Resolve and name explicitly:
  git rev-parse $target   # → $resolved
  git reset --hard $resolved"
  fi

  # target == HEAD: allow. The whole point of 'git reset --hard HEAD' is to
  # wipe the working tree to match HEAD, so working-tree-clean would defeat it,
  # and no commits can be orphaned.
  if [[ "$target" == "HEAD" ]]; then
    exit 0
  fi

  # Non-HEAD target: working tree must be clean.
  if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
    block "Working tree must be clean before reset --hard to a non-HEAD target — it overwrites the tree.

Commit or stash uncommitted changes first."
  fi

  # Non-HEAD target: orphan check.
  # A commit is orphaned iff reachable from current HEAD but not from <target>
  # and not from any other branch/tag/remote-tracking ref.
  current_branch=$(git symbolic-ref --short HEAD 2>/dev/null || echo "")
  excl_args=("^${target}")
  while IFS= read -r ref; do
    [[ -z "$ref" ]] && continue
    [[ -n "$current_branch" && "$ref" == "refs/heads/${current_branch}" ]] && continue
    excl_args+=("^${ref}")
  done < <(git for-each-ref --format='%(refname)' refs/heads refs/tags refs/remotes 2>/dev/null)

  orphans=$(git rev-list HEAD "${excl_args[@]}" 2>/dev/null | head -5 || true)

  if [[ -n "$orphans" ]]; then
    summary=$(printf '%s\n' "$orphans" | head -3 | while read -r sha; do
      git log -1 --format='  %h %s' "$sha" 2>/dev/null
    done)
    block "These commits would become unreachable from any ref after the reset:

${summary}

Create a sibling backup ref first so they stay reachable:
  git branch backup/pre-reset HEAD
  # then re-run the reset"
  fi
fi

exit 0
