# Security model

## Assets

- source and release integrity;
- local file snapshots in the Git object database;
- checkpoint and pending attribution metadata;
- `refs/notes/byline`;
- managed Git `pre-push` note sharing;
- generated local HTML dashboards;
- user and project agent configuration;
- Git hooks and shell configuration;
- maintainer credentials and GitHub settings;
- published archives, checksums, and SBOMs.

## Trust boundaries

Hook input, paths, patch text, Git configuration, repository content, issue
text, pull request code, dependencies, downloaded tools, and release files are
untrusted.

The production binary does not open network connections. Network packages are
forbidden in production code and checked by validation. The managed
`pre-push` hook invokes Git to publish attribution notes to the selected
remote unless installation used `--local-notes`.

The managed `reference-transaction` hook has the opposite failure contract.
It exits immediately when `GIT_BYLINE_NESTED` is set, ignores refs outside
`HEAD`, `refs/heads/*`, and `refs/stash`, and always exits zero. This hook runs
on the critical path of commands such as `git status`, reset, stash, and
branch switching. The wrapper keeps the never-nonzero contract; the binary
parses and filters transaction lines before repository discovery. A provenance
failure must not break repository operations.
The `post-rewrite` and `post-merge` hooks fail closed because their work is
recoverable and an incomplete rewrite could otherwise leave attribution
silently detached. `post-checkout` also fails open because checkout must not
be blocked by pending attribution repair.

## Threats and controls

| Threat | Prevention | Detection | Recovery |
| --- | --- | --- | --- |
| Path escapes worktree | Normalize path, reject `.git`, traversal, symlink escape | Path tests and warnings | Skip snapshot, preserve prior state |
| Secret file enters Git ODB | Skip Git-ignored and non-regular files | Repository scans and review | Remove local object after retention ends |
| Malicious hook payload | Bounded input, strict event and metadata validation, path validation | Parser tests and explicit errors | No checkpoint written |
| Git command injection | Argument slices, no shell, bounded output | Fake and integration command tests | Fail operation without state advance |
| Checkpoint loss during Git GC | Retention ref written before JSONL append | Aggressive GC integration test | Reuse protected blob |
| Oversized local state exhausts memory | Stream checkpoint reads, cap logs at 64 MiB and 100,000 records, cap state at 64 MiB | Limit regression tests | Preserve and inspect local state before repair |
| Concurrent state corruption | Advisory locks and fixed lock order | Contention tests | Process exit releases locks |
| Partial persistence | Idempotent note writes and atomic state replacement | Retry scenarios | Rerun `annotate` |
| Malicious or stale note | Version and blob validation | Warning and untracked fallback | Preserve note for manual review |
| Oversized attribution note exhausts memory | Reject notes above 16 MiB or 500 files before full decode | Note limit regression tests | Inspect or replace the invalid note |
| Attribution metadata is shared unexpectedly | Installation output and documentation disclose default note sharing; `--local-notes` opts out | Inspect managed `pre-push` hook and remote notes ref | Reinstall with `--local-notes`, then remove remote notes deliberately |
| Note publication fails | Managed pre-push exits non-zero and stops the branch push | Git push error | Fix remote access or opt out with `--local-notes` |
| Reference transaction attribution fails | Nested and irrelevant refs exit immediately; all other failures are swallowed and hook exits zero | Hook and rewrite tests | Run `git byline rewrite` manually after the repository operation |
| Dashboard content injects HTML or script | Validate attribution, escape untrusted values with `html/template`, restrictive CSP | Renderer and CLI tests | Delete report and regenerate |
| Dashboard exposes source or metadata | Private temporary file mode, no external resources, no automatic publication | User review and repository scans | Delete local report |
| Hook config overwrite | Structural merge, backup, managed markers | Idempotency and preservation tests | Restore `.git-byline.bak` |
| Fork reaches write token | Read-only workflow defaults, no secrets in CI | Fork pull request check | Disable workflow and rotate token |
| Compromised action or tool | Immutable action pins and pinned tool versions | Renovate, dependency review, CodeQL | Pause automation, pin or remove tool |
| Invalid release | Tag validation, tests, archive checks, checksums, SBOM | Release workflow and manual install | Withdraw release and publish patch |

## Stored data

Checkpoint JSON includes repository-relative paths, timestamps, agent names,
model names, session identifiers, and Git object IDs. Notes contain the same
metadata plus line ranges. Stash ownership notes contain only a SHA-256 digest
of the exact stash attribution note. Snapshot blobs contain full selected file
content.

Raw hook payloads, prompts, transcripts, tool responses, environment variables,
authorization data, and logs are not persisted.

Generated dashboards contain committed source lines, repository-relative
paths, commit and blob IDs, attribution states, agent and model names, and
local status. Default reports use a private temporary file. Explicit output
paths are created with mode `0600` where supported and never replace an
existing file. Reports are self-contained and make no network requests.

After Git hooks are installed, ordinary pushes publish the notes ref before
the branch ref. A later branch rejection can therefore leave note metadata on
the remote before its target commit arrives. Normal fetches still do not
retrieve notes without an explicit refspec. Worktree retention refs are never
published by managed hooks.

## Residual risks

- A user can explicitly transfer local metadata or snapshot objects.
- A successful notes push can precede a rejected branch push.
- Duplicate equal lines cannot prove staging order.
- First-parent merge attribution cannot prove content from other parents.
- A compromised local account can read Git objects and hook configuration.
- A user can copy or publish a generated dashboard containing source and
  attribution metadata.
- A compromised maintainer token can alter public source or releases until
  repository controls contain it.

Changes to storage, notes, hooks, releases, or the no-network rule require
security review.
