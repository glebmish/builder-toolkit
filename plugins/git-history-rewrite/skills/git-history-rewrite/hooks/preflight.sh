#!/usr/bin/env bash
# PreToolUse hook bundled with the git-history-rewrite skill.
# Reads tool-input JSON on stdin; exits 2 to block dangerous git history
# operations that miss required correct-form flags. Other exit codes:
#   0  — command allowed (or not in scope)
#   1  — hook script error (harness lets the command proceed, logs the error)
#
# Design note: this hook is a guard against typical LLM idioms, not an
# adversarial sandbox. It splits on `;`, `&&`, `||`, `|`, and newlines, and
# strips common prefixes (subshell `(`, env-var assignments, `cd <path>`,
# git global options, absolute paths to git). It does NOT parse `bash -c
# '<wrapped>'` or arbitrarily quoted shell payloads — see SKILL.md.

set -uo pipefail

input=$(cat)

extract_command() {
  if ! command -v jq >/dev/null 2>&1; then
    printf 'git-history-rewrite hook requires `jq`; install it or remove the plugin.\n' >&2
    return 1
  fi
  printf '%s' "$input" | jq -r '.tool_input.command // empty'
}

command=$(extract_command) || exit 1
[[ -z "$command" ]] && exit 0

block() {
  printf 'git-history-rewrite: blocked by safety hook.\n\n%s\n' "$1" >&2
  exit 2
}

# ---------- segment normalization ------------------------------------------
#
# Split the raw command on `;`, `&&`, `||`, `|`, and newlines. For each
# segment, strip:
#   - leading whitespace
#   - leading subshell `(`
#   - leading env-var assignments (KEY=val ...)
#   - leading `cd <path>` (chdir-ing into <path> per cd encountered)
# until what's left either starts with `git` (or `/.../git`) or doesn't.
# Then strip git's known global options to expose the subcommand.

# Replace each of `;`, `&&`, `||`, `|`, newline with a single record separator
# so we can iterate with `while read`. Use \x1f (unit separator) which is
# extremely unlikely to occur in command text.
split_segments() {
  local raw="$1"
  # Order matters: turn `&&`/`||` into the separator first (two chars) before
  # collapsing single `|`/`;`/newline.
  raw="${raw//$'\n'/$'\x1f'}"
  raw="${raw//&&/$'\x1f'}"
  raw="${raw//||/$'\x1f'}"
  raw="${raw//;/$'\x1f'}"
  raw="${raw//|/$'\x1f'}"
  printf '%s' "$raw"
}

# Strip leading whitespace, subshell `(`, env-var assignments, and any chain
# of `cd <path>` from a single segment. cd-into the path each time. After
# returning, $clean holds the cleaned segment.
#
# Bash 3.2 has no "return string from function" mechanism so we set a global.
clean_segment() {
  local s="$1"
  local prev=""
  # Loop until nothing more strips off; lets multiple prefixes (env + paren +
  # cd, in any order) compose. We also handle `cd a && cd b` *within* a single
  # segment in the unlikely case the splitter left them together (it won't,
  # since && is itself a segment boundary — but be defensive).
  while [[ "$s" != "$prev" ]]; do
    prev="$s"
    # Trim leading whitespace.
    s="${s#"${s%%[![:space:]]*}"}"
    # Strip leading subshell parens (one or more).
    while [[ "$s" == "("* ]]; do
      s="${s#(}"
      s="${s#"${s%%[![:space:]]*}"}"
    done
    # Strip leading env-var assignments: KEY=val (no whitespace in val).
    # Repeat until none match.
    while [[ "$s" =~ ^([A-Za-z_][A-Za-z0-9_]*)=([^[:space:]]*)[[:space:]]+(.*)$ ]]; do
      s="${BASH_REMATCH[3]}"
    done
    # Strip leading `cd <path>` and chdir into it. Path may be quoted; for
    # simplicity accept any non-whitespace run.
    if [[ "$s" =~ ^cd[[:space:]]+([^[:space:]]+)[[:space:]]*(.*)$ ]]; then
      cd "${BASH_REMATCH[1]}" 2>/dev/null || true
      s="${BASH_REMATCH[2]}"
    fi
  done
  clean="$s"
}

