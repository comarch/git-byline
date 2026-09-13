# Installation

git-byline supports Linux, macOS, and Windows on amd64 and arm64. Every
prebuilt archive is covered by `checksums.txt`.

## Pick your path

| You use | Path |
| --- | --- |
| Factory, Claude Code, or Gemini CLI | [agent package](#agent-packages), one command |
| Copilot, VS Code Agent, Cursor, Codex, Windsurf, or Grok | [agent templates](#agents-without-a-package) |
| No agent, or a CI runner | [release installer](#linux-and-macos) |
| Air-gapped or contributing | [Go toolchain](#go-toolchain) |

Every path installs the same local executable. Git `post-commit` hooks must
work when no agent session is running, so an in-process runtime alone cannot
provide complete attribution.

## Agent packages

Three agents install git-byline as a package. Each ships the same
`/git-byline-setup` command, which selects the operating-system installer,
verifies the release archive, installs the local runtime, installs the Git
hooks, and installs the agent hook.

### Factory

```text
droid plugin marketplace add comarch/git-byline
droid plugin install git-byline@git-byline --scope user
```

Start Droid in a repository and run `/git-byline-setup`.

### Claude Code

```text
/plugin marketplace add comarch/git-byline
/plugin install git-byline@git-byline
```

Then run `/git-byline-setup`. Factory and Claude Code load the same plugin
directory, `marketplace/git-byline`, which carries a manifest per agent under
`.factory-plugin/` and `.claude-plugin/` and shares its `commands/` and
`skills/`.

### Gemini CLI

```sh
gemini extensions install https://github.com/comarch/git-byline
```

Restart the CLI, then run `/git-byline-setup`. Gemini installs an extension
from a repository root, so the manifest is `gemini-extension.json` and the
command is `commands/git-byline-setup.toml`, both at the root of this
repository.

After any of the three, verify:

```sh
git-byline version
git-byline status
```

## Agents without a package

Copilot, VS Code Agent, Cursor, Codex, Windsurf, and Grok have no package
manager that installs from a repository. Two files give them the same result,
and both are checked in under
[`marketplace/harness`](../marketplace/harness/README.md):

1. a setup command, so the agent gains `/git-byline-setup`;
2. a hook file, so the agent reports its edits.

| Agent | Setup command goes to | Hook file goes to |
| --- | --- | --- |
| GitHub Copilot | `.github/prompts/git-byline-setup.prompt.md` | `.github/hooks/promptscript.json` |
| VS Code Agent | `.github/prompts/git-byline-setup.prompt.md` | `.github/hooks/promptscript-vscode.json` |
| Cursor | `.cursor/commands/git-byline-setup.md` | `.cursor/hooks.json` |
| Codex | `$HOME/.codex/prompts/git-byline-setup.md` | `.codex/hooks.json` |
| Windsurf | `.windsurf/workflows/git-byline-setup.md` | `.windsurf/hooks.json` |
| Grok | the prompt directory your build reads | `.grok/hooks/promptscript.json` |

For example, Cursor:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  -o .cursor/commands/git-byline-setup.md --create-dirs \
  https://raw.githubusercontent.com/comarch/git-byline/main/marketplace/harness/cursor/git-byline-setup.md
```

Then run `/git-byline-setup` in Cursor, which installs the binary, the Git
hooks, and `.cursor/hooks.json`.

Each hook file is byte-identical to the one this repository generates for
itself, and validation fails when they drift apart. The hook bodies carry no
absolute path: they resolve the project root with
`git rev-parse --show-toplevel` and find `git-byline` through `PATH`.

`.gemini/settings.json` and `.claude/settings.json` can already hold
unrelated settings, so merge their `hooks` block instead of replacing the
file.

Without the agent hook, git-byline still annotates commits, but agent edits
arrive as plain `human` lines rather than `ai` lines, because nothing
observed them.

## Git hooks

The Git side is identical for every agent:

```sh
git-byline install-hooks --agent none --git --project
```

`--agent droid` and `--agent claude` additionally install those two
user-level or project-level agent hooks; the other agents use the template
files above.

Git hook installation adds a managed `pre-push` hook. It publishes
`refs/notes/byline` to the same remote before each ordinary push.
Attribution notes contain repository paths, line ranges, agent and model
names, human identity tokens, session identifiers, timestamps, and blob IDs.
To disable automatic note sharing:

```sh
git-byline install-hooks --agent none --git --project --local-notes
```

## Linux and macOS

Fast install:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  https://raw.githubusercontent.com/comarch/git-byline/main/install.sh | bash
```

The script supports Linux and macOS on amd64 and arm64. It downloads the
matching release archive and `checksums.txt`, verifies SHA-256 and binary
version, installs to `$HOME/.local/bin`, and adds the current repository Git
hook when run inside a worktree.

It then detects the coding agents present on the machine, by their command
name or their configuration directory, and never writes during detection:

- Factory and Claude Code get a user-level agent hook, which covers every
  repository, because git-byline installs those two itself.
- Every other detected agent reads a project hook file that git-byline does
  not own. The installer prints the one command that copies that file into
  the current project and does not guess a user-level path for it.

Pass `--no-agent-hooks` to skip detection. The per-agent files and their copy
targets are listed in
[agent setup templates](../marketplace/harness/README.md).

Override defaults:

```sh
GIT_BYLINE_VERSION=v0.1.0 \
GIT_BYLINE_BIN_DIR="$HOME/bin" \
sh install.sh --no-git-hook --no-agent-hooks
```

## Windows PowerShell

Fast install:

```powershell
irm https://raw.githubusercontent.com/comarch/git-byline/main/install.ps1 | iex
```

The script supports Windows on amd64 and arm64. It performs the same checksum
and binary-version checks, installs to `$HOME\bin`, and runs the same agent
detection. Pass `-NoAgentHooks` to skip it.

Run a reviewed local script with custom options:

```powershell
Invoke-WebRequest `
  https://raw.githubusercontent.com/comarch/git-byline/main/install.ps1 `
  -OutFile install.ps1
Get-Content .\install.ps1
.\install.ps1 -Version v0.1.0 -BinDir "$HOME\bin" -NoGitHook
```

## Checksum-first manual install

Download the archive and `checksums.txt` from the same
[GitHub release](https://github.com/comarch/git-byline/releases).

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

Windows:

```powershell
Get-FileHash .\git-byline_VERSION_windows_ARCH.zip -Algorithm SHA256
Expand-Archive .\git-byline_VERSION_windows_ARCH.zip
Copy-Item .\git-byline_VERSION_windows_ARCH\git-byline.exe `
  "$HOME\bin\git-byline.exe"
```

Compare the Windows hash with the exact archive line in `checksums.txt`.

## Update

Re-running the same one-liner is the update path. The installer compares the
release version with the binary it is about to replace: when they match it
prints `git-byline ... is already installed at ...` and exits without
downloading anything. When they differ it installs the new release over the
old binary and re-runs the hook installs, so managed hook blocks refresh to
the new version. No backup file is kept; the installer reports the version it
replaced.

Go installs update through the toolchain:

```sh
go install github.com/comarch/git-byline/cmd/git-byline@latest
```

Agent packages update by re-running their `/git-byline-setup` command.

### Air-gapped update

Download the release archive and `checksums.txt` yourself, then let the
binary verify and swap itself with no network access:

```sh
git-byline update --archive git-byline_1.0.0_linux_amd64.tar.gz \
  --checksums checksums.txt
```

Windows uses the matching `.zip` release; the command rejects an archive
built for another operating system, because a checksummed cross-system binary
would still install a binary that cannot run. `--dry-run` verifies the
archive without replacing anything. The command enforces the same rules as the
installers: SHA-256 from `checksums.txt`, the exact archive allowlist, no
symlink in place of the binary, and a regular-file check. On Windows the
previous binary stays as `git-byline.exe.old` until the running process
exits. After any update, confirm with `git-byline version`.

### Automatic updates

git-byline ships no daemon and the binary performs no update check, so an
automatic update is a scheduled re-run of the installer, which is a no-op
when the version is current.

Linux and macOS, weekly with cron:

```text
17 8 * * 1 curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 https://raw.githubusercontent.com/comarch/git-byline/main/install.sh | sh >/dev/null
```

Windows, weekly with Task Scheduler:

```powershell
schtasks /Create /TN "git-byline update" /SC WEEKLY /ST 08:17 /TR 'powershell -NoProfile -Command \"irm https://raw.githubusercontent.com/comarch/git-byline/main/install.ps1 | iex\"'
```

## Go toolchain

With Go 1.24 or newer:

```sh
go install github.com/comarch/git-byline/cmd/git-byline@latest
git-byline install-hooks --agent droid --git --user
```

Use `--agent claude` or `--agent all` when those user-level adapters are
needed. Repositories containing PromptScript-generated native hooks need only:

```sh
git-byline install-hooks --agent none --git --project
```

Add `--local-notes` to disable managed pre-push publication. Users can still
push `refs/notes/byline` explicitly.

## Security

The short one-liners execute a mutable script from the protected `main`
branch. Use the reviewed-script or checksum-first method when that trust model
is not acceptable. Installers never use `sudo`, never disable TLS checks, and
never execute a downloaded git-byline binary before its archive checksum is
verified. They install for the current user only: a system-wide location would
need elevation, which they never request. Agent detection reads a command name
and a directory name, writes nothing of its own, and only ever calls
`git-byline install-hooks`. When run inside a Git worktree, installers enable automatic
attribution-note sharing unless `--no-git-hook` is used and hooks are later
installed with `--local-notes`. The `update` command follows the same rules
offline: it refuses an archive whose checksum, file allowlist, or entry types
do not match, and it never downloads anything.
