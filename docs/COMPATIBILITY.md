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
| OpenCode | `portable-opencode` | Generated plugin for local `edit`, `write`, `apply_patch`, and `bash` tool calls | `tool`, `args`, `sessionID`, and `callID`; `apply_patch` body in `args.patchText` |
| agent-v1 | `agent-v1` | Standard edit payload, `agent_name` required | `edited_filepaths` |

Unknown tool events are ignored. Malformed supported events fail or are
ignored without writing a checkpoint.

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
| OpenCode | no model field in tool hook payloads | Checkpoints keep `unknown` |
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

Shell hooks are supported for Droid, Claude Code, and the ten portable agent
surfaces. The pre-shell checkpoint records the dirty worktree paths and their
blob IDs. The post-shell checkpoint records only paths whose current blob
differs from the pre-shell snapshot. Paths come from Git status in the
worktree where the hook runs, not from the shell payload, so a shell command
that edits another linked worktree is not attributed to the agent. When a
shell payload carries a tool call or event identifier, git-byline stores it
and pairs the post event with the matching pre event. Without an identifier,
it pairs the post event with the latest unpaired pre-shell checkpoint.
Concurrent overlapping shell commands without identifiers remain a
documented race limitation.

Blob comparison avoids claiming unrelated dirty files merely because they were
already present. It does not remove the concurrent-edit race: a human edit to a
tracked path during a long-running shell command can be included in the
post-shell blob and attributed to the agent. This is a known limitation, not a
provenance guarantee.

PromptScript emits native hook configuration only for platforms with project
hook APIs. Other instruction targets can consume generated project guidance,
but cannot provide edit-event attribution without an external watcher or
daemon. git-byline does not add either.

OpenCode uses the generated `.opencode/plugins/promptscript.ts` plugin. It
captures local `edit`, `write`, `apply_patch`, and `bash` tool calls through
OpenCode's `tool.execute.before` and `tool.execute.after` events. The plugin
payload provides `sessionID` and `callID`; git-byline uses the call ID to pair
shell events. `apply_patch` carries its whole patch in `patchText`, and
git-byline reads the touched file paths from the patch headers. When the
payload is too large, the plugin keeps only those paths. OpenCode tool events
do not provide the active model, so the model is recorded as `unknown`.
PromptScript model profiles describe configured model names, not the model
used by an individual tool call.

PromptScript 1.19.1 declares this plugin target Unix-only. The generated code
does not explicitly reject Windows, but git-byline has not verified native
Windows plugin loading or process execution. Native Windows is unverified; the
documented Windows path is to run OpenCode in WSL.

OpenCode plugin hooks wait for their checkpoint process before OpenCode
continues. The pre-edit snapshot exists before the tool writes, and the
post-edit snapshot exists before the next tool starts. A hook that fails or
runs past its 30 second limit is logged, and the tool call continues without
that checkpoint. MCP calls, some subagent paths, and failed tool calls do not
have dedicated plugin events. Attribution is best-effort on those paths, not
guaranteed.

## Git behavior

- First-parent history is authoritative.
- Linked worktrees receive separate pending state.
- An edit is recorded in the worktree of the same repository that owns the
  path, also when the agent hook runs in another worktree, such as the main
  checkout.
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
| Fast-forward pull or merge | Supported | `reference-transaction` | Pulled commits keep their original notes; pending work moves to the new tip; stashed work keeps its attribution in the stash note |
| Pull or merge with `--autostash` | Supported | `reference-transaction` | Git 2.44 or later; older Git needs the manual stash apply after a fast-forward |
| Pull with rebase | Supported | `post-rewrite` | Rewritten local commits |
| Reset soft | Supported | `reference-transaction` | Pending ranges reproject onto worktree |
| Reset mixed | Supported | `reference-transaction` | Pending ranges reproject onto worktree |
| Reset hard | Supported | `reference-transaction` | Pending state is cleared; existing notes stay unchanged |
| Reset with pathspec | Supported | `git byline rewrite` | Manual command after a path-limited index reset |
| Branch switch | Supported | `post-checkout` | Pending ranges reproject onto checked-out worktree |
| Stash push | Supported | `reference-transaction` | Hook stores only paths in the stash commit under `refs/notes/byline-stash` |
| Stash apply | Supported | `git byline rewrite --mode stash-apply` | Manual stdin command: `<stash-commit> 1`; restores pending ranges and keeps note |
| Stash pop | Supported | `reference-transaction` | Hook detects applied or merged worktree content, restores pending ranges, and compare-deletes owned note; manual when older entries remain |
| Stash pathspec | Supported | `reference-transaction` | Hook stores and restores only selected stash paths |

