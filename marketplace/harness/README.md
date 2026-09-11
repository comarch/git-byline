# Agent setup templates

Three agents install git-byline as a package. The rest have no package
manager, so this directory carries the two files they need, ready to copy.

## Agents with a package

| Agent | Install |
| --- | --- |
| Factory | `droid plugin marketplace add comarch/git-byline` then `/git-byline-setup` |
| Claude Code | `/plugin marketplace add comarch/git-byline` then `/git-byline-setup` |
| Gemini CLI | `gemini extensions install https://github.com/comarch/git-byline` then `/git-byline-setup` |

Factory and Claude Code both load [`../git-byline`](../git-byline): one
plugin directory with a manifest per agent and shared `commands/` and
`skills/`. Gemini CLI reads `gemini-extension.json` and
`commands/git-byline-setup.toml` from the repository root, because that is
where it installs an extension from.

## Agents you set up by copying

Copy the setup command so the agent gains `/git-byline-setup`, then run it.
The command installs the verified binary, installs the Git hooks, and copies
the hook file below. Copying the hook file by hand works too.

| Agent | Setup command | Copy it to | Hook file | Copy it to |
| --- | --- | --- | --- | --- |
| GitHub Copilot | [`copilot/git-byline-setup.prompt.md`](copilot/git-byline-setup.prompt.md) | `.github/prompts/` | [`copilot/hooks.json`](copilot/hooks.json) | `.github/hooks/promptscript.json` |
| VS Code Agent | [`vscode/git-byline-setup.prompt.md`](vscode/git-byline-setup.prompt.md) | `.github/prompts/` | [`vscode/hooks.json`](vscode/hooks.json) | `.github/hooks/promptscript-vscode.json` |
| Cursor | [`cursor/git-byline-setup.md`](cursor/git-byline-setup.md) | `.cursor/commands/` | [`cursor/hooks.json`](cursor/hooks.json) | `.cursor/hooks.json` |
| Codex | [`codex/git-byline-setup.md`](codex/git-byline-setup.md) | `~/.codex/prompts/` | [`codex/hooks.json`](codex/hooks.json) | `.codex/hooks.json` |
| Windsurf | [`windsurf/git-byline-setup.md`](windsurf/git-byline-setup.md) | `.windsurf/workflows/` | [`windsurf/hooks.json`](windsurf/hooks.json) | `.windsurf/hooks.json` |
| Grok | [`grok/git-byline-setup.md`](grok/git-byline-setup.md) | the prompt directory your build reads | [`grok/hooks.json`](grok/hooks.json) | `.grok/hooks/promptscript.json` |
| Gemini CLI, without the extension | none, use the extension above | | [`gemini/settings.json`](gemini/settings.json) | merge into `.gemini/settings.json` |

`.gemini/settings.json` and `.claude/settings.json` hold unrelated settings
too, so merge the `hooks` block instead of replacing the file. Every other
hook file in this table is complete and can be copied as is.

## Why these files are safe to copy

Each hook file is byte-identical to the one this repository generates for
itself, and `go run ./tools/validate` fails when they drift apart. The hook
bodies contain no absolute path: they resolve the project root with
`git rev-parse --show-toplevel` and find `git-byline` through `PATH`.

Each hook calls `git-byline checkpoint portable-<agent> --hook-input stdin`,
which records a snapshot before and after an edit. Without the agent hook,
git-byline still annotates commits, but agent edits arrive as plain `human`
lines instead of `ai` lines, because nothing observed them.

## Git hooks

The Git side is the same for every agent and needs no template:

```sh
git byline install-hooks --agent none --git --project
```

Add `--local-notes` to keep attribution notes off ordinary pushes.
