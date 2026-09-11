---
description: Install git-byline and activate attribution hooks
---

Install git-byline for the current operating system without asking the user to
install a binary manually.

On Linux or macOS, run:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  https://raw.githubusercontent.com/comarch/git-byline/main/install.sh |
  bash -s -- --no-git-hook
binary="$HOME/.local/bin/git-byline"
```

On Windows PowerShell, run:

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

Run the remaining commands through the absolute `$binary` path so setup does
not depend on the current agent process reloading `PATH`. Verify
`$binary version`.

This plugin is installed by both Factory and Claude Code, so pick the agent
you are running:

- Factory: `--agent droid`, and its project hook file is `.factory/hooks.json`
- Claude Code: `--agent claude`, and its project hook file is
  `.claude/settings.json`

If that project hook file already contains `promptscript-generated`, keep it
and install only the Git hooks:

```sh
"$binary" install-hooks --agent none --git --project
```

Otherwise install the user-level agent hook for the agent you are running,
plus the current repository Git hooks. For Factory:

```sh
"$binary" install-hooks --agent droid --git --user
```

For Claude Code use `--agent claude` in the same command.

Git hook installation shares `refs/notes/byline` with the selected remote on
ordinary pushes. Tell the user that notes contain paths, line ranges, agent
and model names, human identity tokens, session identifiers, timestamps, and
blob IDs. If the user wants automatic note sharing disabled, rerun the
selected hook command with `--local-notes`. Notes can still be pushed
explicitly.

Run `$binary status`. Tell the user to add the reported install directory to
`PATH` and restart active agents before relying on project hooks. Stop and
report exact error when download, checksum, version, hook installation, or
status verification fails. Never use `sudo`.
