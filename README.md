# git-byline

git-byline is an AI code attribution tool for git. It is a single static Go
binary that tracks human and AI authorship line by line, from agent edits to
commits, and stores the attribution data in git notes. It is a local-only
replacement for git-ai (usegitai.com): no cloud, no daemon, no network.

## Status

In development. The project is being built in phases described in
[PLAN.md](PLAN.md). The current milestone is the repository and module
foundation: command dispatch, help and version output, and the validation
tooling. Attribution commands (checkpoints, notes, blame rendering) land with
later phases.

## Motivation

AI coding agents produce a large share of the changes in modern codebases,
but that authorship is invisible in git history. git-byline closes the gap
between "an agent edited this file" and "these exact lines were written by
which author", using only local data:

- every line of a commit is attributed to exactly one author,
- attribution survives edits and refactors via range assignment,
- data lives in git itself (notes and objects), so there is no database and
  no server to operate.

## Non-goals

- No cloud service, no telemetry, no network calls of any kind.
- No value judgments about AI-generated code. git-byline records authorship,
  it does not score or gate code.
- No prompt or transcript storage by default. Prompts are opt-in and stay off
  until a separate security review approves them.
- No reimplementation of git. git-byline reads the same object database that
  git already maintains.

## Requirements

- Go 1.24 or newer to build from source.
- git 2.x in PATH at runtime (used by later phases for notes and object
  storage).

## Build from source

```
git clone git@github.com:mrwogu/git-byline.git
cd git-byline
go build -o git-byline ./cmd/git-byline
```

The binary is pure Go and builds with `CGO_ENABLED=0`. Release builds target
Linux, macOS, and Windows on amd64 and arm64.

## Quick start

```
git-byline version
git-byline help
```

Both commands are local only and exit with code 0 on success. The first
attribution commands arrive with the engine phases; see PLAN.md for the
roadmap.

## Development

The full validation suite runs with:

```
go run ./tools/validate
```

It formats and vets the code, runs the test suite with a coverage floor,
builds all release targets with CGO disabled, regenerates and checks
PromptScript output, and scans the tree for secrets, placeholders, private
paths, and forbidden characters.

`AGENTS.md` is a generated file: it is compiled from
`.promptscript/project.prs`. Do not edit it by hand; edit the source and
recompile with `promptscript compile --all --force`.

### Repository layout

```
cmd/git-byline/   # main, subcommand dispatch
internal/store/   # JSONL checkpoint log, git ODB blobs, state.json
internal/engine/  # snapshot chain walk, diff, range assignment
internal/notes/   # refs/notes/byline read/write
internal/blame/   # rendering (text, --json)
internal/hooks/   # install-hooks: droid, claude, git post-commit
internal/preset/  # stdin adapters: droid, claude, agent-v1
internal/prompt/  # transcript parsing (phase 4)
tools/validate/   # local validation pipeline
```

Directories for phases that are not implemented yet may be absent from the
tree until their phase starts.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). All contributions are licensed under
Apache-2.0.

## Security

See [SECURITY.md](SECURITY.md). The binary never makes network calls; a
subcommand that tries is a bug.

## License

Apache License 2.0. See [LICENSE](LICENSE).
