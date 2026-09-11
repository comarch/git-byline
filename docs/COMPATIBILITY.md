# Compatibility

## Supported matrix

| Dimension | Minimum | CI coverage | Policy |
| --- | --- | --- | --- |
| Go build toolchain | 1.24 | 1.24 and stable on Linux | Newer stable versions supported |
| Git | 2.31 | Runner-provided Git on Linux, macOS, Windows | Newer stable versions supported |
| Linux | amd64, arm64 | Tests on amd64, cross-build both | Static binary |
| macOS | amd64, arm64 | Tests on hosted runner, cross-build both | Static binary |
| Windows | amd64, arm64 | Tests on hosted runner, cross-build both | Static `.exe` |
| Object format | SHA-1 | Integration tests | Object IDs treated as opaque hex |
| Text | Valid UTF-8 | LF, CRLF, missing final newline | NUL and invalid UTF-8 skipped |

Release archives are built for six operating system and architecture pairs.
Compilation proves arm64 artifacts. Runtime tests execute on hosted amd64 or
native runner architecture.

## Dashboard output

`git byline dashboard` produces one self-contained HTML file. Generation
requires no server, browser extension, network connection, or external runtime.
The report uses standard HTML, CSS, SVG, and a small inline script for file
selection. Browsers that disable JavaScript still render the summary and first
file, but interactive file switching requires JavaScript.

## Agent adapters

| Product or interface | Preset | Events | Paths |
| --- | --- | --- | --- |
| Factory | `droid`, `portable-factory` | `Edit`, `Create`, `ApplyPatch` and generated pre/post hooks | Common file and patch fields |
| Claude Code | `claude`, `portable-claude` | `Write`, `Edit`, `MultiEdit` and generated pre/post hooks | Common file fields |
| GitHub Copilot | `portable-copilot` | Generated pre/post tool hooks | Common file and patch fields |
| VS Code Agent | `portable-vscode` | Generated pre/post tool hooks | Common file and patch fields |
| Cursor | `portable-cursor` | Generated pre/post tool hooks | Common file and patch fields |
| Codex | `portable-codex` | Generated pre/post tool hooks | Common file and patch fields |
| Gemini CLI | `portable-gemini` | Generated pre/post tool hooks | Common file and patch fields |
| Windsurf | `portable-windsurf` | Generated write-event hooks | Common file and patch fields |
| Grok | `portable-grok` | Generated pre/post tool hooks | Common file and patch fields |
| agent-v1 | `agent-v1` | Standard edit payload, `agent_name` required | `edited_filepaths` |

Unknown valid tool events are ignored. Malformed supported events fail without
writing a checkpoint.

## Shell attribution

Shell hooks are supported for Droid, Claude Code, and the nine portable agent
surfaces. The pre-shell checkpoint records the dirty worktree paths and their
blob IDs. The post-shell checkpoint records only paths whose current blob
differs from the pre-shell snapshot. Paths come from Git status, not from the
shell payload. When a shell payload carries a tool call or event identifier,
git-byline stores it and pairs the post event with the matching pre event.
Without an identifier, it pairs the post event with the latest unpaired
pre-shell checkpoint. Concurrent overlapping shell commands without
identifiers remain a documented race limitation.

Blob comparison avoids claiming unrelated dirty files merely because they were
already present. It does not remove the concurrent-edit race: a human edit to a
tracked path during a long-running shell command can be included in the
post-shell blob and attributed to the agent. This is a known limitation, not a
provenance guarantee.

PromptScript emits native hook configuration only for platforms with project
hook APIs. Other instruction targets can consume generated project guidance,
but cannot provide edit-event attribution without an external watcher or
daemon. git-byline does not add either.

OpenCode is not currently integrated. OpenCode exposes plugin execution hooks,
but the pinned PromptScript release does not generate an OpenCode hook plugin.
Track native support in
[PromptScript issue #454](https://github.com/mrwogu/promptscript/issues/454).

## Git behavior

- First-parent history is authoritative.
- Linked worktrees receive separate pending state.
- SHA-1 and longer opaque object IDs are accepted.
- Partial commits preserve excluded provenance for the next commit.
- Renames preserve provenance through Git rename detection.

## History rewrite matrix

| Operation | Status | Hook or command | Coverage |
| --- | --- | --- | --- |
| Rebase | Supported | `post-rewrite` | Plain, reordered, squashed, split, dropped, conflicted and aborted rebases |
| Amend | Supported | `post-rewrite` and `post-commit` | Unstaged and partially staged amend |
| Cherry-pick | Supported | `post-commit` | Normal and `--no-commit` cherry-pick |
| Merge | Supported | `post-merge` and `post-commit` | First parent authoritative; conflict resolution is untracked |
| Pull with rebase | Supported | `post-rewrite` | Rewritten local commits |
| Reset soft | Supported | `reference-transaction` | Pending ranges reproject onto worktree |
| Reset mixed | Supported | `reference-transaction` | Pending ranges reproject onto worktree |
| Reset hard | Supported | `reference-transaction` | Pending state is cleared; existing notes stay unchanged |
| Reset with pathspec | Supported | `git byline rewrite` | Invoke after a path-limited index reset |
| Branch switch | Supported | `post-checkout` | Pending ranges reproject onto checked-out worktree |
| Stash push | Supported | `reference-transaction` | Pending ranges stored under `refs/notes/byline-stash` |
| Stash apply | Supported | `git byline rewrite` | Restores pending ranges; stash note remains |
| Stash pop | Supported | `reference-transaction` | Restores pending ranges and removes consumed stash note |
| Stash pathspec | Supported | `git byline rewrite` | Selected paths follow the same stash note rules |

Run `git byline rewrite --mode <mode> --hook-input stdin` only when a Git hook
does not run automatically, including path-limited reset and stash apply.
Rewritten content with no exact or whitespace-only line match is `untracked`.
Dropped commits do not contribute attribution to later content.

## Attribution matching

The matcher first pairs exact equal lines. A second layer runs only between
already matched anchors and only on still-unmatched lines. It compares line
keys after removing leading and trailing whitespace, preserves order, and
handles formatter-only indentation changes.

The second layer does not tokenize, use a similarity threshold, or compare
meaning. It does not run before the first anchor or after the last anchor.
Anything beyond whitespace-only equality between matched anchors stays
untracked. An explicit transition fallback applies only to lines introduced by
that transition, never to guessed semantic equivalence.

`human-override` is a fourth line state. It marks a human replacement of
content from the most recent AI transition and carries that transition's
agent, model, session, and timestamp. `human` and `untracked` cannot carry
agent metadata.

## Unsupported behavior

- Git older than 2.31.
- Binary and invalid UTF-8 attribution.
- Network files or a separate cloud attribution service.
- Prompt and transcript storage.
- Managed hook installation into external or symlinked `core.hooksPath`
  directories.
- Automatic reconstruction across several commits when the Git hook did not
  annotate any intermediate commit.
- Provenance copied from non-first merge parents.

Do not add a compatibility claim without a test or a documented manual
verification result.
