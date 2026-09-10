# Compatibility

## Supported matrix

| Dimension | Minimum | CI coverage | Policy |
| --- | --- | --- | --- |
| Go build toolchain | 1.24 | 1.24 and stable on Linux | Newer stable versions supported |
| Git | 2.31 | Runner-provided Git on Linux, macOS, Windows | Newer stable versions supported |
| Linux | amd64, arm64 | Tests on amd64, cross-build both | Static binary |
| macOS | amd64, arm64 | Tests on hosted runner, cross-build both | Static binary |
| Windows | amd64, arm64 | Tests on hosted runner, cross-build both | Static `.exe` |
| Object format | SHA-1 | Integration tests | Object IDs treated as opaque hex |
| Text | Valid UTF-8 | LF, CRLF, missing final newline | NUL and invalid UTF-8 skipped |

Release archives are built for six operating system and architecture pairs.
Compilation proves arm64 artifacts. Runtime tests execute on hosted amd64 or
native runner architecture.

## Dashboard output

`git byline dashboard` produces one self-contained HTML file. Generation
requires no server, browser extension, network connection, or external runtime.
The report uses standard HTML, CSS, SVG, and a small inline script for file
selection. Browsers that disable JavaScript still render the summary and first
file, but interactive file switching requires JavaScript.

## Agent adapters

| Product or interface | Preset | Events | Paths |
| --- | --- | --- | --- |
| Factory | `droid`, `portable-factory` | `Edit`, `Create`, `ApplyPatch` and generated pre/post hooks | Common file and patch fields |
| Claude Code | `claude`, `portable-claude` | `Write`, `Edit`, `MultiEdit` and generated pre/post hooks | Common file fields |
| GitHub Copilot | `portable-copilot` | Generated pre/post tool hooks | Common file and patch fields |
| VS Code Agent | `portable-vscode` | Generated pre/post tool hooks | Common file and patch fields |
| Cursor | `portable-cursor` | Generated pre/post tool hooks | Common file and patch fields |
| Codex | `portable-codex` | Generated pre/post tool hooks | Common file and patch fields |
| Gemini CLI | `portable-gemini` | Generated pre/post tool hooks | Common file and patch fields |
| Windsurf | `portable-windsurf` | Generated write-event hooks | Common file and patch fields |
| Grok | `portable-grok` | Generated pre/post tool hooks | Common file and patch fields |
| agent-v1 | `agent-v1` | Standard edit payload, `agent_name` required | `edited_filepaths` |

Unknown valid tool events are ignored. Malformed supported events fail without
writing a checkpoint.

PromptScript emits native hook configuration only for platforms with project
hook APIs. Other instruction targets can consume generated project guidance,
but cannot provide edit-event attribution without an external watcher or
daemon. git-byline does not add either.

OpenCode is not currently integrated. OpenCode exposes plugin execution hooks,
but the pinned PromptScript release does not generate an OpenCode hook plugin.
Track native support in
[PromptScript issue #454](https://github.com/mrwogu/promptscript/issues/454).

## Git behavior

- First-parent history is authoritative.
- Linked worktrees receive separate pending state.
- SHA-1 and longer opaque object IDs are accepted.
- Partial commits preserve excluded provenance for the next commit.
- Renames preserve provenance through Git rename detection.

## Unsupported behavior

- Git older than 2.31.
- Binary and invalid UTF-8 attribution.
- Network files or a separate cloud attribution service.
- Prompt and transcript storage.
- Managed hook installation into external or symlinked `core.hooksPath`
  directories.
- Automatic reconstruction across several commits when the Git hook did not
  annotate any intermediate commit.
- Provenance copied from non-first merge parents.

Do not add a compatibility claim without a test or a documented manual
verification result.
