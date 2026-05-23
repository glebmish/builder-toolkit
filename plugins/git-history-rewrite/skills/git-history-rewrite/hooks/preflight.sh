#!/usr/bin/env bash
# PreToolUse hook bundled with the git-history-rewrite skill.
# Reads tool-input JSON on stdin; exits 2 to block dangerous git history
# operations that miss required correct-form flags. Other exit codes:
#   0  — command allowed (or not in scope)
#   1  — hook script error (harness lets the command proceed, logs the error)
#
# Design note: this hook is a guard against typical LLM behavior, not an
# adversarial sandbox. It locates every `git` token in the raw command text
# (including inside `bash -c`, `sh -c`, `eval`, `$(...)`, `<(...)`, heredocs,
# and function bodies), strips git's known global options, and dispatches to
# per-subcommand gates. Trade-off: a literal `git push --force ...` appearing
# as data inside an echo/printf string will also block — see SKILL.md.

set -uo pipefail

input=$(cat)

# Performance pre-filter: skip the jq fork entirely for commands that have no
# `git` substring at all. Catches the ~60% of Bash invocations that aren't
# git-related (ls, cat, pwd, npm, ...).
[[ "$input" != *git* ]] && exit 0

extract_command() {
  if ! command -v jq >/dev/null 2>&1; then
    # shellcheck disable=SC2016  # backticks are literal text in the message
    printf 'git-history-rewrite hook requires `jq`; install it or remove the plugin.\n' >&2
    return 1
  fi
  printf '%s' "$input" | jq -r '.tool_input.command // empty'
}

command=$(extract_command) || exit 1
[[ -z "$command" ]] && exit 0

# Second pre-filter after JSON extract — the JSON might have contained "git"
# only in unrelated metadata. (Cheap; do not skip if literal "git" present.)
[[ "$command" != *git* ]] && exit 0

block() {
  printf 'git-history-rewrite: blocked by safety hook.\n\n%s\n' "$1" >&2
  exit 2
}

# ---------- leading `cd <path>` pre-pass -----------------------------------
#
# Both the orphan check and working-tree-clean check need the hook's CWD to
# match the repo the user is operating on. Walk the start of $command,
# consuming each `cd <path> <terminator>` and `KEY=val` prefix in turn,
# chdir-ing into each path. Stop once the leading content isn't a `cd` or
# env assignment. The remainder is what gets scanned for `git` tokens.
#
# We deliberately don't try to handle `cd` mid-command (inside bash -c, $(),
# etc.) — only the leading chain.

apply_leading_prefixes() {
  local s="$1"
  local prev=""
  while [[ "$s" != "$prev" ]]; do
    prev="$s"
    # Trim leading whitespace.
    s="${s#"${s%%[![:space:]]*}"}"
    # Strip a leading env-var assignment: KEY=val (no whitespace in val).
    if [[ "$s" =~ ^([A-Za-z_][A-Za-z0-9_]*)=([^[:space:]]*)[[:space:]]+(.*)$ ]]; then
      s="${BASH_REMATCH[3]}"
      continue
    fi
    # Strip a leading `cd <path>` and chdir into it. Path may be quoted; for
    # simplicity accept any non-whitespace run. Terminator can be `;`,
    # `&&`, `||`, `|`, `&`, newline, or just whitespace (`cd a cd b` is not
    # a thing in shell — terminator chars are what shells use).
    if [[ "$s" =~ ^cd[[:space:]]+([^[:space:]]+)[[:space:]]*(.*)$ ]]; then
      local path="${BASH_REMATCH[1]}"
      local rem="${BASH_REMATCH[2]}"
      # Strip a leading chain operator from $rem so the next iteration sees
      # clean text. Recognized terminators: `;`, `&&`, `||`, `|`, `&`,
      # newline. Whitespace-only terminator is fine too.
      rem="${rem#"${rem%%[![:space:]]*}"}"
      while :; do
        case "$rem" in
          '&&'*) rem="${rem#&&}" ;;
          '||'*) rem="${rem#||}" ;;
          ';'*)  rem="${rem#;}" ;;
          '|'*)  rem="${rem#|}" ;;
          '&'*)  rem="${rem#&}" ;;
          $'\n'*) rem="${rem#$'\n'}" ;;
          *) break ;;
        esac
        rem="${rem#"${rem%%[![:space:]]*}"}"
      done
      cd "$path" 2>/dev/null || true
      s="$rem"
      continue
    fi
  done
  # Set global return.
  remainder="$s"
}

# ---------- find-`git` matcher ---------------------------------------------
#
# Linear scan: for each position where the literal three-character sequence
# `git` appears, check word-boundary conditions on the chars immediately
# before and after. If satisfied, extract from there to the next chain op
# (`;`, `&&`, `||`, `|`, `&`, `\n`) or end-of-string and yield the args
# slice (everything after the `git` token itself).

# Boundary chars BEFORE the `git` token: chars that can immediately precede
# a command name in shell. start-of-string is handled by index==0.
is_pre_boundary() {
  local c="$1"
  case "$c" in
    ' '|$'\t'|$'\n'|';'|'&'|'|'|'('|'{'|'['|'<'|"'"|'"'|'$'|'='|','|'/') return 0 ;;
  esac
  return 1
}

# Boundary chars AFTER the `git` token: chars that can terminate a command
# name. end-of-string is handled at the call site.
is_post_boundary() {
  local c="$1"
  case "$c" in
    ' '|$'\t'|$'\n'|';'|'&'|'|'|')'|'}'|']'|'>'|"'"|'"') return 0 ;;
  esac
  return 1
}

