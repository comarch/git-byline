# Installation

git-byline supports Linux, macOS, and Windows on amd64 and arm64. Every
prebuilt archive is covered by `checksums.txt`.

## Factory marketplace plugin

The plugin removes the manual binary installation step. Its
`/git-byline-setup` command selects the operating-system installer, verifies
the release archive, installs the local runtime, and configures hooks.

```text
droid plugin marketplace add comarch/git-byline
droid plugin install git-byline@git-byline --scope user
```

Start Droid in a repository, run `/git-byline-setup`, then verify:

```sh
git-byline version
git-byline status
```

The plugin still installs a local executable. Git `post-commit` hooks must work
when no AI agent session is running, so a plugin-only in-process runtime cannot
provide complete attribution.

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

Override defaults:

```sh
GIT_BYLINE_VERSION=v0.1.0 \
GIT_BYLINE_BIN_DIR="$HOME/bin" \
sh install.sh --no-git-hook
```

## Windows PowerShell

Fast install:

```powershell
irm https://raw.githubusercontent.com/comarch/git-byline/main/install.ps1 | iex
```

The script supports Windows on amd64 and arm64. It performs the same checksum
and binary-version checks and installs to `$HOME\bin`.

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

## Security

The short one-liners execute a mutable script from the protected `main`
branch. Use the reviewed-script or checksum-first method when that trust model
is not acceptable. Installers never use `sudo`, never disable TLS checks, and
never execute a downloaded git-byline binary before its archive checksum is
verified.
