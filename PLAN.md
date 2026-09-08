# git-byline - Implementation and Open Source Plan

Open source replacement for git-ai (usegitai.com). One Go binary with full
local functionality: line-level human/AI code attribution from agent edit to
commit. No cloud, no daemon, no telemetry, and no network transfer by the
binary.

This document is the source of truth for product scope, implementation,
security, public maintenance, and release readiness.

Canonical repository and Go module:
`github.com/mrwogu/git-byline`. Git remote:
`git@github.com:mrwogu/git-byline.git`. The GitHub repository remains private
until the public launch gate passes. Remote has no refs at implementation
planning time, so `main` starts from this reviewed local baseline.

## Why

- Attribution must work without uploading prompts or transcripts to a third
  party.
- Existing git-ai integration has no Droid preset; `install-hooks` does not
  know `.factory/hooks.json`.
- Droid hooks are near-identical to Claude Code hooks (same stdin JSON shape:
  `session_id`, `transcript_path`, `tool_name`, `tool_input`), so custom wiring is cheap.
- Users run Windows, macOS, and Linux, often without Python. A single static
  binary keeps installation independent of language runtimes.
- Name: a byline is the author credit line; this tool writes one for every
  line of code. `git-*` prefix installs as a `git byline` subcommand.

A fork of git-ai was considered and rejected. This project is an independent
implementation: no Rust toolchain, no vendor storage format, no cloud code to
strip, and no copied source or assets.

## Scope

### In

- `git-byline checkpoint` - snapshot + mark edits as human/AI (presets: droid, claude, agent-v1)
- Attribution engine - line-range tracking across snapshot chains
- `git-byline annotate` - post-commit: write per-line authorship to git notes
- `git-byline blame` - render line-level authorship (human / ai / untracked)
- `git-byline ask` - query original prompts behind a line (opt-in, phase 4)
- `git-byline status` - repo state, checkpoint count, last annotated commit
- `git-byline install-hooks` - wire Droid hooks, Claude Code hooks, and git post-commit hook
- Cross-platform binaries: linux/macos/windows x amd64/arm64
- Public source, contribution workflow, and reproducible GitHub Release
  artifacts

### Out (deliberately)

- Cloud sync, daemon, login/auth, dashboards. The tool is 100% local.
- Network calls of any kind. If a subcommand ever tries to dial out, that is a bug.
- IDE extensions. Hook wiring + binary covers it.
- Organization-specific MDM, private package repositories, and fleet rollout.
  Those remain downstream integrations.

## Design decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| Language | Go | Static binary, trivial cross-compile, fast iteration, no runtime deps |
| CGo | Forbidden | CGo breaks single-binary cross-compile matrix |
| Diff library | `github.com/sergi/go-diff` | Mature, pure Go, line diff is the only need |
| Storage | JSONL in `.git/byline/` + blobs in git ODB | Append-only, human-readable, git gc/dedup for free; no SQLite in MVP |
| Notes ref | `refs/notes/byline` | Own namespace, JSON format, versioned |
| Attribution model | Track AI only; default everything else to human | Human edits need no hooks; this is the vendor's trick too |
| Third state | `untracked` for lines predating any checkpoint history | Legacy code honesty; same concept as vendor |
| Prompts | Opt-in, OFF by default, phase 4 behind privacy and required organizational CISO review | Prompts are sensitive; attribution works without them |
| Hosting | `github.com/mrwogu/git-byline` | Private during development, public after the launch gate |
| Versioning | SemVer from `v0.1.0`, managed by Release Please | `feat` and pre-1.0 breaking changes increment minor; `fix` increments patch |
| AI instructions | PromptScript source, generated targets | One reviewable source of truth with drift detection |

## Open source operating model

Public source does not mean public private data. Source, documentation, issues,
and releases are public. Credentials, personal data, unpublished
vulnerabilities, raw transcripts, private infrastructure, and unredacted
diagnostics never belong in the repository.

Automation follows proof. The default branch must remain releasable. Every
workflow that can merge, tag, publish, change issues, or alter permissions has
an explicit owner, least-privilege credentials, an observable log, and a
documented disable path.

### Public repository contract

Complete before the repository accepts outside contributions:

- Add `.gitignore` (including `.worktrees/`), `.editorconfig`,
  `.gitattributes`, deterministic Go module files, and documented format,
  static analysis, test, coverage, build, and validation commands.
- Choose a standard SPDX license with an explicit owner or legal decision.
  Add the complete license text and verify dependency license compatibility.
