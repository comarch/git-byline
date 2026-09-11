# git-byline setup

Install git-byline for the current operating system and activate
attribution hooks. Never use `sudo`.

**1. Install the verified binary.**

Linux or macOS:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  https://raw.githubusercontent.com/comarch/git-byline/main/install.sh |
  bash -s -- --no-git-hook
binary="$HOME/.local/bin/git-byline"
```

Windows PowerShell:

```powershell
$installer = Join-Path ([IO.Path]::GetTempPath()) ("git-byline-" + [guid]::NewGuid() + ".ps1")
try {
    irm https://raw.githubusercontent.com/comarch/git-byline/main/install.ps1 -OutFile $installer
    & $installer -NoGitHook
}
finally {
    Remove-Item $installer -Force -ErrorAction SilentlyContinue
}
$binary = Join-Path $HOME "bin/git-byline.exe"
```

The installer verifies the release archive checksum and the binary version.
Run the remaining commands through the absolute `$binary` path, so setup does
not depend on this process reloading `PATH`. Verify `$binary version`.

**2. Install the Git hooks.**

```sh
"$binary" install-hooks --agent none --git --project
```

This installs `post-commit` attribution and `pre-push` note sharing for the
current repository. Attribution notes contain repository paths, line ranges,
agent and model names, human identity tokens, session identifiers,
timestamps, and blob IDs, and ordinary pushes publish them to the same
remote. Tell the user this. Rerun with `--local-notes` if the user wants
attribution without automatic note sharing.

**3. Install the agent hook for this agent.**

Copy `marketplace/harness/cursor/hooks.json` from the git-byline repository into
`.cursor/hooks.json` of the user's project, creating parent directories as needed:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  -o .cursor/hooks.json --create-dirs \
  https://raw.githubusercontent.com/comarch/git-byline/main/marketplace/harness/cursor/hooks.json
```

The same file is checked in beside this command, so it can also be copied
from a local clone. It calls `git-byline checkpoint portable-cursor
--hook-input stdin` and resolves `git-byline` through `PATH`. Do not edit the
hook bodies.

**4. Verify and report.**

Run `"$binary" status`. Tell the user to add the reported install directory
to `PATH` and to restart this agent so the hook is picked up. Stop and report
the exact error when download, checksum, version, hook installation, or
status verification fails.
