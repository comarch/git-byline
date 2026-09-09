---
name: git-byline
description: Install, configure, inspect, or troubleshoot local human and AI line attribution.
license: MIT
compatibility:
  - factory-ai
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
```

Attribution data stays in local Git objects, worktree state, and
`refs/notes/byline`. Never expose checkpoint content, repository paths, session
identifiers, or note data in public logs. Do not push notes or retention refs
without explicit approval.
