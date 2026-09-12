#!/usr/bin/env bash
# Cross-platform release smoke test for a built git-byline binary.
#
# Drives the real product flow against a throwaway repository: hooks,
# checkpoints, annotation, blame, rebase and partial-commit regressions,
# path edge cases, reports, and note sharing. Runs on Linux, macOS, and
# Windows (Git Bash) with no dependencies beyond Git and POSIX tools.
#
# Usage: BYLINE_BIN=/path/to/git-byline scripts/smoke.sh

set -euo pipefail

BYLINE_BIN="${BYLINE_BIN:-./git-byline}"
test -n "$BYLINE_BIN" || { echo "BYLINE_BIN is required" >&2; exit 2; }
test -x "$BYLINE_BIN" || { echo "BYLINE_BIN is not executable: $BYLINE_BIN" >&2; exit 2; }

fail() {
  echo "SMOKE FAIL: $1" >&2
  exit 1
}

step() {
  echo "== $1"
}

# portable_json_check validates compact JSON output by required keys.
# Smoke runs on runners without Python, so key greps stand in for a parser.
require() {
  # require <description> <haystack> <needle>
  case "$2" in
  *"$3"*) ;;
  *)
    fail "$1: missing '$3'"
    ;;
  esac
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT HUP INT TERM
BIN="$(cd "$(dirname "$BYLINE_BIN")" && pwd)/$(basename "$BYLINE_BIN")"

step "version"
require "version" "$("$BIN" version)" "git-byline"

REPO="$WORK/repo"
git init -q -b main "$REPO" || fail "git init"
cd "$REPO"
git config user.name "Smoke Test"
git config user.email "smoke.test@example.invalid"
git config core.autocrlf false

step "install hooks preserves user content and stays idempotent"
mkdir -p .git/hooks
printf '#!/bin/sh\necho USER-HOOK-RAN\n' >.git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
"$BIN" install-hooks --agent none --git >/dev/null || fail "install-hooks"
"$BIN" install-hooks --agent none --git >/dev/null || fail "install-hooks rerun"
grep -q "USER-HOOK-RAN" .git/hooks/pre-commit || fail "user hook lost"
grep -q "byline" .git/hooks/post-commit || fail "managed post-commit missing"
test -f .git/hooks/pre-push || fail "pre-push hook missing"

step "AI and human attribution through the commit hook"
printf 'ai alpha\nai beta\n' >app.txt
printf '%s' '{"session_id":"smoke-ai","tool_name":"Write","tool_input":{"file_path":"app.txt"},"model":"smoke-model"}' \
  | "$BIN" checkpoint claude --type ai --hook-input stdin >/dev/null || fail "ai checkpoint"
printf 'seed line\n' >seed.txt
printf '%s' '{"session_id":"smoke-human","tool_name":"Edit","tool_input":{"file_path":"seed.txt"}}' \
  | "$BIN" checkpoint droid --type human --hook-input stdin >/dev/null || fail "human checkpoint"
git add -A
git commit -qm "feat: ai and human" || fail "first commit"
BLAME="$("$BIN" blame app.txt)"
require "ai blame" "$BLAME" "ai:claude/smoke-model"
BLAME="$("$BIN" blame seed.txt)"
require "human blame" "$BLAME" "human:smoke.test"
"$BIN" verify >/dev/null || fail "verify after first commit"

step "partial commit keeps excluded provenance"
printf 'p1\n' >p1.txt
printf '%s' '{"session_id":"p1","tool_name":"Write","tool_input":{"file_path":"p1.txt"},"model":"m"}' \
  | "$BIN" checkpoint claude --type ai --hook-input stdin >/dev/null || fail "p1 checkpoint"
printf 'p2\n' >p2.txt
printf '%s' '{"session_id":"p2","tool_name":"Write","tool_input":{"file_path":"p2.txt"},"model":"m"}' \
  | "$BIN" checkpoint claude --type ai --hook-input stdin >/dev/null || fail "p2 checkpoint"
git add p1.txt
git commit -qm "feat: only p1" || fail "partial commit"
git add p2.txt
git commit -qm "feat: p2 now" || fail "second partial commit"
BLAME="$("$BIN" blame p2.txt)"
require "partial commit provenance" "$BLAME" "ai:claude/m"

step "rebase keeps attribution and the next commit annotates"
git checkout -q -b feat || fail "branch feat"
printf 'f1 line\n' >f1.txt
printf '%s' '{"session_id":"f1","tool_name":"Write","tool_input":{"file_path":"f1.txt"},"model":"m"}' \
  | "$BIN" checkpoint claude --type ai --hook-input stdin >/dev/null || fail "f1 checkpoint"
git add -A
git commit -qm "feat: f1" || fail "f1 commit"
git checkout -q main || fail "back to main"
printf 'main work\n' >mw.txt
git add -A
git commit -qm "feat: main work" || fail "main work commit"
git checkout -q feat || fail "back to feat"
git rebase -q main || fail "rebase"
BLAME="$("$BIN" blame f1.txt)"
require "rebase attribution" "$BLAME" "ai:claude/m"
printf 'f2 line\n' >f2.txt
printf '%s' '{"session_id":"f2","tool_name":"Write","tool_input":{"file_path":"f2.txt"},"model":"m"}' \
  | "$BIN" checkpoint claude --type ai --hook-input stdin >/dev/null || fail "f2 checkpoint"
