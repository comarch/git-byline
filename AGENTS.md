# AGENTS.md

## Project

<!-- PromptScript generated | source: .promptscript/project.prs | target: factory - do not edit -->

git-byline is a local AI code attribution tool for Git. One pure Go binary
tracks human, AI, and untracked authorship line by line from agent edits to
commits. Checkpoints stay local; Git notes can follow ordinary pushes
through a managed hook. No cloud, daemon, account, or telemetry. The binary
opens no network connection.

Read `README.md` and relevant files under `docs/` before changing public
behavior, data formats, security boundaries, automation, or releases.

## Tech Stack

Go 1.24+, Git 2.31+

## Architecture

cli: cmd/git-byline and internal/app, git: internal/gitcmd is the only Git subprocess boundary, model: internal/model owns versioned records and invariants, storage: internal/store and internal/lock own local durability, engine: internal/engine is pure attribution transformation, notes: internal/notes and internal/provenance own Git attribution notes, dashboard: internal/dashboard renders self-contained local HTML without network access, hooks: internal/preset and internal/hooks own agent integration, annotation, and default Git note sharing, validation: tools/validate is the complete local gate, automation: .github/workflows owns CI, security, and release automation, generated: Native instructions, agents, workflows, and hooks for nine supported hook surfaces are compiled from .promptscript, installation: Factory users run /git-byline-setup from the marketplace plugin; other users install a checksum-verified release; Git hooks share attribution notes by default unless --local-notes is set

## Context

### Runtime flow

Agent hooks send bounded JSON to `checkpoint`. Preset adapters validate the
event and paths. `internal/gitcmd` reads allowed worktree files and writes
snapshot blobs. Worktree-local state and a retention ref protect pending
provenance. After commit, `annotate` replays snapshots, validates complete
line coverage, writes canonical JSON to `refs/notes/byline`, advances state,
and compacts retention. `dashboard` renders validated blame and status data
into one self-contained local HTML file without external resources. The
managed `pre-push` hook publishes attribution notes through Git by default;
`--local-notes` removes that sharing hook.

### Data locations

- Worktree Git dir plus `byline/checkpoints.jsonl`
- Worktree Git dir plus `byline/state.json`
- Git object database for snapshot blobs
- `refs/worktree/byline/checkpoints` for local retention
- `refs/notes/byline` for per-commit attribution

### Public commands

- `checkpoint`, `annotate`, `blame`, `status`, `dashboard`
- `install-hooks`, `uninstall`
- `version`, `help`

- Project: git-byline
- Purpose: Local line-level human and AI authorship for Git
- Language: Go
- Package Manager: Go modules

## Conventions & Patterns

### Architecture

- Keep internal/engine independent from files, Git, JSON, clocks, subprocesses, and global state
- Run every production Git subprocess through internal/gitcmd using argument slices, never a shell
- Keep worktree state separate and notes common to the repository
- Version persisted formats and use deterministic serialization
- Keep hook installation idempotent, structural, backed up, and limited to managed content

### Quality

- Wrap errors with context using fmt.Errorf and %w
- Validate untrusted hook input, paths, Git output, notes, and state
- Return usage errors as exit code 2 and operational failures as exit code 1
- Preserve complete ordered non-overlapping line coverage
- Make uncertain provenance untracked instead of guessing

### Testing

- Use table-driven tests for engine, model, store, and Git boundaries
- Use temporary Git repositories and isolated configuration for integration tests
- Cover attribution changes with invariants, regression scenarios, or golden contracts
- Cover hook changes with install, preservation, idempotency, and uninstall scenarios
- Run narrow tests during work and go run ./tools/validate before merge

### Formatting

- Use gofmt for Go source
- Write code comments, documentation, configuration text, and commit messages in English
- Use ASCII hyphens instead of em dash or en dash characters
- Keep line endings and generated output deterministic

## Git Workflows

- Format: Conventional Commits
- Subject Limit: 70
- Allowed Types: feat, fix, docs, test, refactor, chore, ci, perf, revert, build

## Don'ts

- Don't add CGo
- Don't add network calls, telemetry, update checks, downloads, or remote lookups to the production binary
- Don't import os/exec outside internal/gitcmd
- Don't store raw hook payloads, prompts, transcripts, tool responses, file content, or environment dumps in checkpoint JSON or notes
- Don't read or snapshot .git, ignored paths, symlink escapes, non-regular files, binary data, or paths outside the active worktree
- Don't overwrite a different existing attribution note automatically
- Don't consume checkpoints from an unrelated base commit
- Don't hand-edit AGENTS.md or CHANGELOG.md generated by project tooling
- Don't add a dependency without source, license, necessity, security, and binary-size justification
- Don't weaken tests, coverage, fixtures, scans, format validation, or security checks to make CI pass
- Don't commit credentials, private keys, private paths, personal data, raw transcripts, or production fixtures
- Don't use mutable GitHub Action references
- Don't publish, push, release, change visibility, alter permissions, or change remote settings without explicit authorization
