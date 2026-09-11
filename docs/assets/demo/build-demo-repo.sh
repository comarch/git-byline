#!/usr/bin/env bash
# Build the demo repository used to record the README media.
#
# The repository is synthetic but the attribution is real: every state is
# produced by sending agent-v1 hook events to git byline checkpoint and
# annotating actual commits. Nothing in the recorded output is edited by hand.
#
# Usage: build-demo-repo.sh <binary> <target-directory>
set -euo pipefail

binary="${1:?binary path is required}"
target="${2:?target directory is required}"

binary="$(cd "$(dirname "$binary")" && pwd)/$(basename "$binary")"
rm -rf "$target"
mkdir -p "$target"
cd "$target"

export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_DATE="2026-02-03T09:00:00+00:00"
export GIT_COMMITTER_DATE="2026-02-03T09:00:00+00:00"

git init -q -b main
git config user.name "John Doe"
git config user.email "john.doe@example.com"
git config commit.gpgsign false

byline() { "$binary" "$@"; }

checkpoint() { printf '%s' "$1" | byline checkpoint agent-v1 --hook-input stdin; }

commit() {
  git add -A
  git commit -q -m "$1"
  byline annotate >/dev/null
}

# at moves both commit dates forward so the range trend has real spacing.
at() {
  export GIT_AUTHOR_DATE="$1"
  export GIT_COMMITTER_DATE="$1"
}

mkdir -p src

# Commit 1: a human baseline, committed before any agent touches the file.
cat >src/pricing.go <<'EOF'
package pricing

// Plan is one billable customer plan.
type Plan struct {
	Name  string
	Seats int
}

func (p Plan) Valid() bool {
	return p.Name != "" && p.Seats > 0
}
EOF
commit "feat(pricing): add plan model"

# Commit 2: an agent adds a function through its edit tool.
at "2026-02-03T10:15:00+00:00"
checkpoint '{"type":"human","agent_name":"droid","edited_filepaths":["src/pricing.go"]}'
cat >>src/pricing.go <<'EOF'

// Total returns the plan price for one billing period.
func (p Plan) Total(unit int) int {
	return p.Seats * unit
}
EOF
checkpoint '{"type":"ai_agent","agent_name":"droid","model":"claude-sonnet-4-5","conversation_id":"pr-4812-droid","edited_filepaths":["src/pricing.go"]}'
commit "feat(pricing): add period total"

# Commit 3: a person rewrites two agent lines by hand, so the AI origin of
# the replaced range stays visible as human-override.
at "2026-02-03T14:40:00+00:00"
python3 - <<'EOF'
import pathlib
path = pathlib.Path("src/pricing.go")
text = path.read_text()
text = text.replace(
    "\treturn p.Seats * unit\n",
    "\t// Volume discount applies from the eleventh seat.\n"
    "\tif p.Seats > 10 {\n"
    "\t\treturn unit * p.Seats * 9 / 10\n"
    "\t}\n"
    "\treturn unit * p.Seats\n",
)
path.write_text(text)
EOF
checkpoint '{"type":"human","agent_name":"droid","edited_filepaths":["src/pricing.go"]}'
commit "fix(pricing): apply volume discount"

# Commit 4: a second agent writes a new file through a shell command.
at "2026-02-04T08:05:00+00:00"
checkpoint '{"type":"shell_pre","agent_name":"codex"}'
cat >src/invoice.go <<'EOF'
package pricing

import "fmt"

// Render formats one invoice line for a plan.
func Render(p Plan, unit int) string {
	return fmt.Sprintf("%s: %d seats, %d due", p.Name, p.Seats, p.Total(unit))
}
EOF
checkpoint '{"type":"shell_post","agent_name":"codex","model":"gpt-5-codex","conversation_id":"pr-4812-codex"}'
commit "feat(pricing): render invoice line"

# Commit 5: a third agent extends the file, then a second person commits.
checkpoint '{"type":"human","agent_name":"claude","edited_filepaths":["src/invoice.go"]}'
cat >>src/invoice.go <<'EOF'

// Summary formats the invoice header.
func Summary(count int) string {
	return fmt.Sprintf("%d plans invoiced", count)
}
EOF
checkpoint '{"type":"ai_agent","agent_name":"claude","model":"claude-opus-4-6","conversation_id":"pr-4817-claude","edited_filepaths":["src/invoice.go"]}'

# A second person adds two lines by hand with no agent involved. No hook
# observes the edit, so annotate records them as her human lines.
cat >>src/invoice.go <<'EOF'

// Footer is required by the billing team.
const Footer = "questions: billing@example.com"
EOF
git config user.name "Maya Chen"
git config user.email "maya.chen@example.com"
at "2026-02-04T11:20:00+00:00"
commit "feat(pricing): add invoice summary"

# Commit 6: a merge brings content no local hook ever observed, so
# git-byline records it as untracked instead of guessing an author.
git checkout -q -b vendor-import
cat >src/tax.go <<'EOF'
package pricing

// TaxRate is the flat rate applied to every invoice.
const TaxRate = 23
EOF
git add -A
git commit -q -m "chore(pricing): import vendor tax rate"
git checkout -q main
at "2026-02-04T15:45:00+00:00"
git merge -q --no-ff -m "merge: vendor tax rate" vendor-import
byline annotate >/dev/null

git config user.name "John Doe"
git config user.email "john.doe@example.com"

# Commit 7: one commit that touches every file, so the HEAD attribution
# note carries all four states at once for the dashboard.
at "2026-02-05T09:30:00+00:00"
checkpoint '{"type":"human","agent_name":"droid","edited_filepaths":["src/pricing.go","src/tax.go"]}'
cat >>src/pricing.go <<'EOF'

// Annual returns the discounted price for a yearly commitment.
func (p Plan) Annual(unit int) int {
	return p.Total(unit) * 10
}
EOF
checkpoint '{"type":"ai_agent","agent_name":"droid","model":"claude-sonnet-4-5","conversation_id":"pr-4823-droid","edited_filepaths":["src/pricing.go"]}'
cat >>src/tax.go <<'EOF'

// WithTax applies the flat rate to one amount.
func WithTax(amount int) int {
	return amount * (100 + TaxRate) / 100
}
EOF
commit "feat(pricing): add annual price and tax helper"

byline status