git add -A
git commit -qm "feat: f2 after rebase" || fail "commit after rebase"
BLAME="$("$BIN" blame f2.txt)"
require "commit after rebase" "$BLAME" "ai:claude/m"
"$BIN" verify >/dev/null || fail "verify after rebase"
git checkout -q main || fail "back to main for merge"
git merge -q --no-ff -m "merge feat" feat || fail "merge"

step "portable special paths"
mkdir -p "dir with spaces/nested" "żółć" "dir(paren)" "dir#hash" "dir\$dollar"
printf 'space content\n' >"dir with spaces/nested/some file.txt"
printf 'unicode content\n' >"żółć/zażółć gęślą jaźń.txt"
printf 'paren content\n' >"dir(paren)/(f).txt"
printf 'hash content\n' >"dir#hash/f#ile.txt"
printf 'dollar content\n' >'dir$dollar/$file.txt'
printf '%s' '{"sessionId":"sp","toolName":"str_replace_based_edit","filePath":"dir with spaces/nested/some file.txt"}' \
  | "$BIN" checkpoint portable-cursor --type ai --hook-input stdin >/dev/null || fail "cursor checkpoint"
printf '%s' '{"sessionId":"sp2","toolName":"str_replace_based_edit","filePath":"żółć/zażółć gęślą jaźń.txt"}' \
  | "$BIN" checkpoint portable-cursor --type ai --hook-input stdin >/dev/null || fail "cursor unicode checkpoint"
printf 'a\r\nb\r\n' >crlf.txt
printf '%s' '{"sessionId":"sp3","toolName":"str_replace_based_edit","filePath":"crlf.txt"}' \
  | "$BIN" checkpoint portable-cursor --type ai --hook-input stdin >/dev/null || fail "cursor crlf checkpoint"
git add -A
git commit -qm "feat: special paths" || fail "special paths commit"
BLAME="$("$BIN" blame "dir with spaces/nested/some file.txt")"
require "space path blame" "$BLAME" "ai:cursor"
BLAME="$("$BIN" blame "żółć/zażółć gęślą jaźń.txt")"
require "unicode path blame" "$BLAME" "ai:cursor"
BLAME="$("$BIN" blame crlf.txt)"
require "crlf blame" "$BLAME" "ai:cursor"

step "cwd-relative blame from a subdirectory"
cd "dir with spaces" || fail "cd subdir"
BLAME="$("$BIN" blame "nested/some file.txt")"
require "cwd-relative blame" "$BLAME" "ai:cursor"
BLAME="$("$BIN" blame "dir with spaces/nested/some file.txt")"
require "root-relative blame" "$BLAME" "ai:cursor"
cd "$REPO" || fail "cd back"

step "reports and policy"
"$BIN" stats >/dev/null || fail "stats"
"$BIN" stats --json | grep -q '"lines"' || fail "stats json"
"$BIN" verify --deep >/dev/null || fail "verify deep"
"$BIN" check --max-untracked-percent 100 >/dev/null || fail "check pass"
if "$BIN" check --max-ai-percent 0 >/dev/null 2>&1; then
  fail "check must exit 1 when the policy is violated"
fi
"$BIN" disclosure >/dev/null || fail "disclosure json"
"$BIN" disclosure --format spdx | grep -q '"spdxId"' || fail "disclosure spdx"
"$BIN" disclosure --format cyclonedx | grep -q '"components"' || fail "disclosure cyclonedx"
"$BIN" export --format gitai | grep -q '"schema_version":"authorship/3.0.0"' || fail "export gitai"
"$BIN" export --format agent-trace | grep -q '"vcs"' || fail "export agent-trace"

step "dashboard is self-contained and refuses replacement"
"$BIN" dashboard --output "$WORK/report.html" >/dev/null || fail "dashboard"
grep -q "Content-Security-Policy" "$WORK/report.html" || fail "dashboard CSP missing"
if grep -Eq 'https?://' "$WORK/report.html"; then
  fail "dashboard references an external URL"
fi
if "$BIN" dashboard --output "$WORK/report.html" >/dev/null 2>&1; then
  fail "dashboard replaced an existing output"
fi

step "notes follow an ordinary push"
git init -q --bare "$WORK/remote.git" || fail "bare remote"
git remote add origin "$WORK/remote.git" || fail "remote add"
git push -q origin main || fail "push main"
git --git-dir="$WORK/remote.git" notes --ref=byline list >/dev/null || fail "notes list"
NOTES_COUNT="$(git --git-dir="$WORK/remote.git" notes --ref=byline list | wc -l | tr -d ' ')"
test "$NOTES_COUNT" -ge 1 || fail "notes missing on remote: $NOTES_COUNT"

step "uninstall removes managed hooks only"
"$BIN" uninstall --git >/dev/null || fail "uninstall"
grep -q "USER-HOOK-RAN" .git/hooks/pre-commit || fail "user hook deleted"
if grep -q "byline" .git/hooks/post-commit 2>/dev/null; then
  fail "managed block left in post-commit"
fi
test ! -f .git/hooks/pre-push || fail "pre-push left behind"

echo
echo "SMOKE OK"
