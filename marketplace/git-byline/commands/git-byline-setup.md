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
`$binary version`. If `.factory/hooks.json` already contains
`promptscript-generated`, keep that project hook and install only the Git hook:

```sh
"$binary" install-hooks --agent none --git --project
```

Otherwise install the Factory user hook and current repository Git hook:

```sh
"$binary" install-hooks --agent droid --git --user
```

Git hook installation shares `refs/notes/byline` with the selected remote on
ordinary pushes. Tell the user that notes contain paths, line ranges, agent
and model names, session identifiers, timestamps, and blob IDs. If the user
wants automatic note sharing disabled, rerun the selected hook command with
`--local-notes`. Notes can still be pushed explicitly.

Run `$binary status`. Tell the user to add the reported install directory to
`PATH` and restart active agents before relying on project hooks. Stop and
report exact error when download, checksum, version, hook installation, or
status verification fails. Never use `sudo`.