# Strip git's global options from a string. After return, $git_args holds
# everything from the first non-global-option token onward (i.e. starting
# with the subcommand).
strip_git_globals() {
  local rest="$1"
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
        if [[ "$rem" == *[[:space:]]* ]]; then
          rest="${rem#*[[:space:]]}"
          rest="${rest#"${rest%%[![:space:]]*}"}"
        else
          rest=""
        fi
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
}

# Extract the args slice starting at $1 (a string that begins immediately
# after a matched `git` token) up to the next chain operator or EOS. Result
# stored in $slice.
extract_slice() {
  local s="$1"
  local i=0 n=${#s}
  while (( i < n )); do
    local c="${s:i:1}"
    case "$c" in
      $'\n'|';')
        slice="${s:0:i}"
        return 0
        ;;
      '&'|'|')
        # Both `&&` and lone `&` are terminators; same for `||` and `|`.
        slice="${s:0:i}"
        return 0
        ;;
    esac
    i=$(( i + 1 ))
  done
  slice="$s"
}

# Walk $remainder and dispatch each found git invocation to its gate.
# Returns nothing — gate functions exit 2 on block.
scan_for_git_tokens() {
  local s="$1"
  local n=${#s}
  local i=0
  while (( i + 3 <= n )); do
    if [[ "${s:i:3}" != "git" ]]; then
      i=$(( i + 1 ))
      continue
    fi
    # Check pre-boundary.
    if (( i > 0 )); then
      local prev="${s:i-1:1}"
      if ! is_pre_boundary "$prev"; then
        i=$(( i + 1 ))
        continue
      fi
    fi
    # Check post-boundary.
    if (( i + 3 < n )); then
      local next="${s:i+3:1}"
      if ! is_post_boundary "$next"; then
        i=$(( i + 1 ))
        continue
      fi
    fi
    # Match! Extract slice starting just after `git`.
    local after_git="${s:i+3}"
    # Drop the leading whitespace that follows `git ` so strip_git_globals
    # sees args directly. If the next char is a quote / paren / brace etc.,
    # that's not a real arg list — but extract_slice handles bare `git`.
    slice=""
    extract_slice "$after_git"
    local args="$slice"
    # Trim leading whitespace from args.
    args="${args#"${args%%[![:space:]]*}"}"
    # Trim a leading close-quote/paren/brace if any (post-boundary char that
    # got included by extract_slice up to chain op).
    case "$args" in
      \)*|\}*|\]*|\>*|\'*|\"*)
        args="${args:1}"
        args="${args#"${args%%[![:space:]]*}"}"
        ;;
    esac

    # Trim trailing close-quote/paren/brace/bracket so the last arg isn't
    # corrupted by the wrapper char (e.g. `git push --force"` → `git push --force`).
    # Strip until no more recognized closers remain at the end.
    local trimmed=1
    while (( trimmed )); do
      trimmed=0
      case "$args" in
        *\)|*\}|*\]|*\>|*\'|*\")
          args="${args%?}"
          # Also drop any trailing whitespace exposed.
          args="${args%"${args##*[![:space:]]}"}"
          trimmed=1
          ;;
      esac
    done

    git_args=""
    strip_git_globals "$args"
    dispatch_git "$git_args"

    # Advance past this `git` token + the slice we consumed.
    i=$(( i + 3 + ${#slice} ))
    if (( i < n )); then
      # Skip the chain-op char itself.
      local op="${s:i:1}"
      case "$op" in
        '&'|'|') i=$(( i + 1 ))
                 # Maybe `&&` or `||` — skip the second char too.
                 if (( i < n )); then
                   local op2="${s:i:1}"
                   [[ "$op2" == "$op" ]] && i=$(( i + 1 ))
                 fi
                 ;;
        *) i=$(( i + 1 )) ;;
      esac
    fi
  done
}

# Dispatch: given $1 = args starting with subcommand, route to gate.
dispatch_git() {
  local ga="$1"
  ga="${ga#"${ga%%[![:space:]]*}"}"
  local sub rest
  sub="${ga%%[[:space:]]*}"
  if [[ "$ga" == *[[:space:]]* ]]; then
    rest="${ga#*[[:space:]]}"
  else
    rest=""
  fi

  case "$sub" in
    filter-branch|filter-repo|push|reset)
      # Only gate within a git repo (after any leading cd we followed).
      git rev-parse --git-dir >/dev/null 2>&1 || return 0
      ;;
    *)
      return 0
      ;;
  esac

  case "$sub" in
    filter-branch)  check_filter_branch ;;
    filter-repo)    check_filter_repo "$rest" ;;
    push)           check_push "$rest" ;;
    reset)          check_reset "$rest" ;;
  esac
}

# ---------- per-subcommand gates -------------------------------------------
#
# Each gate takes the args *after* `git <subcommand>` (i.e. the part starting
# with subcommand-specific flags and positional args). They re-use a single
# helper `has_flag_in` operating on a space-padded version of the args.

has_flag_in() {
  # has_flag_in <padded-args> <flag>
  local padded="$1" f="$2"
  [[ "$padded" == *" ${f} "* ]] || [[ "$padded" == *" ${f}="* ]]
}

# Build a $padded string (" subcmd <args> ") for has_flag_in.
make_padded() {
  padded=" ${1} "
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

remainder=""
apply_leading_prefixes "$command"
scan_for_git_tokens "$remainder"

exit 0
