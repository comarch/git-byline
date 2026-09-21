# git-byline forge attribution

Reconstruct human, AI, and untracked line-level attribution after a GitHub
squash or rebase merge, and publish it to `refs/notes/byline`.

Squash and rebase merges create commits that never passed a local
git-byline hook. This action fetches the merged pull request commits and
the existing attribution notes, reconstructs attribution with a
checksum-verified git-byline release binary, and pushes the notes ref
through Git. It needs only `contents: write`, uses immutable action
pins, and pushes only `refs/notes/byline`.

## Usage

Add one step to a workflow that runs after a merged pull request:

```yaml
name: git-byline forge attribution

on:
  pull_request:
    types:
      - closed
    branches:
      - main

concurrency:
  group: git-byline-notes-${{ github.repository }}-${{ github.event.pull_request.base.ref }}
  cancel-in-progress: false

permissions:
  contents: write

jobs:
  reconstruct:
    if: github.event.pull_request.merged == true
    runs-on: ubuntu-latest
    steps:
      - uses: comarch/git-byline/action@v1
```

The workflow, not the action, decides when attribution is reconstructed,
so keep the `if`, `permissions`, and `concurrency` blocks as shown.

## Inputs

| Input | Default | Description |
| --- | --- | --- |
| `version` | `latest` | git-byline release tag to install, like `v1.3.0`. Pin a release tag for reproducible runs. |
| `token` | `github.token` | Token used for Git fetch and push. The default needs the calling workflow to grant `contents: write`. |

## Versioning

GitHub Actions has no `@latest` reference, so the action is reached
through Git refs. The moving major tag `v1` follows every 1.x release and
is the easiest pin. For immutable runs, pin a release tag like `@v1.3.0`
or the commit SHA behind it; release tags are never moved. The `v1` tag
exists from the first release that ships the action.

## Security

- The binary is downloaded from the pinned git-byline release repository
  and verified against the release `checksums.txt` before it runs.
- System, global, and user Git configuration is disabled, repository
  hooks are disabled, and the token is passed only to Git through an
  in-memory header, never to the binary.
- The push is limited to `refs/notes/byline` with `--no-verify`.
- Attribution notes contain line ranges, paths, agent and model names,
  human identity tokens, session identifiers, and timestamps. They never
  contain prompts, transcripts, tool responses, file content, or
  environment dumps.

See the repository [security model](../docs/SECURITY_MODEL.md) for the
full trust boundary.

## Attribution without this action

`git byline install-hooks --git` writes a workflow that builds git-byline
from your merge commit and reconstructs attribution without any
marketplace dependency. Both paths write the same notes. Use the action
when you prefer a versioned binary over running repository code.