# After cleaning, decide if this segment is a git invocation. Returns 0 (yes)
# and sets $git_args to the part *after* `git` (with global options stripped).
# Returns 1 (no) if this segment isn't a git command.
is_git_invocation() {
  local s="$1"
  local cmd0 rest

  # Take the first token.
  cmd0="${s%%[[:space:]]*}"
  if [[ "$s" == *[[:space:]]* ]]; then
    rest="${s#*[[:space:]]}"
  else
    rest=""
  fi

  # Match `git` exactly OR a path ending in `/git`.
  if [[ "$cmd0" == "git" ]] || [[ "$cmd0" == */git ]]; then
    :
  else
    return 1
  fi

  # Strip git global options: `-C <dir>`, `-c <k=v>`, `--git-dir=<path>`,
  # `--work-tree=<path>`, `--namespace=<n>`, `--bare`, `--no-pager`,
  # `--exec-path[=<path>]`, `--literal-pathspecs`, `--no-replace-objects`.
  rest="${rest#"${rest%%[![:space:]]*}"}"
  while [[ -n "$rest" ]]; do
    local tok rem
    tok="${rest%%[[:space:]]*}"
    if [[ "$rest" == *[[:space:]]* ]]; then
      rem="${rest#*[[:space:]]}"
      rem="${rem#"${rem%%[![:space:]]*}"}"
    else
      rem=""
    fi
    case "$tok" in
      -C|-c)
        # Two-token form: consume the value too.
        if [[ -z "$rem" ]]; then
          rest=""
          break
        fi
        local val
        val="${rem%%[[:space:]]*}"
        if [[ "$rem" == *[[:space:]]* ]]; then
          rest="${rem#*[[:space:]]}"
          rest="${rest#"${rest%%[![:space:]]*}"}"
        else
          rest=""
        fi
        # val itself is discarded; we just needed to consume it.
        : "$val"
        ;;
      --git-dir=*|--work-tree=*|--namespace=*|--exec-path|--exec-path=*|\
      --bare|--no-pager|--literal-pathspecs|--no-replace-objects)
        rest="$rem"
        ;;
      *)
        break
        ;;
    esac
  done

  git_args="$rest"
  return 0
}

# ---------- per-subcommand gates -------------------------------------------
#
# Each gate takes the args *after* `git <subcommand>` (i.e. the part starting
# with subcommand-specific flags and positional args). They re-use a single
# helper `has_flag` operating on a space-padded version of the args.

has_flag_in() {
  # has_flag_in <padded-args> <flag>
  local padded="$1" f="$2"
  [[ "$padded" == *" ${f} "* ]] || [[ "$padded" == *" ${f}="* ]]
}

# Build a $padded string (" subcmd <args> ") for has_flag_in.
make_padded() {
  padded=" ${1} "
}

starts_with_subcmd() {
  # starts_with_subcmd <git_args> <subcmd>
  local args="$1" sub="$2"
  args="${args#"${args%%[![:space:]]*}"}"
  [[ "$args" == "$sub" ]] || [[ "$args" == "$sub "* ]] || [[ "$args" == "$sub"$'\t'* ]]
}

check_filter_branch() {
  block "git filter-branch is unsafe by default and deprecated upstream.

Use git filter-repo with --refs <ref> instead. Before invoking it, create:
  - a sibling backup ref:  git branch backup/pre-<topic> HEAD
  - an off-repo bundle:    git bundle create /tmp/<repo>.bundle --all

See the git-history-rewrite skill body for the full verification step."
}

