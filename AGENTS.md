# AGENTS.md

## Project

<!-- PromptScript generated | source: .promptscript/project.prs | target: factory - do not edit -->

git-byline is an internal AI code attribution tool for git. Single static Go
binary that tracks human/AI authorship line-by-line, from agent edits to
commits, stored in git notes. Replacement for git-ai (usegitai.com); no
cloud, no daemon, no network.

Read `PLAN.md` before starting any work here. It is the source of truth for
scope, data model, and phases.

## Project layout

```
cmd/git-byline/   # main, subcommand dispatch
internal/store/   # JSONL checkpoint log, git ODB blobs, state.json
internal/engine/  # snapshot chain walk, diff, range assignment
internal/notes/   # refs/notes/byline read/write
internal/blame/   # rendering (text, --json)
internal/hooks/   # install-hooks: droid, claude, git post-commit
internal/preset/  # stdin adapters: droid, claude, agent-v1
internal/prompt/  # transcript parsing (phase 4)
```

### Data locations

- `.git/byline/checkpoints.jsonl` - append-only checkpoint log
- `.git/byline/state.json` - last annotated commit
- git ODB - file snapshots (via `git hash-object -w`)
- `refs/notes/byline` - per-commit authorship notes

### Definition of done

1. `go test ./...` green.
2. New behavior covered by a test (engine: property or golden; hooks: scenario).
3. No new dependency without justification.
4. PLAN.md updated if scope, format, or an accepted limitation changed.

## Conventions

### Toolchain

- Go 1.24+. Modules, no vendoring.

### Commits

- Comments and commit messages in English. Conventional Commits, subject max 70 chars (`feat:`, `fix:`, `test:`, `chore:`, `build:`).

### Error Handling

- Errors wrapped with context (`fmt.Errorf` + `%w`), never swallowed.

### Testing

- All engine and store code gets table-driven tests. Integration scenarios run against temp git repos, never the developer's working repos.

### Fixtures

- Hook fixtures (`internal/preset/testdata/`) are pinned real captures. When Droid hook stdin changes, update fixtures deliberately - do not edit them to make tests pass.

### Hooks

- `install-hooks` must be idempotent and merge with existing hooks. Never overwrite a user's hook configuration.

## Development Commands

```
  go build ./...            # build all
  go test ./...             # full test suite - run before every commit
  go test ./internal/engine -run TestInvariants   # narrow run during engine work
  goreleaser release --snapshot --clean            # local release dry-run, 6 targets
```

## Hard rules

- Pure Go. No CGo anywhere. No `mattn/go-sqlite3`.
- No network calls. The binary never dials out. A subcommand that tries is a bug.
- Keep the binary small: stdlib + `sergi/go-diff` in MVP. Every new dependency needs a written justification in the PR.
- Prompts and transcripts are opt-in and OFF by default. Do not store them without the phase-4 CISO sign-off.
- Notes format is versioned (`version` field). Readers tolerate unknown versions with a warning, never crash.
- Attribution invariants are non-negotiable: complete line coverage, no overlapping ranges. Property tests must pass.
