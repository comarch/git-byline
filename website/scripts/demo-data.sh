#!/usr/bin/env bash
# Regenerate src/data/demo.json from real git-byline output.
#
# The landing page charts and terminal panels read this file. It is built
# the same way as the README media: compile the binary from this
# repository, build the demo repository from real hook events with
# docs/assets/demo/build-demo-repo.sh, then record what the commands print.
# No number or line in the file is typed by hand.
#
# Requirements: go, git, jq, python3.
# Usage: scripts/demo-data.sh [work-directory]
set -euo pipefail

site_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_root="$(cd "$site_root/.." && pwd)"
work="${1:-${TMPDIR:-/tmp}/git-byline-site-data}"
out="$site_root/src/data/demo.json"
range="HEAD~6..HEAD"

for tool in go git jq python3; do
  command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 1; }
done

rm -rf "$work"
mkdir -p "$work"
work="$(cd "$work" && pwd)"

echo "building binary"
(cd "$repo_root" && CGO_ENABLED=0 go build -trimpath -o "$work/git-byline" ./cmd/git-byline)

echo "building demo repository"
"$repo_root/docs/assets/demo/build-demo-repo.sh" "$work/git-byline" "$work/demo" >/dev/null

# Commands run exactly as a reader would type them: git resolves
# `git byline` through PATH, like an installed release.
export PATH="$work:$PATH"
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null
export NO_COLOR=1
cd "$work/demo"

# step <command> prints one terminal panel as JSON: the command line, its
# combined output, and its exit code.
step() {
  local command=$1 output status=0
  output="$(bash -c "$command" 2>&1)" || status=$?
  jq -n --arg command "$command" --arg output "$output" --argjson exit "$status" \
    '{command: $command, output: $output, exit: $exit}'
}

# blame <path> prints the plain blame report of one file at HEAD.
blame() {
  local file=$1
  jq -n --arg path "$file" --arg output "$(git byline blame --color=never "$file")" \
    '{path: $path, output: $output}'
}

echo "recording output"
git byline stats --json "$range" >"$work/stats.json"
git log --first-parent --format='%H%x1f%an%x1f%aI%x1f%s' "$range" |
  jq -R -s 'split("\n") | map(select(length > 0) | split("\u001f")
    | {commit: .[0], author: .[1], date: .[2], subject: .[3]})' >"$work/log.json"
{
  blame src/pricing.go
  blame src/invoice.go
  blame src/tax.go
} | jq -s . >"$work/blame.json"
{
  step "git byline check $range --max-ai-percent 30"
  step "git byline check $range --max-ai-percent 60"
} | jq -s . >"$work/gate.json"
step "git byline verify $range --deep" | jq -s . >"$work/verify.json"
{
  step "git byline disclosure --range $range --output disclosure.json >/dev/null"
  step "jq -c '{ai_share: .totals.ai_share_percent, lines: .totals.lines, agents: [.agents[].agent]}' disclosure.json"
} | jq -s . >"$work/disclosure.json"
step "git byline stats --json $range | jq -c .totals" | jq -s . >"$work/json.json"

mkdir -p "$(dirname "$out")"
jq -n \
  --arg range "$range" \
  --slurpfile stats "$work/stats.json" \
  --slurpfile log "$work/log.json" \
  --slurpfile blame "$work/blame.json" \
  --slurpfile gate "$work/gate.json" \
  --slurpfile verify "$work/verify.json" \
  --slurpfile disclosure "$work/disclosure.json" \
  --slurpfile json "$work/json.json" \
  '{
    generator: "website/scripts/demo-data.sh",
    repository: "docs/assets/demo/build-demo-repo.sh",
    range: $range,
    stats: $stats[0],
    log: $log[0],
    blame: $blame[0],
    terminal: {
      gate: $gate[0],
      verify: $verify[0],
      disclosure: $disclosure[0],
      json: $json[0]
    }
  }' >"$out"

echo "wrote ${out#"$repo_root"/}"
