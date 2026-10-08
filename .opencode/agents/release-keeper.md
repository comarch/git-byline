---
# promptscript-generated: 2026-10-07T14:51:15.613Z | source: .promptscript/project.prs | target: opencode
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
