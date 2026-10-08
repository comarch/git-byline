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

Install `marketplace/harness/opencode/promptscript.ts` from the git-byline
release tag below into `.opencode/plugins/promptscript.ts` of the user's
project. OpenCode loads everything in that directory as code. The commands
therefore download the plugin to a temporary file, compare its SHA-256 with
the value below, and move it into place only when the two match. Do not change
the tag or the SHA-256. On a mismatch, stop and report both values.

Linux, macOS, or WSL:

```sh
tag=v1.6.0 # x-release-please-version
sha256=6e2c94e589439ad4d927a356820d4b68279ecce2c079d87aa04aa27c2ca54ef6
plugin="$(mktemp)"
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 -o "$plugin" \
  "https://raw.githubusercontent.com/comarch/git-byline/$tag/marketplace/harness/opencode/promptscript.ts"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$plugin" | awk '{ print $1 }')"
else
  actual="$(shasum -a 256 "$plugin" | awk '{ print $1 }')"
fi
if [ "$actual" = "$sha256" ]; then
  mkdir -p .opencode/plugins
  chmod 644 "$plugin"
  mv "$plugin" .opencode/plugins/promptscript.ts
else
  rm -f "$plugin"
  echo "plugin checksum mismatch: want $sha256, got $actual" >&2
  false
fi
```

Native Windows PowerShell, where `curl` is an alias for `Invoke-WebRequest`
and rejects the flags above:

```powershell
$tag = "v1.6.0" # x-release-please-version
$sha256 = "6e2c94e589439ad4d927a356820d4b68279ecce2c079d87aa04aa27c2ca54ef6"
$plugin = Join-Path ([IO.Path]::GetTempPath()) ("git-byline-" + [guid]::NewGuid() + ".ts")
try {
    irm "https://raw.githubusercontent.com/comarch/git-byline/$tag/marketplace/harness/opencode/promptscript.ts" -OutFile $plugin
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $plugin).Hash
    if ($actual -ne $sha256) {
        throw "plugin checksum mismatch: want $sha256, got $actual"
    }
    New-Item -ItemType Directory -Force -Path .opencode/plugins | Out-Null
    Move-Item -Force -LiteralPath $plugin .opencode/plugins/promptscript.ts
}
finally {
    Remove-Item $plugin -Force -ErrorAction SilentlyContinue
}
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
