# Validation contract

One command runs the complete local gate:

```sh
go run ./tools/validate
```

It runs with `CGO_ENABLED=0` and `GOPROXY=off`.

## Stages

1. `gofmt` drift check.
2. `go vet ./...`.
3. Full unit, contract, and integration tests.
4. Statement coverage with an 80 percent floor.
5. CGO-free builds for Linux, macOS, and Windows on amd64 and arm64.
6. Standard-library-only dependency policy.
7. Forbidden production import policy.
8. PromptScript 1.18.1 strict validation and generated output drift check.
9. Repository scans for credentials, unfinished markers, private paths, and
   forbidden dash characters.

The command fails on the first stage failure. Tests use temporary repositories
and do not need credentials, network access, personal Git configuration, or
untracked project files.

## Narrow commands

```sh
go test ./internal/engine
go test ./internal/provenance
go test ./internal/hooks
go vet ./...
CGO_ENABLED=0 go build ./...
```

Run package tests during development. Run the complete contract before every
pull request.

## CI checks

Stable pull request check names:

```text
Validate repository
Quality and build
Coverage
Dependency review
CodeQL
Validate PromptScript
```

`Quality and build` aggregates the operating system test matrix and verified
GoReleaser snapshot. The snapshot must contain six archives, six SPDX JSON
SBOM files, and a non-empty SHA256 checksum file.

Nightly robustness repeats randomized attribution scenarios and fuzzes line
splitting and Droid payload parsing. Nightly checks are not branch requirements
because they do not conclude on every pull request.

## Coverage policy

- Total statement coverage must remain at least 80 percent.
- Codecov patch coverage target is 80 percent.
- Project coverage may drop by at most 1 percent.
- Parsing, path validation, storage recovery, hooks, and attribution
  invariants need direct review even when aggregate coverage passes.
- Never lower a threshold only to pass a change.

## Release validation

Release changes also run:

```sh
goreleaser release --snapshot --clean
```

Inspect archive names, file allowlist, version output, checksums, and SBOMs.
See [RELEASES.md](RELEASES.md).
