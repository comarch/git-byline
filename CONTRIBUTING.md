# Contributing to git-byline

Contributions are welcome. Keep changes focused, test public behavior, and
protect local repository data.

## Prerequisites

- Go 1.24 or newer.
- Git 2.31 or newer.
- PromptScript CLI 1.18.1.
- GoReleaser 2.18.1 when release configuration changes.
- Syft on `PATH` when building release snapshots.

## Setup

```sh
git clone https://github.com/mrwogu/git-byline.git
cd git-byline
go test ./...
go build ./...
```

Tests create isolated temporary repositories. They must not use your working
repository, home configuration, credentials, or live services.

## Validation

Run narrow package tests while working:

```sh
go test ./internal/engine
go test ./internal/provenance
```

Before opening a pull request, run:

```sh
go run ./tools/validate
```

Do not bypass a failing stage, lower coverage, weaken a fixture, or skip a
required check to get a green result. See [docs/VALIDATION.md](docs/VALIDATION.md).

## Code rules

- Pure Go. CGo is forbidden.
- Production code must not import network packages.
- `os/exec` is allowed only in `internal/gitcmd`.
- Validate paths and untrusted hook input at the boundary.
- Wrap errors with context using `fmt.Errorf` and `%w`.
- Keep the attribution engine independent from Git, files, JSON, clocks, and
  process state.
- Keep comments short and in English.
- Use ASCII hyphens, not em dash or en dash characters.
- Add no dependency without necessity, license, source, binary size, and
  security justification in the pull request.

Engine and store work uses table-driven tests. Attribution behavior needs
property, golden, or integration coverage. Hook changes need isolated
configuration scenarios. Every bug fix includes a minimal regression test.

Fixtures must be minimal, deterministic, synthetic, or redacted. Never commit
raw agent captures, transcripts, private repository paths, tokens, complete
environment dumps, or production data.

## Generated files

`AGENTS.md` and `.factory/droids/*.md` are generated from `.promptscript/`.
Edit PromptScript sources, then:

```sh
promptscript validate --strict .promptscript/project.prs
promptscript compile --all --force
```

Review the generated diff. Never edit generated instruction files directly.

Release Please owns `CHANGELOG.md`, release versions, release pull requests,
and tags. Do not hand-edit release output except while bootstrapping an empty
changelog.

## Branches and commits

Create a focused branch from `main`. Use Conventional Commits with an
imperative subject, no trailing period, and at most 70 characters.

Allowed types:

```text
feat fix docs test refactor chore ci perf revert build
```

Use `fix(deps)` for runtime dependency updates, `chore(deps-dev)` for
development tools, and `chore(ci)` for action updates.

## Pull requests

Every pull request must explain:

- behavior and rationale;
- compatibility and migration impact;
- exact validation commands and results;
- tests and documentation;
- generated artifact impact;
- release impact;
- security and privacy impact.

Link an issue for non-trivial work. CODEOWNERS review is required for
automation, security, release, storage, notes, Git execution, and hook paths.
Squash merge is the default.

Contributions are licensed under Apache-2.0.
