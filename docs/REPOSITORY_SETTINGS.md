# GitHub repository settings

Local files cannot enforce these settings. Apply and verify them before public
contributions or release publication.

## General

- Description: `Local line-level human and AI authorship for Git`
- Topics: `git`, `go`, `ai`, `attribution`, `developer-tools`, `cli`
- Default branch: `main`
- Issues: enabled
- Discussions: enabled with maintainer moderation
- Wiki and Projects: disabled unless actively maintained
- Squash merge and auto-merge: enabled
- Merge commits and rebase merge: disabled
- Head branches: deleted after merge

## Security

- Dependency graph and Dependabot alerts enabled
- Dependabot security updates enabled
- Secret scanning and push protection enabled
- Private vulnerability reporting enabled
- CodeQL enabled
- Default workflow token permission set to read-only
- Fork pull request workflows require approval according to repository risk

## Integrations

- Install the Renovate GitHub App with access limited to this repository.
- Activate this repository in Codecov.
- Keep Codecov upload tokenless and use the workflow OIDC permission.
- Verify Codecov reports the expected project and patch status names before
  making them required.

## Default branch ruleset

Name: `Protect default branch`

Rules:

- pull request required;
- one approving review;
- CODEOWNERS review required;
- stale approvals dismissed;
- latest push approved;
- conversations resolved;
- branch up to date before merge;
- linear history required;
- force pushes blocked;
- branch deletion blocked;
- no broad administrator bypass.

Required check names:

```text
Validate repository
Quality and build
Coverage
Dependency review
CodeQL
Validate PromptScript
```

Enable a required check only after a same-repository pull request and a fork
pull request prove that exact check always reports a conclusion.

## Release

- Create protected `release` environment.
- Require maintainer approval for publication.
- Add repository-scoped `RELEASE_PLEASE_TOKEN`.
- Keep `GITHUB_TOKEN` as the artifact publishing token.
- Add a second recovery owner for release settings and private advisories.
- Keep bypass actors empty unless a tested bot path needs a narrow exception.

## Labels

Create:

```text
bug
enhancement
security
dependencies
breaking
automerge
autorelease: pending
autorelease: tagged
needs-reproduction
documentation
```

## Verification pull requests

1. Open a representative same-repository pull request.
2. Confirm templates and CODEOWNERS.
3. Confirm all required checks and Codecov status.
4. Confirm a major Renovate update cannot auto-merge.
5. Open a fork pull request.
6. Confirm it receives no write token or release credential.
7. Confirm every required check concludes.
8. Test Release Please without publishing.
9. Verify release environment blocks unapproved publication.
