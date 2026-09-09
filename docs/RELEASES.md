# Release procedure

git-byline uses SemVer, Release Please, and GoReleaser.

## Version policy

- Tags use `vMAJOR.MINOR.PATCH`.
- Before 1.0, `feat` and breaking changes increment minor.
- `fix` increments patch.
- `fix(deps)` increments patch.
- Development tooling and GitHub Action updates do not create product
  releases.
- Release Please owns release pull requests, `CHANGELOG.md`, versions, and
  tags.
- The binary receives its version from the validated tag at link time. Local
  builds report `dev`.

## Automation level

The repository starts at checks-only automation:

- Renovate may open pull requests.
- Humans review and merge dependency updates.
- Humans merge release pull requests.
- Merging a release pull request authorizes Release Please to create the tag
  and GitHub release.
- The protected `release` environment separately gates binary, checksum, and
  SBOM upload.

Enable dependency auto-merge only after a real non-major update proves all
required checks and rollback. Enable release pull request auto-merge only after
one successful manual release.

## Required configuration

- `RELEASE_PAT`: repository-scoped GitHub App or dedicated bot token.
- `release` environment: artifact upload protection and required reviewer.
- Branch rules from [REPOSITORY_SETTINGS.md](REPOSITORY_SETTINGS.md).

Release Please can be disabled by removing the repository secret or disabling
`release-please.yml`. Artifact upload can be disabled by protecting or
disabling the `release` environment and `release.yml`. Disable both workflows
to stop all release publication.

## Release steps

1. Review Conventional Commits on `main`.
2. Confirm all required checks pass.
3. Review and merge the Release Please pull request.
4. Confirm the generated tag points to the expected commit.
5. Approve the protected release environment.
6. Let `release.yml` build from the exact tag.
7. Verify six archives, six SBOM files, and `checksums.txt`.
8. Install one artifact per supported operating system.
9. Run `git-byline version` and compare it with the tag.
10. Review release notes and publish state.

If the tag event does not start publication, dispatch `release.yml` manually
with the existing tag. The workflow validates the tag and checks out that exact
ref.

## Local dry run

```sh
go run ./tools/validate
syft version
goreleaser release --snapshot --clean
```

Inspect `dist/`:

```text
git-byline_VERSION_linux_amd64.tar.gz
git-byline_VERSION_linux_arm64.tar.gz
git-byline_VERSION_macOS_amd64.tar.gz
git-byline_VERSION_macOS_arm64.tar.gz
git-byline_VERSION_windows_amd64.zip
git-byline_VERSION_windows_arm64.zip
checksums.txt
six archive SBOM files
```

Each archive contains only the binary, `LICENSE`, `README.md`, and
`SECURITY.md`.

## Verification

- Tag matches `vMAJOR.MINOR.PATCH`.
- Binary output matches the tag.
- Checksums verify every archive.
- SBOM exists and is non-empty for every archive.
- Archive contents match the allowlist.
- No local path, secret, debug file, or source cache appears.
- Installation documentation matches actual names.

## Bad release

1. Stop further publication and auto-merge.
2. Mark the release as withdrawn or draft.
3. Do not delete a consumed tag.
4. Fix source or release configuration.
5. Publish a corrective patch.
6. Document impact and upgrade guidance.