check_filter_repo() {
  local args="$1"
  make_padded "$args"

  # --analyze is read-only; never blocks.
  if has_flag_in "$padded" "--analyze"; then
    return 0
  fi

  if ! has_flag_in "$padded" "--refs"; then
    block "git filter-repo without --refs rewrites ALL refs, including any backup branches you created.

Scope to a single ref so siblings (backup/*, tags, remote-tracking) survive:
  git filter-repo --refs <ref> ..."
  fi

  if [[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]]; then
    block "Working tree must be clean before filter-repo — it overwrites the tree to match the new HEAD.

Commit or stash uncommitted changes to tracked files first."
  fi

  # Distinct transformation kinds. Booleans are bash-3.2-compatible.
  local k_path=0 k_replace=0 k_callback=0 k_mailmap=0 k_blob_strip=0
  local f
  for f in '--path' '--path-glob' '--path-regex' '--paths-from-file' '--invert-paths'; do
    has_flag_in "$padded" "$f" && k_path=1
  done
  for f in '--replace-text' '--replace-message'; do
    has_flag_in "$padded" "$f" && k_replace=1
  done
  for f in '--commit-callback' '--message-callback' '--blob-callback' \
           '--name-callback' '--email-callback' '--filename-callback' \
           '--refname-callback' '--tag-callback' '--reset-callback'; do
    has_flag_in "$padded" "$f" && k_callback=1
  done
  has_flag_in "$padded" "--mailmap" && k_mailmap=1
  has_flag_in "$padded" "--strip-blobs-bigger-than" && k_blob_strip=1
  has_flag_in "$padded" "--strip-blobs-with-ids" && k_blob_strip=1

  local total=$(( k_path + k_replace + k_callback + k_mailmap + k_blob_strip ))
  if (( total > 1 )); then
    local names=""
    (( k_path ))       && names="${names} path"
    (( k_replace ))    && names="${names} replace"
    (( k_callback ))   && names="${names} callback"
    (( k_mailmap ))    && names="${names} mailmap"
    (( k_blob_strip )) && names="${names} blob_strip"
    block "filter-repo with multiple transformation kinds (${names# }) in one pass interacts unpredictably — one kind's output can become another's input in an order you did not intend.

Run as separate filter-repo invocations with a verification step between each."
  fi
}

check_push() {
  local args="$1"
  make_padded "$args"

  local has_force=0
  local has_lease=0
  if has_flag_in "$padded" "--force" || has_flag_in "$padded" "-f"; then
    has_force=1
  fi
  if [[ "$padded" == *" --force-with-lease"* ]]; then
    has_force=1
    has_lease=1
  fi

  if (( has_force == 0 )); then
    return 0
  fi

  # Tighten: require --force-with-lease whenever any --force/-f is present.
  if (( has_lease == 0 )); then
    block "Force-pushing without --force-with-lease may clobber remote work you haven't fetched. Plain --force / -f is never the right form.

Use the strictest safe form:
  git push --force-with-lease --force-if-includes <remote> <ref>"
  fi

  if ! has_flag_in "$padded" "--force-if-includes"; then
    block "Force-pushing without --force-if-includes lets a recent fetch silently make the lease tautological.

Use the strictest safe form:
  git push --force-with-lease --force-if-includes <remote> <ref>"
  fi
}

# Allowlist of reset-side flags we know how to skip over. Anything we don't
# recognize is treated as a parse failure — block and ask the user to invoke
# explicitly. (Long-form flags accept `--flag=value` too; two-token forms are
# listed here but the only reset flags that take values are
# --pathspec-from-file and the deprecated -- separator.)
RESET_ALLOWED_FLAGS=(
  '--hard' '--soft' '--mixed' '--keep' '--merge'
  '--quiet' '-q' '--no-refresh' '--refresh'
  '--pathspec-from-file' '--pathspec-file-nul'
  '--recurse-submodules' '--no-recurse-submodules'
)

is_reset_allowed_flag() {
  local t="$1" f
  for f in "${RESET_ALLOWED_FLAGS[@]}"; do
    [[ "$t" == "$f" ]] && return 0
    [[ "$t" == "$f="* ]] && return 0
  done
  return 1
}

check_reset() {
  local args="$1"
  make_padded "$args"

  if ! has_flag_in "$padded" "--hard"; then
    return 0
  fi

  # Tokenize args, skip allowed reset flags; first unrecognized non-flag token
  # is the target. An unknown flag is a parse failure → block.
  # `read -ra` works on bash 3.2.
  # shellcheck disable=SC2206
  local tokens=( $args )
  local target=""
  local i=0
  local consume_value=0
  while (( i < ${#tokens[@]} )); do
    local t="${tokens[$i]}"
    if (( consume_value )); then
      consume_value=0
      i=$(( i + 1 ))
      continue
    fi
    if [[ "$t" == "--" ]]; then
      # `git reset --hard -- <pathspec>` — target defaults to HEAD.
      target="HEAD"
      break
    fi
    if [[ "$t" == --* || "$t" == -* ]]; then
      if is_reset_allowed_flag "$t"; then
        # --pathspec-from-file takes a value in the next token (no = form
        # is fine, with = form is handled by the allowlist match).
        if [[ "$t" == "--pathspec-from-file" ]]; then
          consume_value=1
        fi
        i=$(( i + 1 ))
        continue
      fi
      block "Unrecognized 'git reset' flag '$t' in this invocation. The hook can't be sure what target you mean.

Re-invoke explicitly with only the flags you need, e.g.:
  git reset --hard <sha>"
    fi
    target="$t"
    break
  done

  if [[ -z "$target" ]]; then
    block "Bare 'git reset --hard' is ambiguous about the target.

Specify it explicitly:
  git reset --hard HEAD"
  fi

  # Reject relative refs (HEAD~N, HEAD^, branch~N, branch^N).
  if [[ "$target" == *"~"* ]] || [[ "$target" == *"^"* ]]; then
    local resolved
    resolved=$(git rev-parse "$target" 2>/dev/null || echo "<unresolved>")
    block "Reset target must be a SHA or an unambiguous named ref, not a relative ref like '$target' — off-by-one is too easy here.

Resolve and name explicitly:
  git rev-parse $target   # → $resolved
  git reset --hard $resolved"
  fi

  # target == HEAD: allow.
  if [[ "$target" == "HEAD" ]]; then
    return 0
  fi

  # Non-HEAD target: tracked working tree must be clean.
  if [[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]]; then
    block "Working tree must be clean before reset --hard to a non-HEAD target — it overwrites tracked files.

Commit or stash uncommitted changes to tracked files first."
  fi

  # Orphan check.
  local current_branch
  current_branch=$(git symbolic-ref --short HEAD 2>/dev/null || echo "")
  local excl_args=("^${target}")
  while IFS= read -r ref; do
    [[ -z "$ref" ]] && continue
    [[ -n "$current_branch" && "$ref" == "refs/heads/${current_branch}" ]] && continue
    excl_args+=("^${ref}")
  done < <(git for-each-ref --format='%(refname)' refs/heads refs/tags refs/remotes 2>/dev/null)

  local orphans
  orphans=$(git rev-list HEAD "${excl_args[@]}" 2>/dev/null | head -5 || true)

  if [[ -n "$orphans" ]]; then
    local summary
    summary=$(printf '%s\n' "$orphans" | head -3 | while read -r sha; do
      git log -1 --format='  %h %s' "$sha" 2>/dev/null
    done)
    block "These commits would become unreachable from any ref after the reset:

${summary}

Create a sibling backup ref first so they stay reachable:
  git branch backup/pre-reset HEAD
  # then re-run the reset"
  fi
}

# ---------- main driver ----------------------------------------------------

split=$(split_segments "$command")

# Iterate segments. bash 3.2: read can't take a here-string with NUL, so we
# substitute the unit separator with newlines and use a simple while-read.
IFS=$'\x1f'
# shellcheck disable=SC2206
segments=( $split )
unset IFS

for seg in "${segments[@]}"; do
  clean=""
  clean_segment "$seg"
  [[ -z "$clean" ]] && continue

  git_args=""
  if ! is_git_invocation "$clean"; then
    continue
  fi

  # Only gate within a git repo (after any cd we followed).
  git rev-parse --git-dir >/dev/null 2>&1 || continue

  # First token of git_args is the subcommand.
  ga="${git_args#"${git_args%%[![:space:]]*}"}"
  sub="${ga%%[[:space:]]*}"
  if [[ "$ga" == *[[:space:]]* ]]; then
    rest="${ga#*[[:space:]]}"
  else
    rest=""
  fi

  case "$sub" in
    filter-branch)  check_filter_branch ;;
    filter-repo)    check_filter_repo "$rest" ;;
    push)           check_push "$rest" ;;
    reset)          check_reset "$rest" ;;
    *)              : ;;  # not in scope
  esac
done

exit 0