- Add `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, and
  a generated `CHANGELOG.md`.
- Document purpose, non-goals, supported Go and Git versions, supported
  platforms, installation, quick start, configuration, limitations,
  troubleshooting, release process, and support boundaries.
- Add issue forms, contact links, a pull request template, labels, and
  `CODEOWNERS`. Security reports use GitHub private vulnerability reporting,
  never public issues.
- Add a lightweight threat model, supply-chain policy, incident response
  runbook, compatibility matrix, validation contract, and bad-release recovery
  procedure.
- Create `.promptscript/` as authored instruction source. Generate only targets
  the project actually uses, initially `AGENTS.md`. Pin the PromptScript CLI,
  run strict validation and compilation in CI, and fail on generated drift.
  Do not use remote PromptScript imports initially.
- Search the full repository for template placeholders, private names, local
  paths, credentials, tokens, private URLs, hostnames, and personal identifiers
  before the first public push.
- Verify all repository settings and required checks with a test pull request,
  including a fork pull request, before enabling auto-merge or publishing.

### Governance and contribution

- `main` is the protected default and release integration branch.
- Changes arrive through focused pull requests. Squash merge is the default.
  Direct pushes, force pushes, and deletion of `main` are blocked.
- At least one approval, required CODEOWNERS review for sensitive paths,
  dismissal of stale approvals, latest-push approval, resolved conversations,
  and required checks gate every merge.
- Sensitive paths include `.github/`, `.promptscript/`, `SECURITY.md`,
  `CODEOWNERS`, release and dependency configuration, `internal/hooks/`,
  `internal/store/`, and `internal/notes/`.
- Pull requests explain behavior, rationale, compatibility, validation,
  documentation, generated artifacts, release impact, and security/privacy
  impact. Non-trivial changes link an issue.
- Conventional Commits use imperative subjects with no trailing period.
  Project maximum remains 70 characters. Release Please owns release commits,
  versions, and changelog updates.
- Issues and fixtures use minimal, deterministic, synthetic or redacted data.
  Contributors are never asked for complete environment dumps, raw
  transcripts, production repositories, credentials, or unrelated diagnostics.

### Automation maturity

| Level | Policy | Promotion gate |
|-------|--------|----------------|
| 0 - Manual | Human review for dependencies, release PRs, and publishing | Repository bootstrap |
| 1 - Checks only | Bots may open PRs; humans merge and publish | Stable CI names and safe fork runs |
| 2 - Safe maintenance | Non-major dependency PRs may auto-merge after all required checks | Real update PR verified; rollback documented |
| 3 - Routine release | Release PR may auto-merge; protected tag workflow publishes | At least one successful manual release |

Major dependency updates, breaking behavior, license or contributor-term
changes, vulnerability disclosure, ownership changes, credential changes, and
release rollback remain human decisions. Level 4 broad autonomy is not planned.

### Public launch sequence

1. Resolve the blocking open questions while the project remains local.
2. Initialize local git on `main`, attach
   `git@github.com:mrwogu/git-byline.git` as `origin`, and add `.worktrees/` to
   `.gitignore` before creating any worktree. Commit only reviewed source and
   public-safe documentation.
3. Configure the existing private GitHub repository: least-privilege access,
   Actions, security features, labels, CODEOWNERS, and branch rules.
4. Finish phases 0-3. Run the complete local validation contract and inspect
   every release archive.
5. Audit the full git history, not only the working tree, for secrets, private
   data, private paths, copied material, large files, and incompatible
   licenses. Get owner sign-off before changing visibility.
6. Open a representative pull request. Prove required status names, ownership
   review, release behavior, and blocked auto-merge. Fix the configuration,
   never bypass missing proof.
7. Change visibility to public only after source, history, settings, security
   reporting, and recovery ownership pass review.
8. Test a pull request from a fork. Confirm it gets no write credential and all
   required checks report a conclusion.
9. Create the first release manually through Release Please. Verify checksums,
   SBOM, archive contents, installation, version output, and documentation from
   every supported platform.
10. Keep automation at level 1 initially. Promote dependency maintenance and
    release automation one level at a time only after its promotion gate passes.

## Binary layout

```
cmd/
  git-byline/
    main.go              # subcommand dispatch
internal/
  app/                  # command dispatch, flag parsing, orchestration
  gitcmd/               # safe git subprocess boundary; never invokes a shell
  lock/                 # cross-platform repository operation lock
  model/                # versioned records, ranges, metadata, validation
  store/                # JSONL checkpoint log, ODB blob writes, state file
  engine/               # snapshot chain walk, diff, range assignment, merging
  notes/                # notes read/write, format versioning
  blame/                # blame rendering (text + --json)
  hooks/                # install-hooks: droid, claude, git post-commit wiring
  preset/               # stdin adapters: droid, claude, agent-v1
  prompt/               # transcript parsing (phase 4)
  version/              # build-time version, local fallback `dev`
  testutil/             # temp git repo helpers used only by tests
tools/
  validate/             # cross-platform repository validation; not shipped
