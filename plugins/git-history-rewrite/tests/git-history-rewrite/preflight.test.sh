#!/usr/bin/env bash
# Tests for the git-history-rewrite skill's PreToolUse hook (preflight.sh).
# Runs each matrix case against the hook script, asserts exit code, and
# (where relevant) asserts a substring of the stderr block message.
#
# Each test runs inside an isolated temp git repo so the host repo state
# does not leak into the assertions.
#
# Usage: ./preflight.test.sh
# Exit: 0 if all tests pass, 1 if any fail.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="${SCRIPT_DIR}/../../skills/git-history-rewrite/hooks/preflight.sh"

if [[ ! -x "$HOOK" ]]; then
  echo "FAIL: hook script not found or not executable: $HOOK" >&2
  exit 1
fi

PASS=0
FAIL=0
FAILED_CASES=()

# Collect every temp dir created by mk_repo so we can clean them up on exit.
TMP_DIRS=()
cleanup_tmp_dirs() {
  local d
  for d in "${TMP_DIRS[@]:-}"; do
    [[ -n "$d" && -d "$d" ]] && rm -rf "$d"
  done
}
trap cleanup_tmp_dirs EXIT

mk_repo() {
  # Create an empty, clean git repo in a fresh temp dir and cd into it.
  # Echoes the repo path on stdout.
  local d
  d=$(mktemp -d)
  TMP_DIRS+=("$d")
  cd "$d"
  git init -q -b main
  git config user.email "test@test"
  git config user.name "Test"
  echo seed > seed
  git add seed
  git commit -q -m "seed"
  echo "$d"
}

run_case() {
  # run_case <label> <command-string> <expected-exit> [<stderr-substring>]
  local label="$1" cmd="$2" expected="$3" needle="${4:-}"
  local input err rc
  input=$(jq -nc --arg c "$cmd" '{tool_name:"Bash",tool_input:{command:$c}}')
  err=$(printf '%s' "$input" | "$HOOK" 2>&1 >/dev/null)
  rc=$?
  if [[ "$rc" != "$expected" ]]; then
    printf '  FAIL  %s — exit %s (expected %s)\n' "$label" "$rc" "$expected"
    printf '          stderr: %s\n' "${err:0:200}"
    FAIL=$((FAIL + 1))
    FAILED_CASES+=("$label")
    return
  fi
  if [[ -n "$needle" ]] && [[ "$err" != *"$needle"* ]]; then
    printf '  FAIL  %s — stderr missing substring %q\n' "$label" "$needle"
    printf '          stderr: %s\n' "${err:0:200}"
    FAIL=$((FAIL + 1))
    FAILED_CASES+=("$label")
    return
  fi
  printf '  PASS  %s\n' "$label"
  PASS=$((PASS + 1))
}

# -- Section 1: filter-branch -------------------------------------------------
echo "=== filter-branch (block outright) ==="
mk_repo >/dev/null
run_case "filter-branch any args" \
  "git filter-branch --tree-filter true HEAD" 2 "deprecated upstream"

# -- Section 2: filter-repo ---------------------------------------------------
echo "=== filter-repo ==="
mk_repo >/dev/null
run_case "missing --refs" \
  "git filter-repo --path foo" 2 "rewrites ALL refs"

# Clean tree, scoped, single transformation: should pass.
# (The hook still runs `git status --porcelain` to check clean state.)
mk_repo >/dev/null
run_case "scoped + single kind + clean tree" \
  "git filter-repo --refs main --path foo" 0

# Dirty tracked file should block. (Untracked files are tested separately below.)
mk_repo >/dev/null
echo dirty >> seed
run_case "scoped + single kind + dirty tracked file" \
  "git filter-repo --refs main --path foo" 2 "Working tree must be clean"

# Multi-kind: path + replace-text → two kinds.
mk_repo >/dev/null
run_case "scoped + multiple transformation kinds" \
  "git filter-repo --refs main --path foo --replace-text r.txt" 2 "multiple transformation kinds"

# Multiple --path flags = one kind: should pass.
mk_repo >/dev/null
run_case "scoped + multiple --path (single kind)" \
  "git filter-repo --refs main --path a --path b --path c" 0

# Callback alone = one kind.
mk_repo >/dev/null
run_case "scoped + commit-callback (single kind)" \
  "git filter-repo --refs main --commit-callback x" 0

# --analyze does not rewrite history; should never be blocked for missing --refs.
mk_repo >/dev/null
run_case "filter-repo --analyze (read-only)" \
  "git filter-repo --analyze" 0

mk_repo >/dev/null
run_case "filter-repo --analyze with extra args" \
  "git filter-repo --analyze --target /tmp/out" 0

# --analyze on a dirty tree is also fine — it doesn't touch the tree.
mk_repo >/dev/null
echo dirty > tracked_dirty
git add tracked_dirty
git commit -q -m "tracked"
echo modified >> tracked_dirty
run_case "filter-repo --analyze on dirty tree" \
  "git filter-repo --analyze" 0

