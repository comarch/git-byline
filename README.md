# git-byline

git-byline tracks human and AI authorship line by line from agent edits to Git
commits. Attribution stays in the local repository. No cloud service, account,
daemon, telemetry, or network connection is used by the binary.

Use it when AI agents and humans edit the same files and commit-level
authorship is not precise enough.

## Features

- Human, AI, and untracked attribution for every text line.
- Droid, Claude Code, and agent-v1 checkpoint adapters.
- Automatic annotation through a Git `post-commit` hook.
- Versioned, deterministic attribution in `refs/notes/byline`.
- Partial commit, rename, worktree, restart, and Git garbage collection
  handling.
- Text and versioned JSON blame output.
- Linux, macOS, and Windows binaries for amd64 and arm64.
- Pure Go binary with no CGo and no runtime dependencies.

## Requirements

| Component | Supported |
| --- | --- |
| Git | 2.31 or newer |
| Build toolchain | Go 1.24 or newer |
| Operating systems | Linux, macOS, Windows |
| Architectures | amd64, arm64 |
| Text files | Valid UTF-8 without NUL bytes |

Git 2.31 is the minimum because git-byline uses absolute Git path discovery.
CI tests Go 1.24 and current stable Go on Linux, plus Go 1.24 on macOS and
Windows. See [compatibility details](docs/COMPATIBILITY.md).

## Install