```

## Data model

### Checkpoint log - `.git/byline/checkpoints.jsonl`

Append-only, one JSON object per line:

```json
{"version":1,"kind":"edit","seq":1,"base_commit":"abc123","ts":"2026-09-08T14:03:11Z","type":"human","session":"","agent":"","model":"","files":[{"path":"src/auth.ts","exists":true,"blob":"oid"}]}
{"version":1,"kind":"edit","seq":2,"base_commit":"abc123","ts":"2026-09-08T14:03:12Z","type":"ai","session":"sess-123","agent":"droid","model":"glm-5","files":[{"path":"src/auth.ts","exists":true,"blob":"oid"}]}
{"version":1,"kind":"commit","seq":3,"commit":"def456","parent":"abc123","files":[{"path":"src/auth.ts","exists":true,"blob":"worktree-oid"}]}
```

- `kind=edit`: an agent-hook checkpoint.
- `kind=commit`: durable boundary appended by `annotate` before attribution
  work. It records HEAD, first parent, and immediate post-commit worktree
  snapshots for paths carrying pending provenance. Retry reuses the same
  boundary instead of observing a later worktree.
- `base_commit`: HEAD observed with the edit. It partitions checkpoints across
  commits and prevents replay against an unrelated branch. It is empty only
  while HEAD is unborn.
- `type=human`: written by PreToolUse (state before AI touches the file).
- `type=ai`: written by PostToolUse (state after AI edit, with metadata).
- `seq`: monotonic repository-local sequence assigned while holding the
  checkpoint lock. Log order, not wall-clock time, defines replay order.
- `exists=false`: the path did not exist after the event; `blob` is omitted.
  This represents create, delete, and rename boundaries without fake content.
- `blob` = `git hash-object -w` of the file content at checkpoint time
  (lives in git ODB, deduped, gc-eligible once annotated).
- Unknown checkpoint versions are skipped with a warning. Invalid interior
  JSONL is an error. One truncated final line is ignored with a warning so an
  interrupted append does not destroy earlier records.

### Worktree isolation

Checkpoint log, state, pending files, and retention belong to one worktree.
Resolve their directory from the worktree-specific Git dir, not the common Git
dir. The main worktree uses `.git/byline/`; linked worktrees use their own Git
administrative directory. This prevents checkpoints from one checked-out
branch from being replayed against another.

`refs/notes/byline` remains common because notes are keyed by commit. Note
updates use one common-repository lock. Checkpoint and state writes use a
worktree lock. `annotate` acquires common lock first, then worktree lock, so
lock order is fixed. Lock files live under the resolved common and
worktree-specific `byline/` administrative directories, never in tracked
source.

### Snapshot retention - `refs/worktree/byline/checkpoints`

Checkpoint blobs are unreachable objects unless a ref protects them, so Git
garbage collection could otherwise delete them before `annotate`.

- A local-only synthetic tree referenced directly by the per-worktree ref
  `refs/worktree/byline/checkpoints` contains every blob needed by unconsumed
  checkpoints and `pending.files`.
- Synthetic tree entries use sequence and file index, not user path, so
  multiple versions remain reachable without exposing path names in the tree.
- Checkpoint writes create and move the retention ref before appending JSONL.
  A crash may retain an unused blob but never leaves a logged blob unprotected.
- After note and state writes succeed, `annotate` atomically replaces the ref
  with a rebuilt tree containing only still-pending blobs. Old snapshots then
  become garbage-collectable.
- The ref is never fetched or pushed by default. Documentation treats
  `refs/worktree/byline/checkpoints` as content-bearing private data, like raw
  checkpoints.
- Integration tests run aggressive `git gc --prune=now` before annotation and
  prove all pending snapshots remain readable.

### State - `.git/byline/state.json`

```json
{"version":1,"last_annotated_commit":"def456","last_checkpoint_seq":42,"notes_version":1,"pending":{"base_commit":"def456","files":{}}}
```

- `last_checkpoint_seq` marks the durable replay boundary.
- `pending` stores base commit, latest uncommitted worktree blobs, and
  attributed ranges after a partial commit. Its file map is empty after a clean
  full-worktree commit.
- State writes use temp file, file sync, and atomic rename. State advances only
  after the git note write succeeds.

### Notes - `refs/notes/byline`, format v1

One note per commit. It contains paths present in the commit and changed from
the first parent; deleted paths have no entry. Each entry records the exact
commit blob so a reader never applies ranges to different content:

```json
{
  "version": 1,
  "files": {
    "src/auth.ts": {
      "blob": "oid",
      "ranges": [
        {"start":1,"end":3,"author":"human"},
        {"start":4,"end":7,"author":"ai","agent":"droid","model":"glm-5","session":"sess-123","ts":"2026-09-08T14:03:12Z"},
        {"start":8,"end":11,"author":"untracked"}
      ]
    }
  }
}
```

Notes are canonical JSON: stable path order, stable field order, trailing
newline. Readers tolerate unknown `version` with a warning, never crash.
`blame` walks first-parent history to the nearest matching path entry. Missing
history, unknown versions, or a blob mismatch produce `untracked`, never a
guess. A retry accepts an existing byte-identical note. It never overwrites a
different existing note without an explicit recovery command.

## Attribution algorithm (core)

At `annotate` (post-commit), while holding common and worktree locks:

1. Read HEAD and state. If no unconsumed commit boundary exists for HEAD,
   snapshot immediate post-commit worktree state for every path carrying
   pending provenance, expand the retention tree, and append a `kind=commit`
   record. This is the first durable action.
2. Select the oldest boundary after `last_checkpoint_seq`. Its parent must
   equal `last_annotated_commit`, except for root initialization. A missing or
   divergent boundary fails safely without consuming records. Process queued
   boundaries in first-parent order through current HEAD.
3. For boundary commit C with parent P, select pending state based on P and
   `kind=edit` records whose `base_commit` is P and sequence precedes C's
   boundary. Build each changed file's chronological snapshot chain. Start
   from the nearest compatible note for that path before C, or `untracked`
   ranges when none exists.
4. Replay transitions. Changed lines entering a human checkpoint become
   `human`. Changed lines entering an AI checkpoint receive that checkpoint's
   metadata. Equal lines retain attribution.
5. Read C's committed blobs from Git. Changes after the final edit checkpoint
   default to `human`. For merge commits, unmatched merge-result content
   defaults to `untracked` because first-parent replay cannot prove its source.
6. Project replayed provenance onto C's committed blob. Equal aligned lines
   retain attribution. Content found in an earlier snapshot keeps its most
   recent matching provenance. Content never observed in a checkpoint defaults
   to `human`; unchanged parent content without history stays `untracked`.
7. Project the same provenance onto boundary worktree snapshots and retain only
   content not present in C. This becomes pending state based on C and preserves
   partial-stage attribution for the next commit.
8. Merge adjacent ranges with identical author and metadata. Validate complete
   coverage, ordered ranges, valid bounds, no overlap, and matching blob line
   counts for both note and pending state.
9. Write deterministic note JSON. Atomically write state with C and boundary
   sequence. Only then compact the retention tree to remaining pending blobs.
   A retry after any interruption reuses the boundary and converges.

Invariants (property-tested):
- Every line of every changed file has exactly one range (complete coverage, no overlap).
- A line never flips human -> ai -> human across chains unless checkpoints say so.
- Projection is deterministic for duplicate lines and repeated snapshots.
- Replaying the same inputs produces byte-identical notes and state.
- Every consumed edit belongs to exactly one commit boundary or explicit
  pending state.

Known accepted limitations (document, do not silently "fix"):
- Rebase/amend rewrites SHAs -> notes stay on old commits (same as vendor).
- Partial staging (`git add -p`): attribution is computed for the committed
  blob. Git exposes no staging timestamp, so identical duplicate lines may be
  ambiguous; deterministic most-recent matching provenance wins.
- Merge commits: MVP reads first-parent history. A merge-resolution blob that
  does not match inherited content becomes `untracked` with a warning until
  phase 3 merge handling.
- If the post-commit hook never starts, no boundary exists. A later `annotate`
  detects the parent gap and refuses automatic consumption. Phase 3 recovery
  writes missing notes from available commit blobs but marks ambiguous
  carry-over content `untracked` and warns instead of inventing provenance.
- Binary files: skipped.

## Commands

```
git-byline checkpoint <preset> --type human|ai --hook-input stdin
git-byline annotate                               # called from git post-commit
git-byline blame <file> [--json]                  # humans + droid slash command
git-byline status                                 # checkpoints, last note, coverage
git-byline ask <query>                            # phase 4, prompts opt-in
git-byline install-hooks [--agent droid,claude] [--git] [--user|--project] [--env]
git-byline uninstall                              # reverse of install-hooks
git-byline version
```

Binary name follows the `git-*` convention (like git-lfs, git-ai): on PATH,
git exposes it as `git byline ...` for free.

## Presets

### `droid` (first-class)

Stdin = Droid hook JSON (`PreToolUse` / `PostToolUse` on `Edit|Create|ApplyPatch`).

- `PreToolUse` calls `--type human`; `PostToolUse` calls `--type ai`. The
  explicit flag is required because hook payloads need not identify their
  phase.
- `Edit` and `Create` paths come from `tool_input.file_path`.
- `ApplyPatch` paths come from validated add, update, delete, and move headers
  in the patch input. PreToolUse captures source and destination state before
  execution; PostToolUse captures every resulting path. Malformed or ambiguous
  patch input fails closed.
- PostToolUse rereads actual files after the tool finishes. It does not trust
  `tool_response` as file content.
- AI checkpoints include `session_id`; model is read from
  `transcript_path` (session JSONL); fallback `unknown`

Wiring (`install-hooks --agent droid`), project scope `.factory/hooks.json`:

```json
{
  "PreToolUse": [
    {"matcher": "Edit|Create|ApplyPatch",
     "hooks": [{"type": "command", "command": "git-byline checkpoint droid --type human --hook-input stdin", "timeout": 30}]}
  ],
  "PostToolUse": [
    {"matcher": "Edit|Create|ApplyPatch",
     "hooks": [{"type": "command", "command": "git-byline checkpoint droid --type ai --hook-input stdin", "timeout": 30}]}
  ]
}
```

`--env` appends the binary dir to PATH in detected shell rc files (same idea
as vendor, for users whose PATH lacks the install dir).

### `claude` (compat)

Vendor-compatible stdin on `Write|Edit|MultiEdit` matchers with explicit
human/AI phase flags; merges into `~/.claude/settings.json`. Goal: drop-in for
teams already using git-ai hook shape.

### `agent-v1` (vendor standard)

Vendor's generic preset: strict JSON via stdin (`type: human|ai_agent`,
`transcript`, `agent_name`, `model`, `conversation_id`, `edited_filepaths`).
For this preset, payload `type` is authoritative and `--type` is omitted.
Conflicting explicit type and payload type is rejected. Keeps interop with any
agent that adopts the standard; our upstream-contribution path later.

## Hook wiring

Two hook systems, different jobs:

1. **Agent hooks** (Droid / Claude Code) -> checkpoints. Fire on every AI edit.
2. **Git hooks** (post-commit) -> annotate. Fires on every commit, including
   commits made by humans in a terminal (agent hooks never fire there).

Git hook install policy:
- If `core.hooksPath` unset: write `.git/hooks/post-commit`.
- If set (husky/lefthook users): chain - call existing script then `git-byline annotate`. Never overwrite.
- `install-hooks` must be idempotent and merge, not overwrite (vendor's
  first-class requirement, adopted).

## Phases

| Phase | Scope | Exit criteria | Est. |
|-------|-------|---------------|------|
| 0 - Foundation | initialize the empty private remote; choose license; public docs and templates; PromptScript source; Go module and command tree; git process boundary; validation command; CI, security, coverage, and GoReleaser configuration | local validation passes; no placeholders or private data; `git-byline version` builds on all 6 targets | 4-5 d |
| 1 - Attribution MVP | versioned checkpoint/state/note formats; snapshot retention; preset parsing; checkpoint; pure attribution engine; partial-commit projection; annotate; blame | interleaved human/AI edits and partial commit scenarios produce deterministic golden notes and valid coverage | 8-10 d |
| 2 - Wiring | `install-hooks` for droid + claude + git, `--env`, uninstall | fresh machine: install -> droid session -> commit -> correct blame, zero manual steps | 3-4 d |
| 3 - Robustness | rename detection, duplicate-line stress, cross-process locking, crash recovery, `status`, Windows hook invocation, untracked and merge-warning polish | integration, fuzz, contention, and aggressive-gc suites green on macOS, Windows, and Linux | 5-7 d |
| 4 - Prompts & ask | transcript parsing, opt-in prompt storage, `git-byline ask` | maintainer privacy decision and required CISO sign-off recorded; attribution still works with prompts OFF | 3-5 d |
| 5 - Public launch | private GitHub bootstrap; repository settings and ruleset; Renovate and Release Please; same-repository and fork test PRs; public visibility; first manual release | clean public history; protected checks proven; release installs on all supported targets; checksums and SBOM verified | 3-4 d |

Total: about 5 weeks, one developer, to phase 3 (full attribution without
prompts), plus public launch.

## Detailed implementation sequence

Each slice ends with committed tests and a green narrow validation command.
No slice starts by weakening a failing gate.

### Slice 0.1 - Repository and module foundation

Implementation:

- Initialize `main` from the currently empty private remote. Set module path to
  `github.com/mrwogu/git-byline`.
- Add `.gitignore` with `.worktrees/`, `.editorconfig`, `.gitattributes`,
  `go.mod`, baseline documentation, license after owner decision, and public
  community files.
- Create `cmd/git-byline`, `internal/app`, and `internal/version`. Use the
  standard `flag` package and explicit command dispatch. Unknown commands and
  invalid flags exit `2`; operational failures exit `1`.
- Inject release version with linker flags. Local builds report `dev`.
- Add PromptScript source and compile only `AGENTS.md`.

Tests:

- CLI table tests: no command, help, unknown command, invalid flag, version,
  stdout/stderr separation, and exact exit codes.
- Build smoke test with `CGO_ENABLED=0`.
- PromptScript validation and generated drift test.
- Placeholder, private-path, forbidden-dash, and secret scans.

Gate: `go test ./...`, `go vet ./...`, PromptScript checks, and six target
builds pass.

### Slice 0.2 - Safe Git process boundary

Implementation:

- Build `internal/gitcmd` around `exec.CommandContext`. Pass arguments as a
  slice, never through a shell. Set explicit working directory and bounded
  stdout/stderr buffers.
- Implement repository discovery, git-dir and worktree resolution, HEAD and
  first-parent lookup, blob/tree reads, changed-path listing, ignored-path
  checks, hash-object writes, notes reads/writes, ref updates, and git version
  parsing.
- Use machine-readable Git formats and NUL-delimited path output. Never parse
  localized human output. Treat object IDs as validated opaque hex, not
  fixed-width SHA-1.
- Accept absolute or relative hook paths, resolve them against the active
  worktree, then store repository-relative slash paths. Reject NUL, `.git`,
  parent traversal, symlink escape, ignored paths, and every resolved path
  outside the active worktree.
- Use Go 1.24 `os.OpenRoot` for worktree file access. Read bounded regular
  files through the root and feed bytes to `git hash-object --stdin`; never ask
  Git to open an untrusted hook path. Skip symlinks, submodules, directories,
  devices, sockets, NUL-containing files, and invalid UTF-8 with a warning.
- Define typed command errors containing operation and exit status but no file
  content or full environment.

Tests:

- Table tests for argument construction, error wrapping, output limits,
  cancellation, unusual filenames, Unicode, spaces, newlines, and invalid
  paths, internal and escaping symlinks, submodules, and non-regular files.
- Integration tests against temporary SHA-1 repositories for every wrapper.
- Optional SHA-256 repository tests when installed Git supports
  `--object-format=sha256`.
- A fake `git` executable proves arguments are not shell-expanded and
  credentials or hook payloads are not included in errors.

Gate: all Git calls flow through `internal/gitcmd`; production code contains no
shell invocation or network package.

### Slice 1.1 - Versioned storage and snapshot retention

Implementation:

- Add strict model types for edit checkpoint v1, commit boundary v1, state v1,
  note v1, file entries, ranges, author metadata, and object IDs.
- Implement repository path setup, append-only checkpoint writes, streaming
  JSONL reads, final-line crash tolerance, atomic state writes, canonical note
  JSON, and schema validation.
- Add `internal/lock` before any read-modify-write operation. Use OS-released
  advisory file locks through platform-specific standard-library syscalls:
  `flock` on Unix and `LockFileEx` on Windows. Lock metadata is diagnostic
  only. Contention waits with bounded timeout; process death releases the lock.
- Protect unconsumed snapshots with `refs/worktree/byline/checkpoints`. Use
  compare-and-swap ref updates so concurrent writers cannot lose retained
  blobs.

Tests:

- Table tests for valid, malformed, unknown-version, truncated, oversized,
  duplicate, conflicting, and out-of-order edit and boundary records.
- Golden tests for byte-stable state and note JSON.
- Failure-injection tests around append, sync, rename, note write, ref update,
  and state advancement.
- Multi-process contention tests. Exactly one sequence number per append, no
  lost records, no deadlock.
- Aggressive `git gc --prune=now` between checkpoint and annotate proves
  retained blobs survive. Consumed blobs become prune-eligible.

Gate: storage replay is deterministic and recoverable after a process is
terminated at every persistence boundary.

### Slice 1.2 - Pure attribution engine

Implementation:

- Keep `internal/engine` independent from filesystem, Git, JSON, clocks, and
  global state. Inputs are line snapshots, prior ranges, checkpoint metadata,
  and target content.
- Tokenize valid UTF-8 text into content plus exact `LF`, `CRLF`, or absent
  terminator. A final terminator creates no phantom line, and a terminator-only
  change remains attributable. Treat NUL-containing or invalid UTF-8 input as
  binary and skip it with a warning. Enforce documented line and file limits.
- Convert `sergi/go-diff` output into stable line operations. Keep a package
  adapter around the dependency so its representation does not leak.
- Implement transition replay, range transformation, metadata assignment,
  adjacent-range merge, committed-blob projection, and invariant validation.
- Resolve duplicate equal lines deterministically with most-recent provenance
  and stable positional tie-breaking. Document ambiguity rather than guessing
  nondeterministically.

Tests:

- Table tests: empty, insert-only, delete-only, replace-all, repeated lines,
  moved text, no final newline, CRLF, long lines, Unicode, invalid UTF-8, and
  metadata changes.
- Golden edit-chain tests for human-only, AI-only, alternating authors,
  untouched legacy content, create/delete/recreate, and two AI sessions.
- Property tests over randomized snapshots assert complete coverage, valid
  bounds, no overlap, deterministic replay, idempotent normalization, and
  preservation of unchanged-line provenance.
- Fuzz targets for line splitting, diff conversion, transition replay, target
  projection, and range validation. Seed corpus comes from all regression
  fixtures.

Gate: invariant property suite passes at least 100,000 deterministic generated
chains; each fuzz target completes a 10-minute pre-release run without a crash
or invariant failure.

### Slice 1.3 - Presets and checkpoint command

Implementation:

- Define a preset interface that converts stdin plus explicit hook phase into
  one or more normalized checkpoint events. Require `--type` for Droid and
  Claude. Use payload `type` for `agent-v1` and reject conflicts.
- Implement and fuzz path extraction separately for Droid
  `Edit|Create|ApplyPatch` and Claude `Write|Edit|MultiEdit`. ApplyPatch parsing
  returns source and destination paths for add, update, delete, and move.
- Bound stdin size. Reject malformed JSON and unsupported tool events. Ignore
  valid non-edit events without creating a checkpoint.
- Resolve edited paths through `gitcmd`, snapshot only allowed paths, write
  blobs, update retention, then append checkpoint records under one lock.
- Use hook input only to locate changes and metadata. Never persist raw input,
  tool response, transcript, environment, or file content in JSONL.
- Keep model extraction isolated and return `unknown` when unavailable without
  reading prompt content.

Tests:

- Table tests for each event kind and phase, ApplyPatch add/update/delete/move,
  multiple and duplicate paths, create/delete, missing fields, unknown fields,
  type conflict, path rejection, ignored files, oversized stdin, and absent
  transcript.
- Pinned, minimized, redacted real Droid and Claude captures. Synthetic IDs and
  paths only.
- Command tests prove one logical event produces one ordered checkpoint and
  that failed retention or append leaves recoverable state.

Gate: captured fixtures parse unchanged and a temp repository survives
checkpoint, process restart, and aggressive Git GC.

### Slice 1.4 - Annotate, notes, and blame

Implementation:

- Orchestrate `annotate` in `internal/app`: lock, durably append or reuse HEAD's
  commit boundary, replay checkpoints, project onto commit and boundary
  worktree, validate, write note, update state, compact retention.
- Make repeated `annotate` on the same HEAD a no-op with byte-identical state.
  Recovery after a note-only or state-only interruption must converge.
- Implement `blame <file>` by walking first-parent notes to the nearest matching
  file entry, validating its blob, and rendering every current line.
- Text output is stable and human-readable. `--json` has a documented,
  versioned schema. Diagnostics go to stderr.
- Unknown notes, missing notes, merge ambiguity, and mismatched blobs warn and
  return `untracked` coverage instead of crashing.

Tests:

- End-to-end temp repositories: root commit, legacy history, human-only,
  AI-only, interleaved edits, partial stage with pending worktree changes,
  amend, missing note, unknown note version, binary file, empty file, and two
  consecutive commits.
- Golden note and blame outputs, including exact JSON and line endings.
- Failure injection before and after boundary append, note write, state update,
  and retention compaction, then retry.
- Contract tests for exit codes and stdout/stderr.

Gate: the interleaved and partial-stage golden scenarios produce complete,
non-overlapping, deterministic attribution after restart.

### Slice 2 - Hook installation and removal

Implementation:

- Install project or user Droid and Claude configuration with structural JSON
  merging. Preserve unknown fields, existing hooks, formatting where
  practical, and file permissions.
- Install a small managed block in Git `post-commit`. Chain existing hooks and
  preserve their exit status according to documented policy. Respect
  `core.hooksPath`.
- Add explicit ownership markers and backups so uninstall removes only
  git-byline-managed content.
- Implement `--env` for supported shells with managed blocks, atomic writes,
  clear preview, and exact rollback.

Tests:

- Fixture matrices for absent, valid, malformed, and concurrently changed
  configuration.
- Idempotency: install twice and uninstall twice produce no extra change.
- Preservation: unrelated JSON, shell configuration, hooks, mode bits, CRLF,
  and existing exit behavior stay intact.
- Scenario tests in isolated temporary HOME and repository directories for
  default hooks, custom `core.hooksPath`, worktrees, spaces, and Unicode paths.
- Real command invocation smoke tests on Linux, macOS, and Windows.

Gate: fresh temp HOME -> install -> AI edit -> commit -> blame -> uninstall
passes without manual steps or unrelated file changes.

### Slice 3 - Robustness and status

Implementation:

- Add rename detection using Git's changed-path data while preserving engine
  identity separately from display path.
- Finish pending-state behavior for repeated partial commits, reset, checkout,
  amend, deleted branches, detached HEAD, and stale checkpoints.
- Implement `status` with repository identity, checkpoint counts, retained
  snapshot count, last annotated commit, note version, pending files, coverage,
  lock state, and actionable warnings. JSON mode remains stable.
- Add configurable file and input limits with safe defaults and explicit skip
  warnings. Binary detection must not load an unbounded file.
- Keep merges first-parent and warning-only unless a separately tested merge
  algorithm is accepted.

Tests:

- Scenario matrix for rename, rename plus edit, rename cycles, partial commit
  sequences, reset, amend, detached HEAD, worktrees, missing post-commit
  boundary, corrupt tail, large file, binary file, and concurrent sessions.
- Repeated subprocess concurrency tests on Linux. The race detector is not a
  gate because it requires CGo; the project keeps the no-CGo rule absolute.
- Scheduled fuzz runs and repeated randomized end-to-end histories.
- Performance baselines for a 10 MB text file, 100 checkpoint chain, 10,000
  changed files in status metadata, and bounded error output. Record budgets
  after the first implementation baseline rather than inventing claims.

Gate: full suite passes on Linux, macOS, and Windows with no data loss,
unbounded memory path, concurrency failure, or invariant violation.

### Slice 4 - Prompt storage and ask

No implementation begins before the privacy gate. First deliver a separate
design covering explicit opt-in, retention, deletion, redaction, note/export
boundaries, local search, threat model, and organizational CISO approval.

Tests must prove prompts remain absent with default settings, disabled mode
never opens transcript files, uninstall removes only owned prompt data when
requested, and `ask` performs no network access.

### Slice 5 - Release and public launch

Implementation:

- Add immutable-pinned CI, CodeQL, dependency review, coverage, PromptScript,
  Release Please, GoReleaser, SBOM, checksum, and release verification
  workflows.
- Configure Renovate with manual majors and no release for development-tool or
  action-only updates.
- Complete repository docs, security reporting, threat model, CODEOWNERS,
  issue forms, pull request template, labels, ruleset, and recovery ownership.
- Publish only after full history and artifact privacy scans pass.

Tests:

- Same-repository and fork pull requests prove every required check reports and
  forks receive no write credential.
- Snapshot release builds all six targets. Each archive is inspected, checksum
  verified, installed, and asked for its version in a clean environment.
- First Release Please PR and GitHub Release remain manually approved.

Gate: public visibility and `v0.1.0` only after license, owners, response
targets, and all launch checks are resolved.

## Validation and testing

The repository exposes one cross-platform complete validation command:

```text
go run ./tools/validate
```

It runs the same core stages locally and in CI:

1. `gofmt` drift check.
2. `go vet ./...` and compile-time analysis.
3. Unit, property, contract, and integration tests.
4. Coverage generation and policy check.
5. `CGO_ENABLED=0 go build ./...`.
6. Builds for linux, macOS, and Windows on amd64 and arm64.
7. PromptScript strict validation, compilation, and generated-file drift check.
8. Production dependency, license, secret, and forbidden-network checks.
9. Release archive, version metadata, checksum, and SBOM verification.

Narrow commands remain available for fast feedback. Validation must be
deterministic on a clean checkout, require no personal credentials or live
services, fail on missing tests or artifacts, and work without untracked local
files.

### Testability rules

- `main` only wires streams and calls `app.Run`; tests never need to intercept
  `os.Exit`.
- Command handlers accept `stdin`, `stdout`, and `stderr` explicitly and return
  an exit code plus wrapped error.
- Clock, sequence allocation, process liveness, and Git execution have narrow
  injectable seams. Core engine code stays plain value transformation, not an
  interface hierarchy.
- Tests never use the developer repository, HOME, global Git configuration, or
  credentials. `internal/testutil` sets isolated HOME, `GIT_CONFIG_NOSYSTEM`,
  local author identity, deterministic timestamps, and temporary repositories.
- Expected JSON and user output are golden only when they are public
  contracts. Internal transformations use table assertions and invariants.
- Map-backed output is sorted before serialization. Random property tests print
  and persist their seed on failure.
- Test helpers fail the current test immediately and show commands without
  leaking environment values or captured file contents.
- Production package imports are checked. `os/exec` is allowed only in
  `internal/gitcmd`; networking packages are forbidden unless PLAN.md changes.

- Unit: range-walk invariants (complete coverage, no overlap), diff edge cases
  (empty file, all lines changed, insert-only, delete-only).
- Property: randomized edit sequences -> invariants hold.
- Integration: temp git repos, scripted scenarios: human-only, AI-only,
  interleaved, partial stage, rename, two sessions same repo, hook chaining,
  install/uninstall idempotency, and rollback after failed installation.
- Contract: every command's exit code, stdout, stderr, JSON schema, malformed
  input, empty input, interrupted write, path handling, encoding, and newline
  behavior.
- Golden: notes JSON fixtures.
- Fixtures: real captured Droid hook stdin samples, minimized and redacted
  before commit, then pinned so format drift fails loudly. Synthetic values
  replace session IDs, usernames, local paths, repository URLs, and transcript
  content.
- Compatibility: Go 1.24 and current stable Go; minimum supported Git version
  selected from tested command requirements plus current stable Git; Git
  behavior on Linux, macOS, and Windows; all advertised release architectures
  are built and inspected.
- Coverage: establish the project baseline when code lands, require at least
  80% patch coverage, allow at most a 1% project drop, and never lower a
  threshold only to pass a pull request. Attribution invariants, parsers,
  storage recovery, and hook mutation paths need branch-focused review even
  when aggregate coverage passes.
- Regression: every fixed bug includes a minimal reproduction, failing
  assertion, fix, and focused validation result.
- No product test uses network access. CI may access trusted package and
  security services only in separate, explicit jobs.

### CI execution matrix

| Check | Trigger | Matrix and responsibility |
|-------|---------|---------------------------|
| Validate repository | pull request, `main` | Ubuntu, Go 1.24; format, vet, validation tool, generated drift, placeholder/private-data scan, dependency and forbidden-import policy |
| Quality and build | pull request, `main` | Linux, macOS, Windows with Go 1.24; current stable Go on Linux; unit, property, integration, command-contract, subprocess concurrency, and six-target no-CGo builds |
| Coverage | pull request, `main` | Linux, current stable Go; standard no-CGo test run with project and patch report |
| Dependency review | pull request | Reject vulnerable or disallowed new runtime dependencies |
| CodeQL | pull request, `main`, weekly | Go analysis; fail on analysis error |
| Validate PromptScript | pull request, `main` | Strict validation, compile, and clean generated diff |
| Scheduled robustness | nightly | Randomized histories, aggressive Git GC, all fuzz targets for 10 minutes each, large-file performance smoke, latest supported Git |
| Release verification | release PR, tag, manual | GoReleaser snapshot or tagged build, six archive installs, version checks, checksum and SBOM validation, archive allowlist |

`Quality and build` is an aggregate required check over its matrix, so one
stable name gates branch protection. Nightly and manual jobs create issues or
alerts but are not required checks because they do not conclude on every pull
request.

Required pull request checks use stable names and always report a conclusion:

```text
Validate repository
Quality and build
Coverage
Dependency review
CodeQL
Validate PromptScript
```

GitHub Actions use explicit permissions, `contents: read` by default,
immutable commit SHA pins with version comments, concurrency cancellation,
timeouts, and no write credentials for fork pull requests. Required checks are
enabled in branch rules only after same-repository and fork pull requests prove
their exact status names.

### Quality gates

Every pull request:

- Narrow tests pass during development, then `go test ./...`, `go vet ./...`,
  full validation, and required CI checks pass before merge.
- New behavior includes focused tests. Engine and store changes include
  table-driven coverage; attribution changes include invariant or golden
  coverage; hook changes include an isolated scenario.
- No skipped required test, reduced threshold, broad fixture rewrite, generated
  hand-edit, unexplained dependency, or accepted warning is used to make CI
  green.

Phase 1 gate:

- Golden notes and blame output pass for root, legacy, human, AI, interleaved,
  partial-stage, restart, and aggressive-GC histories.
- 100,000 seeded randomized edit chains pass all attribution invariants.
- Persistence failure injection converges after retry without lost checkpoints
  or double attribution.

Phase 2 gate:

- Fresh isolated HOME scenarios pass on Linux, macOS, and Windows.
- Install and uninstall remain idempotent and preserve every unrelated byte and
  mode bit covered by fixtures.

Phase 3 gate:

- Full cross-platform suite, nightly fuzzing, subprocess contention, and
  performance smoke pass for seven consecutive days before release candidate.
- Any crash, corruption, invariant failure, leaked path, or unbounded memory
  behavior blocks release.

Release gate:

- `goreleaser release --snapshot --clean` passes before every release workflow
  change and release PR merge.
- Tagged artifacts are rebuilt from the tag, installed in clean environments,
  checked for exact version, compared with the file allowlist, and verified
  against checksums and SBOM.
- First public release and first workflow change after public launch require
  manual maintainer approval.

## Release and distribution

- SemVer tags use `vMAJOR.MINOR.PATCH`. Until `v1.0.0`, features and breaking
  changes increment minor; fixes increment patch.
- Release Please is the only version, changelog, release PR, and tag source.
  `feat` creates a minor release, `fix` creates a patch release, and breaking
  changes create a minor release before `v1.0.0`.
- The binary version comes from the validated git tag at build time and falls
  back to `dev` for local builds. No second hand-edited version is kept.
- GoReleaser builds from the release tag with `CGO_ENABLED=0` and `-trimpath`
  for linux, macOS, and Windows on amd64 and arm64.
- GitHub Releases are the public release channel. Every release contains
  versioned archives, SHA256 checksums, an SBOM, and release notes. Artifact
  names stay synchronized with installation documentation.
- The release workflow validates the tag, rebuilds from the tagged commit,
  rejects missing or empty artifacts, inspects archives for secrets, local
  paths, debug files, and unexpected files, and verifies installation and
  `git-byline version` before upload.
- Start with manually merged release PRs and a protected publishing
  environment. Move to automation level 3 only after one successful manual
  release and a tested bad-release procedure.
- Prefer the built-in `GITHUB_TOKEN`. Use a repository-scoped GitHub App or
  dedicated bot credential only when Release Please cannot trigger the required
  workflow. Never make a personal token the only recovery path.
- Do not recommend piping a remote install script into a shell. Document manual
  archive download, checksum verification, extraction, and PATH installation.
- Downstream Homebrew, Scoop, WinGet, MDM, or private package recipes may consume
  public release assets but are not release blockers for the core project.

### Dependency maintenance

- Commit `go.mod` and `go.sum`. Use only trusted upstream modules and review
  source, license, necessity, and binary-size impact before adding one.
- Renovate updates Go modules, GitHub Action SHA pins, GoReleaser, and pinned
  development tools. Its configuration is validated before activation.
- Runtime patch and non-breaking minor updates may auto-merge only at
  automation level 2 after all required checks. Major updates remain manual
  and need compatibility tests, migration notes, rollback, and a release owner.
- Runtime dependency updates use `chore(deps)` and create a patch release.
  Development tools use `chore(deps-dev)` and actions use `chore(ci)`; neither
  creates a product release.
- Dependency review, CodeQL, secret scanning, push protection, Dependabot
  alerts, and scheduled vulnerability scanning are enabled. Findings are fixed
  or accepted with written rationale; checks never silently ignore tool errors.

## Privacy and security

- The shipped binary never opens a network connection and contains no
  telemetry, update checker, analytics, remote lookup, or hidden download.
  Review the production dependency graph and fail validation when network
  packages or dial paths enter it without an explicit design change.
- Prompts and transcripts stay OFF by default. Phase 4 requires explicit
  opt-in, a documented data model and retention policy, threat-model review,
  maintainer approval, and required organizational CISO sign-off.
- Default metadata is timestamp, agent name, model, session ID, and repository
  relative file path. Never log raw hook payloads, file contents, transcript
  contents, full environment dumps, authorization data, or credentials.
- Checkpoint paths must resolve inside the current worktree. Reject `.git`,
  symlink escapes, and paths outside the worktree. Skip Git-ignored paths by
  default so common secret and local configuration files are not copied into
  the object database.
- Worktree-specific `byline/` administrative directories and
  `refs/worktree/byline/checkpoints` are local Git metadata and are not part of
  a commit or release archive. Normal `git push` does not publish
  `refs/notes/byline`, and normal clone/fetch does not retrieve it. Sharing
  notes is a separate, explicit user action and documentation must explain that
  notes can expose repository paths, agent/model names, session IDs, and
  timestamps.
- `git-byline ask` remains entirely local. Prompt data is never written into
  public notes or release artifacts by default.
- Hook input, issue text, pull request text, filenames, git config, repository
  contents, dependency metadata, and downloaded release files are untrusted.
  Validate paths and tags, quote shell arguments, avoid `eval`, preserve
  pipeline exit status, and fail closed on invalid state.
- `install-hooks`, `uninstall`, and `--env` mutate user configuration. They must
  show the target, preserve existing content, use atomic writes and backups,
  be idempotent, and provide a tested rollback path. Never overwrite a hook or
  shell configuration wholesale.
- Release assets contain only expected binaries, licenses, notices, and
  documentation. Produce and inspect an SBOM; MVP production code remains
  standard library plus `sergi/go-diff`, with no CGo.
- Publish `SECURITY.md` with supported-version policy, realistic response
  targets, private reporting instructions, and coordinated disclosure. Keep a
  maintainer incident runbook for workflow compromise, dependency compromise,
  leaked credentials, malicious notes, and bad releases.

## Open questions

Blocking before public bootstrap:

1. License and contributor terms. Recommended candidates: Apache-2.0 for an
   explicit patent grant, or MIT for minimum ceremony. Owner/legal decides.
2. Maintainer and backup owner for releases, security advisories, credentials,
   repository settings, and incident response.
3. Honest vulnerability response targets based on maintainer capacity.
4. Minimum supported Git version, selected after phase 1 passes against the
   oldest version containing every required command and object-format behavior.

Product decisions:

5. Prompts default OFF - confirm in the privacy review and with CISO.
6. `ask` scope: local keyword search first, embeddings later? Keyword search is
   recommended because an embedded local index keeps the no-network contract.
7. Whether users should have an explicit sanitized notes export command. Raw
   notes sharing stays manual until this is decided.
8. Upstream contribution of the `droid` preset to the agent-v1 standard -
   separate decision after MVP.

## Risks

| Risk | Mitigation |
|------|------------|
| Droid hook stdin format drift | Pinned fixtures + tolerant parser + clear error on unknown fields |
| Windows: how Droid invokes hook commands (shell selection) | Verify in phase 0-1 on a Windows box, before engine work locks in assumptions |
| Droid transcript format change | Isolate parser, fixture-pinned |
| Big files slow the chain walk | Checkpoint is per-file (hook gives path) -> diff is already narrow; add size cap with warning |
| `core.hooksPath` conflicts | Chain-don't-overwrite policy, phase 2 test with husky |
| Ignored file or escaped path enters the ODB | Resolve inside worktree, reject `.git` and symlink escapes, skip ignored paths by default |
| Public notes disclose metadata | Notes are local by default; document explicit push/fetch and consider sanitized export |
| Malicious fork reaches release credentials | No secrets on fork runs; read-only defaults; protected publishing environment |
| Compromised action or dependency | Immutable action pins, minimal dependencies, Renovate, dependency review, CodeQL, SBOM |
| Automation publishes an invalid release | Tag and artifact validation, manual first release, checksums, install smoke tests, corrective-release runbook |
| Public history contains private company data | Pre-public history, fixture, placeholder, secret, path, and license scan with owner sign-off |
| Maintainer account or release token is lost | Least privilege, backup owner, scoped bot or GitHub App, documented revocation and recovery |