# Untracked files alone should NOT block filter-repo.
mk_repo >/dev/null
echo unt > untracked_only
run_case "filter-repo with untracked file only (tracked clean)" \
  "git filter-repo --refs main --path foo" 0

# -- Section 3: push --force family ------------------------------------------
echo "=== push --force family ==="
mk_repo >/dev/null
run_case "plain --force (no includes)" \
  "git push --force origin main" 2 "force-if-includes"
run_case "short -f (no includes)" \
  "git push -f origin main" 2 "force-if-includes"
run_case "lease only (no includes)" \
  "git push --force-with-lease origin main" 2 "force-if-includes"
run_case "lease with =ref:sha + no includes" \
  "git push --force-with-lease=main:abc123 origin main" 2 "force-if-includes"
run_case "lease + includes" \
  "git push --force-with-lease --force-if-includes origin main" 0
run_case "force + includes (no lease keyword)" \
  "git push --force --force-if-includes origin main" 2 "force-with-lease"
run_case "plain push (no force)" \
  "git push origin main" 0

# -- Section 4: reset --hard --------------------------------------------------
echo "=== reset --hard ==="
mk_repo >/dev/null
run_case "bare reset --hard" \
  "git reset --hard" 2 "ambiguous about the target"
run_case "reset --hard HEAD" \
  "git reset --hard HEAD" 0
run_case "reset --hard HEAD~1 (relative)" \
  "git reset --hard HEAD~1" 2 "relative ref"
run_case "reset --hard HEAD^ (relative)" \
  "git reset --hard HEAD^" 2 "relative ref"
run_case "reset --soft HEAD~1 (no --hard)" \
  "git reset --soft HEAD~1" 0
run_case "reset (implicit --mixed)" \
  "git reset HEAD~1" 0

# Orphan detection: requires non-trivial repo state.
# Case A: sibling ref protects the would-be-orphan → allow.
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
git branch sibling   # sibling at C (current HEAD)
run_case "reset --hard <B> with sibling protecting C" \
  "git reset --hard $B" 0

# Case B: sibling exists but at B (not C) → C still orphans.
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
git branch sibling "$B"
run_case "reset --hard <B> sibling at B (C orphans)" \
  "git reset --hard $B" 2 "unreachable from any ref"

# Case C: no sibling → orphans, block.
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
run_case "reset --hard <B> no sibling (orphans)" \
  "git reset --hard $B" 2 "unreachable from any ref"

# Case D: target is a named ref where commits also reachable via remote-tracking.
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
git update-ref refs/remotes/origin/saved HEAD  # simulate fetched remote ref
run_case "reset --hard <B> remote-tracking covers C" \
  "git reset --hard $B" 0

# `reset --hard -- <pathspec>` → "--" means target defaults to HEAD, no orphans.
mk_repo >/dev/null
echo p > somefile; git add somefile; git commit -q -m "P"
run_case "reset --hard -- pathspec (target defaults to HEAD)" \
  "git reset --hard -- somefile" 0

# Untracked files in working tree must NOT block reset --hard <non-HEAD>.
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
git branch sibling   # sibling at C protects orphans
echo unt > untracked_only
run_case "reset --hard <B> with untracked file only (sibling protects)" \
  "git reset --hard $B" 0

# Unrecognized reset flag must be a parse failure (block, ask for explicit invocation).
mk_repo >/dev/null
run_case "reset --hard with unknown flag" \
  "git reset --hard --bogus-flag HEAD" 2 "explicit"

# Reset --hard <non-HEAD> with uncommitted tracked changes must block, and the
# block message must say "tracked files".
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
git branch sibling   # protects orphans so the working-tree check is the one we hit
echo modified >> seed   # modify a tracked file
run_case "reset --hard <B> with dirty tracked file" \
  "git reset --hard $B" 2 "tracked files"

# -- Section 5: out-of-scope and edge cases ----------------------------------
echo "=== out-of-scope and edge cases ==="
mk_repo >/dev/null
run_case "git commit (not in matcher)" \
  "git commit -m hi" 0
run_case "git status (not in matcher)" \
  "git status" 0

# Command starts with `cd <path> &&` — hook should follow into the path.
# If the path isn't a git repo, the hook exits 0 (bails out).
run_case "cd into /tmp + git push (not a repo there)" \
  "cd /tmp && git push origin main" 0

# -- Section 6: bypass matrix -------------------------------------------------
echo "=== bypass matrix (Critical 1) ==="

# Chaining operators — each segment must be inspected independently.
mk_repo >/dev/null
run_case "chain && — git status && git push --force" \
  "git status && git push --force origin main" 2 "force-with-lease"
run_case "chain ; — git status ; git push --force" \
  "git status ; git push --force origin main" 2 "force-with-lease"
run_case "chain || — git status || git push --force" \
  "git status || git push --force origin main" 2 "force-with-lease"