Download the archive for your operating system and architecture from
[GitHub Releases](https://github.com/mrwogu/git-byline/releases). Download
`checksums.txt` from the same release and verify the archive before extracting
it.

Linux:

```sh
grep ' git-byline_VERSION_linux_ARCH.tar.gz$' checksums.txt |
  sha256sum --check
tar -xzf git-byline_VERSION_linux_ARCH.tar.gz
install -m 0755 git-byline "$HOME/.local/bin/git-byline"
```

macOS:

```sh
grep ' git-byline_VERSION_macOS_ARCH.tar.gz$' checksums.txt |
  shasum -a 256 --check
tar -xzf git-byline_VERSION_macOS_ARCH.tar.gz
install -m 0755 git-byline "$HOME/.local/bin/git-byline"
```

Windows PowerShell:

```powershell
Get-FileHash .\git-byline_VERSION_windows_ARCH.zip -Algorithm SHA256
Expand-Archive .\git-byline_VERSION_windows_ARCH.zip
Copy-Item .\git-byline_VERSION_windows_ARCH\git-byline.exe `
  "$HOME\bin\git-byline.exe"
```

Compare the PowerShell hash with the matching line in `checksums.txt`. Put the
destination directory on `PATH`. Do not pipe a remote installation script into
a shell.

Build from source:

```sh
git clone https://github.com/mrwogu/git-byline.git
cd git-byline
CGO_ENABLED=0 go build -trimpath -o git-byline ./cmd/git-byline
```

## Quick start

Install Droid and the Git hook for the current repository:

```sh
git byline install-hooks --agent droid --git --user
git byline status
```

Use `--agent claude` for Claude Code or `--agent all` for both. `--user`
stores the agent hook under your home directory with the current executable
path. Use `--project` to create a portable, reviewable project configuration
that resolves `git-byline` through `PATH`.
User-scoped agent hooks quietly ignore events outside a Git worktree.

Work normally with your agent, stage selected changes, and commit:

```sh
git add src/example.go
git commit -m "feat: add example"
git byline blame src/example.go
```

Example output:

```text
human                        1 | package example
ai:droid/model-name          2 | func AddedByAgent() {}
untracked                    3 | // Predates attribution history.
```

Get machine-readable output:

```sh
git byline blame --json src/example.go
git byline status --json
```

Remove only hooks managed by git-byline:

```sh
git byline uninstall --agent droid --git --user
```

Use `--agent none --git` to manage only the Git hook.

Backups of changed agent and Git hook configuration use the
`.git-byline.bak` suffix. Existing hooks and unrelated configuration are
preserved. Symlinked and other non-regular configuration files are refused.

## Commands

| Command | Purpose |
| --- | --- |
| `checkpoint <preset>` | Record a human or AI edit snapshot from hook input |
| `annotate` | Replay pending snapshots and annotate `HEAD` |
| `blame [--json] <file>` | Show line attribution for a file at `HEAD` |
| `status [--json]` | Show checkpoint, pending, and annotation state |
| `install-hooks` | Merge Droid, Claude Code, and Git hooks |
| `uninstall` | Remove only git-byline-managed hooks |
| `version` | Print the build version |
| `help [command]` | Show command help |

Droid and Claude Code hooks call `checkpoint` automatically. The generic
agent-v1 adapter accepts its standard JSON payload through stdin:

```sh
git byline checkpoint agent-v1 --hook-input stdin
```

## How attribution works

Agent hooks capture file states before and after an edit. Git stores those
snapshots as content-addressed blobs. A local retention ref protects pending
blobs from garbage collection. After a commit, `annotate` replays snapshots,
projects provenance onto committed content, validates complete non-overlapping
line coverage, and writes canonical JSON to `refs/notes/byline`.

Lines without checkpoint history are `untracked`. Content changed after the
last agent checkpoint defaults to `human`. More detail:
[architecture and data formats](docs/ARCHITECTURE.md).

## Local data and privacy

git-byline stores:

- `.git/byline/checkpoints.jsonl`
- `.git/byline/state.json`
- pending snapshot blobs in the Git object database
- `refs/worktree/byline/checkpoints`
- `refs/notes/byline`

Linked worktrees keep checkpoint state separate. Notes are shared within the
common repository and are not pushed or fetched by normal Git operations.
Explicitly sharing notes can disclose repository paths, agent and model names,
session identifiers, and timestamps.

git-byline does not store raw hook payloads, prompts, transcripts, environment
variables, or file contents in checkpoint JSON. Snapshot blobs contain file
content and remain local unless a user explicitly transfers related refs or
Git objects.

## Limitations

- Files above 64 MiB or 1,000,000 lines are skipped.
- Binary, invalid UTF-8, symlink, submodule, device, and ignored paths are
  skipped.
- Rebase and amend leave existing notes on old commit IDs.
- Merge commits use first-parent history. Unmatched merge result content is
  `untracked`.
- Duplicate equal lines in partial commits are resolved deterministically, but
  Git provides no staging timestamp to prove which duplicate was selected.
- If several commits complete without annotation, git-byline refuses to guess
  across the gap.
- Prompts and transcripts are not stored.

## Troubleshooting

`git byline status` reports pending checkpoints, retained snapshots, and
whether `HEAD` is annotated.

If a user or Git hook cannot find the executable, reinstall hooks using the
final binary location. Project agent hooks resolve `git-byline` through the
agent process `PATH`.

If annotation reports a commit gap, do not delete `.git/byline` or rewrite
notes. Preserve the repository and open a redacted bug report with commit IDs
shortened or replaced.

If blame shows `untracked`, check that agent and Git hooks are installed, then
make a new checkpointed edit and commit. Existing history remains honestly
untracked.

## Development

Run the complete local validation contract:

```sh
go run ./tools/validate
```

It checks formatting, vet, tests, at least 80 percent statement coverage,
CGO-free builds for all six targets, dependency and import policy,
PromptScript drift, secrets, placeholders, private paths, and forbidden
characters.

See [CONTRIBUTING.md](CONTRIBUTING.md), [validation](docs/VALIDATION.md), and
[release procedure](docs/RELEASES.md).

## Support and security

Use the bug form for reproducible defects and GitHub Discussions for usage
questions. Never include private repository content, raw hook input, prompts,
transcripts, credentials, or full environment dumps.

Report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/mrwogu/git-byline/security/advisories/new).
See [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE).
