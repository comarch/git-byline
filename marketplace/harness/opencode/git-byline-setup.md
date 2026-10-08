# git-byline setup

Install git-byline for the current operating system and activate
attribution hooks. Never use `sudo`.

On Windows, run OpenCode inside WSL and follow the Linux or macOS commands in
the WSL shell. PromptScript 1.19.1 declares the generated plugin Unix-only,
and native Windows plugin loading has not been verified. `install.ps1` does
not detect OpenCode or install its plugin. If the user runs OpenCode natively
on Windows, use the PowerShell commands below and tell the user that this
setup is unverified.

**1. Install the verified binary.**

Linux, macOS, or WSL:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  https://raw.githubusercontent.com/comarch/git-byline/main/install.sh |
  bash -s -- --no-git-hook
binary="$HOME/.local/bin/git-byline"
```

Native Windows PowerShell:

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
not depend on this process reloading `PATH`. PowerShell needs the call
operator for that, as in `& $binary version`. Verify `$binary version`.

**2. Install the Git hooks.**

```sh
"$binary" install-hooks --agent none --git --project
```

In PowerShell, run `& $binary install-hooks --agent none --git --project`.

This installs `post-commit` attribution and `pre-push` note sharing for the
current repository. Attribution notes contain repository paths, line ranges,
agent and model names, human identity tokens, session identifiers,
timestamps, and blob IDs, and ordinary pushes publish them to the same
remote. Tell the user this. Rerun with `--local-notes` if the user wants
attribution without automatic note sharing.

**3. Install the OpenCode plugin.**

Copy `marketplace/harness/opencode/promptscript.ts` from the git-byline
repository into `.opencode/plugins/promptscript.ts` of the user's project.

Linux, macOS, or WSL:

```sh
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \
  -o .opencode/plugins/promptscript.ts --create-dirs \
  https://raw.githubusercontent.com/comarch/git-byline/main/marketplace/harness/opencode/promptscript.ts
```

Native Windows PowerShell, where `curl` is an alias for `Invoke-WebRequest`
and rejects the flags above:

```powershell
New-Item -ItemType Directory -Force -Path .opencode/plugins | Out-Null
irm https://raw.githubusercontent.com/comarch/git-byline/main/marketplace/harness/opencode/promptscript.ts -OutFile .opencode/plugins/promptscript.ts
```

This generated PromptScript plugin captures OpenCode edit, write,
apply_patch, and Bash tool events, and waits for each checkpoint before
OpenCode continues. OpenCode does not include the active model in these events, so
the checkpoint model stays `unknown`. MCP calls, some subagent paths, and
failed tool calls do not have a dedicated plugin event. Do not edit the
generated plugin.

**4. Verify and report.**

Run `"$binary" status`, or `& $binary status` in PowerShell. Tell the user to
add the reported install directory to `PATH` and restart OpenCode so it loads
the plugin. Stop and report the exact error when download, checksum, version,
hook installation, or status verification fails.
