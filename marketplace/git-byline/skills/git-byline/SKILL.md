---
name: git-byline
description: Install, configure, inspect, or troubleshoot local human and AI line attribution.
license: MIT
compatibility:
  - factory-ai
  - claude-code
allowed-tools:
  - Read
  - Execute
  - Grep
---

# git-byline

Use `/git-byline-setup` for checksum-verified installation and hook activation.

Use:

```sh
git-byline status
git-byline blame path/to/file
git-byline blame --json path/to/file
git-byline dashboard
git-byline dashboard --output report.html path/to/file
```

Attribution data stays in local Git objects, worktree state, and
`refs/notes/byline`. Git hook installation shares the notes ref on ordinary
pushes unless `--local-notes` was selected. Never expose checkpoint content,
repository paths, session identifiers, note data, or generated dashboards in
public logs. Never push retention refs.
