# Contributing to git-byline

Thank you for investing time in git-byline. This document describes how to
set up a development environment and what every change must pass before
review.

## Prerequisites

- Go 1.24 or newer.
- git 2.x.
- The PromptScript CLI, version 1.18.1 (pinned). Needed to regenerate
  AGENTS.md. Other versions are not validated against this repository.

## Setup

```
git clone git@github.com:mrwogu/git-byline.git
cd git-byline
go build ./...
go test ./...
```

## Validation

Run the full local pipeline before every push:

```
go run ./tools/validate
```

The pipeline checks, in order: formatting, vet, tests with a coverage floor,
CGO-free builds for all six release targets, PromptScript strict validation
with a drift check against the compiled AGENTS.md, and repository scans
(secrets, placeholders, private paths, forbidden characters, dependency
policy). A failing stage stops the pipeline. Never bypass a failing check;
fix the cause.

## Generated files

`AGENTS.md` is generated from `.promptscript/project.prs`. Never edit it
directly. Change the source file instead, then regenerate:

```
promptscript compile --all --force
```

The validation pipeline fails when the committed AGENTS.md does not match
what the source compiles to, so hand edits cannot slip through review.

## Commit message style

Conventional Commits, subject line at most 70 characters. Use the types
`feat:`, `fix:`, `test:`, `chore:`, and `build:`. Comments, commit messages,
documentation, and code are written in English.

Example:

```
feat: add version command with linker-injected version string
```

## Code and testing conventions

- Table-driven tests for engine and store code; property or golden tests
  where invariants demand them.
- Integration scenarios run against temporary git repositories created for
  the test, never against your working repositories.
- Errors are wrapped with context using `fmt.Errorf` and `%w`, never
  swallowed.
- Pure Go only: no CGo, no new dependencies without a written justification
  in the pull request. The binary must never make network calls.
- Do not use em dashes or en dashes anywhere in the repository; use plain
  hyphens.

## Pull requests

1. Create a branch from `main`.
2. Make your change with tests covering the new behavior.
3. Run `go run ./tools/validate` and fix everything it reports.
4. Open a pull request describing what changed and why, and link the issue
   it resolves when one exists.

## License

By contributing, you agree that your contributions are licensed under the
Apache License 2.0.
