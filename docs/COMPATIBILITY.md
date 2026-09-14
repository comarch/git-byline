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
| Factory | `droid`, `portable-factory` | `Edit`, `Create`, `ApplyPatch` and generated pre/post hooks | Common file and patch fields; `ApplyPatch` body in `patch` or `input` |
| Claude Code | `claude`, `portable-claude` | `Write`, `Edit`, `MultiEdit` and generated pre/post hooks | Common file fields |
| GitHub Copilot | `portable-copilot` | Generated pre/post tool hooks | Common file and patch fields |
| VS Code Agent | `portable-vscode` | Generated pre/post tool hooks | Common file and patch fields |
| Cursor | `portable-cursor` | Generated pre/post tool hooks | Common file and patch fields |
| Codex | `portable-codex` | Generated pre/post tool hooks | Common file and patch fields |
| Gemini CLI | `portable-gemini` | Generated pre/post tool hooks | Common file and patch fields |
| Windsurf | `portable-windsurf` | Generated write-event hooks | `tool_info` file and command fields |
| Grok | `portable-grok` | Generated pre/post tool hooks | Common file and patch fields |
| agent-v1 | `agent-v1` | Standard edit payload, `agent_name` required | `edited_filepaths` |

Unknown valid tool events are ignored. Malformed supported events fail without
writing a checkpoint.

## Model detection

AI attribution records the model that produced each edit. Sources, verified
per surface:

| Surface | Model source | Verification |
| --- | --- | --- |
| Factory | session transcript `modelId`, then the session settings sidecar | Live hook capture |
| Claude Code | session transcript `model` | Live hook capture |
| Codex | hook payload `model` field | Upstream hooks documentation |
| Cursor | hook payload `model` field | Upstream hooks documentation |
| Windsurf | hook payload `model_name` field | Upstream hooks documentation and replayed payload tests |
| Gemini CLI | none in tool hook payloads; transcript read is unverified | Follow-up |
| VS Code | none in hook payload; transcript read is unverified | Follow-up |
| Copilot CLI | none in tool hook payloads | Upstream gap |
| Grok | none in tool hook payloads | Upstream gap |
| agent-v1 | hook payload `model` field | By schema |

Factory, Claude Code, Gemini CLI, VS Code, and Cursor payloads carry
`transcript_path`. When a payload names no model, git-byline reads the newest
model identifier from that local transcript and falls back to the session
settings sidecar before keeping `unknown`. Only the model name is read and
kept; transcript content never enters any record.

Known gaps:

- Gemini CLI tool hooks expose no model. The `BeforeModel` hook payload
  carries `llm_request.model`, which a future generated hook could relay.
- Copilot CLI and Grok tool hook payloads expose neither a model nor
  `transcript_path`.

Windsurf nests event details in `agent_action_name` and `tool_info`; the
portable preset reads both, including `trajectory_id` as the session and
`model_name` as the model.

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
- `blame` and `dashboard` file arguments resolve against the current
  directory first, then the repository root, matching `git blame`; stored
  attribution and checkpoint paths stay repository-relative.

## History rewrite matrix

| Operation | Status | Hook or command | Coverage |
| --- | --- | --- | --- |
| Rebase | Supported | `post-rewrite` | Plain, reordered, squashed, split, dropped, conflicted and aborted rebases |
| Amend | Supported | `post-rewrite` and `post-commit` | Unstaged and partially staged amend |
| Cherry-pick | Supported | `post-commit` | Normal `-x` cherry-pick; `--no-commit` requires a commit message marker |
| Merge | Supported | `post-merge` and `post-commit` | First parent authoritative; conflict resolution is untracked |
| Pull with rebase | Supported | `post-rewrite` | Rewritten local commits |
| Reset soft | Supported | `reference-transaction` | Pending ranges reproject onto worktree |
| Reset mixed | Supported | `reference-transaction` | Pending ranges reproject onto worktree |
| Reset hard | Supported | `reference-transaction` | Pending state is cleared; existing notes stay unchanged |
| Reset with pathspec | Supported | `git byline rewrite` | Manual command after a path-limited index reset |
| Branch switch | Supported | `post-checkout` | Pending ranges reproject onto checked-out worktree |
| Stash push | Supported | `reference-transaction` | Hook stores only paths in the stash commit under `refs/notes/byline-stash` |
| Stash apply | Supported | `git byline rewrite --mode stash-apply` | Manual stdin command: `<stash-commit> 1`; restores pending ranges and keeps note |
| Stash pop | Supported | `reference-transaction` | Hook detects applied worktree content, restores pending ranges, and compare-deletes owned note |
| Stash pathspec | Supported | `reference-transaction` | Hook stores and restores only selected stash paths |

Run `git byline rewrite --mode <mode> --hook-input stdin` only when a Git hook
does not run automatically, including path-limited reset and stash apply. For
manual stash apply, stdin is `<full-stash-commit> 1`; use `0` when the note
should be removed after a manual pop.
Rewritten content with no exact or whitespace-only line match is `untracked`.
Dropped commits do not contribute attribution to later content.
When a rewritten commit cannot receive a reprojected note, the attribution
boundary clears and the next commit re-initializes attribution from live
checkpoint evidence.

Restored stash ranges merge with existing pending state. Existing entries win
for a path already present; restored entries fill paths not already pending.
The operation lock covers the complete read, projection, retention, and state
write sequence.

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
