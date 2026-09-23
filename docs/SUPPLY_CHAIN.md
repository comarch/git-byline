# Supply chain policy

## Source

- GitHub Actions use immutable commit SHAs with version comments.
- Renovate updates Go modules, action pins, documentation site packages, and
  configuration.
- Major updates remain manual.
- The documentation site under `website/` is a separate npm project pinned to
  exact versions by `package-lock.json`. CI installs it with `npm ci` and
  lifecycle scripts disabled. It never ships in release archives, and the
  built site loads no third-party script, font, or stylesheet.
- New Go dependencies require source, license, necessity, security, and binary
  size review.
- Runtime code remains pure Go without CGo.
- The short installer one-liners execute only scripts from this repository's
  protected `main`; checksum-first and reviewed-script alternatives are
  documented.
- The marketplace action, defined by the repository-root `action.yml` and
  mirrored byte for byte under `action/`, installs the git-byline binary from
  the pinned release repository, verifies it against the release
  `checksums.txt`, and pins every action it uses by commit SHA. Release tags are never moved;
  the moving action major tag `v1` follows each release publish and is
  documented as the convenience pin, with release tags as the immutable one.
- The forge workflow templates written by `ci install` pin one release tag,
  which Release Please rewrites on every release. The workflows download that
  release over HTTPS and verify the archive against its `checksums.txt` and
  the binary's reported version before running it. The validate gate fails
  when a template pins a release other than the manifest version.

## CI

Workflow permissions default to `contents: read`. Write access exists only in
Release Please, release publication, GitHub Pages deployment, and CodeQL jobs
that need it. Pull request workflows do not receive release credentials, and
pull requests build the documentation site without deploying it.

Required checks cover validation, operating system tests, release snapshots,
coverage, dependency review, CodeQL, and generated instruction drift.

Repository settings must enable the dependency graph, Dependabot alerts,
secret scanning, push protection, private vulnerability reporting, and
read-only default workflow permissions.

Renovate remains checks-only until a reviewed non-major update proves required
checks and rollback. CodeRabbit suggestions never auto-apply labels, assign
reviewers, or replace human approval. Codecov annotations and comments contain
coverage data only.

Release installers must validate stable version tags, use HTTPS without
fallback, verify the selected archive against `checksums.txt`, verify binary
version, avoid elevated privileges, and clean temporary files.

## Build

GoReleaser 2.18.1 builds tagged source with:

- `CGO_ENABLED=0`;
- `-trimpath`;
- stripped symbols;
- linker-injected tag version;
- deterministic commit timestamps;
- Linux, macOS, and Windows on amd64 and arm64.

Each release contains archives, `checksums.txt`, SPDX JSON SBOM documents,
license, README, and security policy. Release archives must not contain source
trees, local paths, debug files, credentials, or unlisted content.

## Access

`RELEASE_PAT` must belong to a repository-scoped GitHub App or
dedicated bot. It exists because tags created by the built-in workflow token do
not trigger the separate release workflow reliably.

Merging the Release Please pull request authorizes tag and GitHub release
creation. The `release` environment protects the separate artifact upload.
Keep a recovery owner independent from the release credential. No personal
token should be the only recovery path.

Repository rulesets must protect every `refs/tags/v*.*.*` against updates
and deletion, so a semantic release tag cannot be retargeted after
publish. The moving action major tag, like `v1`, sits outside that
pattern by design: the release workflow moves it on every publish.

## Compromise response

1. Disable affected workflows and auto-merge.
2. Identify affected commits, tags, archives, and users.
3. Revoke or rotate credentials when execution or disclosure is plausible.
4. Pin, remove, or replace the affected action or dependency.
5. Rebuild from a reviewed tag.
6. Publish a corrective patch.
7. Update this policy and the incident record without exposing secret values.