Run `git byline rewrite --mode <mode> --hook-input stdin` only when a Git hook
does not run automatically, including path-limited reset and stash apply. For
manual stash apply, stdin is `<full-stash-commit> 1`; use `0` when the note
should be removed after a manual pop. A pop that leaves older stash entries
moves `refs/stash` without a reference transaction, so no hook sees it. After
such a pop, pass the commit from its `Dropped refs/stash@{0} (<commit>)` line
with `0`.
Rewritten content with no exact or whitespace-only line match is `untracked`.
Dropped commits do not contribute attribution to later content.
When a rewritten commit cannot receive a reprojected note, the attribution
boundary clears and the next commit re-initializes attribution from live
checkpoint evidence.

Restored stash ranges merge with existing pending state. Existing entries win
for a path already present; restored entries fill paths not already pending.
A restored path layers the stash note on top of the note of `HEAD`, so lines
that a pull merged into the stashed file keep the attribution of their commit.
The operation lock covers the complete read, projection, retention, and state
write sequence.

## Fast-forward pulls

A fast-forward pull or merge moves the branch to commits made in another clone
or by the forge merge workflow. This clone has no evidence for them, so
`post-merge` annotation skips them with a warning. A guessed local note would
conflict with their real note on the next notes push. Fetch their notes after
the pull and before the next commit:

```sh
git fetch origin refs/notes/byline:refs/notes/byline
```

A commit made before those notes arrive marks lines it inherits from the
pulled commits as `untracked` in the files it changes. The attribution
boundary moves to the new tip only when that tip already carries a valid note.

Git refuses a fast-forward that would overwrite local changes, and
`--autostash` applies the stash only after the branch moves. So a path the
pulled commits changed was clean, and its pending ranges and checkpoints
describe edits that were reverted or stashed before the pull. The pull drops
those pending ranges, so the next commit reads the path from the new tip and
its note. Replaying the checkpoints over the incoming content would attribute
lines they never produced, so the pull consumes them with a warning. Pending
ranges and checkpoints on paths the pull did not change move to the new tip
and keep their attribution.

Stashed agent edits keep their attribution across such a pull. Before the
pull consumes checkpoints, it saves what they attribute on stashed paths in
the stash attribution note. Only the autostash of the pull and the newest
`git stash` entry count, and only when the stash was created on the old tip.
Applying the stash restores the note, so agent lines stay AI lines and pulled
lines keep the attribution of their commits. A stash that Git merged with the
pulled changes counts as applied only when each stashed path still holds, in
order, every line the stash added. Otherwise the hook drops the note instead
of guessing.

No hook sees the stash in two cases. Run the manual stash apply there, or the
agent edits commit as `human` lines on the paths the pull changed:

- `git stash pop` leaves older stash entries. Use the commit from its
  `Dropped refs/stash@{0} (<commit>)` line.
- A pull or merge with `--autostash` runs on Git older than 2.44, which
  records the autostash without a reference transaction. Use the commit from
  its `Created autostash: <commit>` line.

```sh
printf '%s 0\n' "$(git rev-parse <commit>)" |
  git byline rewrite --mode stash-apply --hook-input stdin
```

The `0` also removes the stash attribution note. Without it, the note stays
in `refs/notes/byline-stash` until a drop or `git stash clear` empties the
stash list.

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
