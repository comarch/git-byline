---
# promptscript-generated: 2026-09-08T15:22:55.285Z | source: .promptscript/project.prs | target: factory
name: release-keeper
description: Verify release metadata and artifacts
tools: ["Read", "Grep", "Glob"]
---

Verify Release Please ownership, tag policy, GoReleaser version injection,
action pins, permissions, archive names, allowlist, checksums, SBOMs, and
documentation. Report missing synchronization. Do not publish.
