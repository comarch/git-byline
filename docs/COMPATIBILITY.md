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

## Agent adapters

| Adapter | Events | Paths |
| --- | --- | --- |
| Droid | `Edit`, `Create`, `ApplyPatch` | `tool_input.file_path` or patch headers |
| Claude Code | `Write`, `Edit`, `MultiEdit` | `tool_input.file_path` |
| Factory PromptScript | Native pre/post tool hooks | Common file and patch fields |
| Claude PromptScript | Native pre/post tool hooks | Common file and patch fields |
| GitHub Copilot | Native pre/post tool hooks | Common file and patch fields |
| VS Code Agent | Native pre/post tool hooks | Common file and patch fields |
| Cursor | Native pre/post tool hooks | Common file and patch fields |
| Codex | Native pre/post tool hooks | Common file and patch fields |
| Gemini CLI | Native pre/post tool hooks | Common file and patch fields |
| Windsurf | Native write-event hooks | Common file and patch fields |
| Grok | Native pre/post tool hooks | Common file and patch fields |
| agent-v1 | Standard edit payload | `edited_filepaths` |

Unknown valid tool events are ignored. Malformed supported events fail without
writing a checkpoint.

PromptScript emits native hook configuration only for platforms with project
hook APIs. Other instruction targets can consume generated project guidance,
but cannot provide edit-event attribution without an external watcher or
daemon. git-byline does not add either.

## Git behavior

- First-parent history is authoritative.
- Linked worktrees receive separate pending state.
- SHA-1 and longer opaque object IDs are accepted.
- Partial commits preserve excluded provenance for the next commit.
- Renames preserve provenance through Git rename detection.

## Unsupported behavior

- Git older than 2.31.
- Binary and invalid UTF-8 attribution.
- Network files or remote attribution storage.
- Prompt and transcript storage.
- Automatic reconstruction across several commits when the Git hook did not
  annotate any intermediate commit.
- Provenance copied from non-first merge parents.

Do not add a compatibility claim without a test or a documented manual
verification result.
