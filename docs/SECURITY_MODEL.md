# Security model

## Assets

- source and release integrity;
- local file snapshots in the Git object database;
- checkpoint and pending attribution metadata;
- `refs/notes/byline`;
- user and project agent configuration;
- Git hooks and shell configuration;
- maintainer credentials and GitHub settings;
- published archives, checksums, and SBOMs.

## Trust boundaries

Hook input, paths, patch text, Git configuration, repository content, issue
text, pull request code, dependencies, downloaded tools, and release files are
untrusted.

The production binary has no network boundary. Network packages are forbidden
in production code and checked by validation.

## Threats and controls

| Threat | Prevention | Detection | Recovery |
| --- | --- | --- | --- |
| Path escapes worktree | Normalize path, reject `.git`, traversal, symlink escape | Path tests and warnings | Skip snapshot, preserve prior state |
| Secret file enters Git ODB | Skip Git-ignored and non-regular files | Repository scans and review | Remove local object after retention ends |
| Malicious hook payload | Bounded input, strict event and metadata validation, path validation | Parser tests and explicit errors | No checkpoint written |
| Git command injection | Argument slices, no shell, bounded output | Fake and integration command tests | Fail operation without state advance |
| Checkpoint loss during Git GC | Retention ref written before JSONL append | Aggressive GC integration test | Reuse protected blob |
| Concurrent state corruption | Advisory locks and fixed lock order | Contention tests | Process exit releases locks |
| Partial persistence | Idempotent note writes and atomic state replacement | Retry scenarios | Rerun `annotate` |
| Malicious or stale note | Version and blob validation | Warning and untracked fallback | Preserve note for manual review |
| Hook config overwrite | Structural merge, backup, managed markers | Idempotency and preservation tests | Restore `.git-byline.bak` |
| Fork reaches write token | Read-only workflow defaults, no secrets in CI | Fork pull request check | Disable workflow and rotate token |
| Compromised action or tool | Immutable action pins and pinned tool versions | Renovate, dependency review, CodeQL | Pause automation, pin or remove tool |
| Invalid release | Tag validation, tests, archive checks, checksums, SBOM | Release workflow and manual install | Withdraw release and publish patch |

## Stored data

Checkpoint JSON includes repository-relative paths, timestamps, agent names,
model names, session identifiers, and Git object IDs. Notes contain the same
metadata plus line ranges. Snapshot blobs contain full selected file content.

Raw hook payloads, prompts, transcripts, tool responses, environment variables,
authorization data, and logs are not persisted.

Normal Git operations do not publish the notes or worktree retention ref.
Users can explicitly push unusual refs, so these values remain sensitive local
data.

## Residual risks

- A user can explicitly transfer local metadata or snapshot objects.
- Duplicate equal lines cannot prove staging order.
- First-parent merge attribution cannot prove content from other parents.
- A compromised local account can read Git objects and hook configuration.
- A compromised maintainer token can alter public source or releases until
  repository controls contain it.

Changes to storage, notes, hooks, releases, or the no-network rule require
security review.