run_case "chain | — true | git push --force" \
  "true | git push --force origin main" 2 "force-with-lease"
run_case "chain newline — git status<NL>git push --force" \
  "$(printf 'git status\ngit push --force origin main')" 2 "force-with-lease"

# Subshell wrapper.
run_case "subshell — (git push --force origin main)" \
  "(git push --force origin main)" 2 "force-with-lease"

# Leading env-var assignments.
run_case "env var prefix — GIT_DIR=.git git filter-branch ..." \
  "GIT_DIR=.git git filter-branch --tree-filter true HEAD" 2 "deprecated upstream"
run_case "two env var prefixes — A=1 B=2 git push --force" \
  "A=1 B=2 git push --force origin main" 2 "force-with-lease"

# git global options between `git` and the subcommand.
mk_repo >/dev/null
run_case "git -C . reset --hard <sha> orphans" \
  "git -C . reset --hard $(git rev-parse HEAD)" 0  # HEAD ~ current → exit 0 anyway
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
run_case "git -C . reset --hard <B> no sibling (orphans)" \
  "git -C . reset --hard $B" 2 "unreachable from any ref"

mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
run_case "git -c user.email=x reset --hard <B> orphans" \
  "git -c user.email=x reset --hard $B" 2 "unreachable from any ref"

mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
run_case "git --no-pager reset --hard <B> orphans" \
  "git --no-pager reset --hard $B" 2 "unreachable from any ref"

mk_repo >/dev/null
run_case "git --no-pager filter-branch ..." \
  "git --no-pager filter-branch --tree-filter true HEAD" 2 "deprecated upstream"

# Absolute / path-suffixed git binary.
mk_repo >/dev/null
run_case "absolute /usr/bin/git filter-branch" \
  "/usr/bin/git filter-branch --tree-filter true HEAD" 2 "deprecated upstream"
run_case "absolute /opt/homebrew/bin/git push --force" \
  "/opt/homebrew/bin/git push --force origin main" 2 "force-with-lease"

# `cd <path> ;` (semicolon, not &&) — should follow cd and then gate.
mk_repo >/dev/null
REPO_FOR_CD=$(pwd)
mk_repo >/dev/null
echo b > b; git add b; git commit -q -m "B"; B=$(git rev-parse HEAD)
echo c > c; git add c; git commit -q -m "C"
REPO_WITH_ORPHANS=$(pwd)
run_case "cd <repo>; git reset --hard <B> orphans" \
  "cd $REPO_WITH_ORPHANS ; git reset --hard $B" 2 "unreachable from any ref"

# cd then newline.
run_case "cd <repo><NL>git reset --hard <B> orphans" \
  "$(printf 'cd %s\ngit reset --hard %s' "$REPO_WITH_ORPHANS" "$B")" 2 "unreachable from any ref"

# cd a && cd b && git ... should land in the FINAL directory.
mk_repo >/dev/null
INTERMEDIATE=$(pwd)
run_case "cd <intermediate> && cd <repo-with-orphans> && git reset --hard <B>" \
  "cd $INTERMEDIATE && cd $REPO_WITH_ORPHANS && git reset --hard $B" 2 "unreachable from any ref"

# Bypass attempt: env var prefix on a force push should still trip the lease gate.
run_case "env var prefix on push --force" \
  "FOO=bar git push --force origin main" 2 "force-with-lease"

# -- Section 7: jq-missing error path ----------------------------------------
echo "=== jq-missing error path ==="

# Run the hook with a PATH that has bash + standard tools but no jq.
# Expect exit 1 and the explicit error message.
mk_repo >/dev/null
NOJQ_DIR=$(mktemp -d)
TMP_DIRS+=("$NOJQ_DIR")
for b in bash sh cat git env printf; do
  src=$(command -v "$b") || continue
  ln -sf "$src" "$NOJQ_DIR/$b"
done
# Make sure jq is NOT in the sandbox.
rm -f "$NOJQ_DIR/jq"

label="hook errors clearly when jq missing"
input=$(printf '{"tool_input":{"command":"git status"}}')
err=$(printf '%s' "$input" | PATH="$NOJQ_DIR" "$HOOK" 2>&1 >/dev/null)
rc=$?
if [[ "$rc" == "1" ]] && [[ "$err" == *"requires \`jq\`"* ]]; then
  printf '  PASS  %s\n' "$label"
  PASS=$((PASS + 1))
else
  printf '  FAIL  %s — exit %s, stderr: %s\n' "$label" "$rc" "${err:0:200}"
  FAIL=$((FAIL + 1))
  FAILED_CASES+=("$label")
fi

# -- Summary ------------------------------------------------------------------
echo
echo "============================================"
echo "Tests passed: $PASS"
echo "Tests failed: $FAIL"
if (( FAIL > 0 )); then
  echo "Failed cases:"
  for c in "${FAILED_CASES[@]}"; do
    echo "  - $c"
  done
  exit 1
fi
echo "All tests passed."
