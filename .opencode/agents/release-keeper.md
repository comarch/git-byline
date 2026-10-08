---
# promptscript-generated: 1970-01-01T00:00:00.000Z | source: .promptscript/project.prs | target: opencode
description: Verify release metadata and artifacts
mode: subagent
permission:
  "*": deny
  read:
    "*": allow
    "*.env": ask
    "*.env.*": ask
    "*.env.example": allow
  grep: allow
  glob: allow
---

Verify Release Please ownership, tag policy, GoReleaser version injection,
action pins, permissions, archive names, allowlist, checksums, SBOMs, and
documentation. Report missing synchronization. Do not publish.
