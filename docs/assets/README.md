# Documentation assets

## `integrations.png`

Composed for git-byline from these public brand sources:

- Factory organization avatar from [Factory-AI](https://github.com/Factory-AI);
- VS Code icon from the
  [official VS Code repository](https://github.com/microsoft/vscode/blob/main/resources/win32/code_150x150.png);
- Claude Code, GitHub Copilot, Cursor, Codex, Gemini, Windsurf, and Grok icons
  from [Lobe Icons](https://github.com/lobehub/lobe-icons), static SVG package
  version 1.94.0, distributed under MIT.

Brand names and marks remain property of their respective owners. Their
appearance documents supported integrations and does not imply endorsement.

## Demo assets

`git-byline-blame.png`, `git-byline-stats.png`, and `git-byline-check.png`
show real terminal output captured from a temporary local repository. The
repository was built by sending synthetic hook events through the agent-v1
adapter: two agent edit sessions, one agent session writing a file through a
shell command, and one manual edit that replaced agent-written lines.

`git-byline-dashboard.png` and `git-byline-dashboard-range.png` are real
browser renderings of the self-contained HTML files written by
`git byline dashboard` and `git byline dashboard --range` for that same
repository. No content was edited beyond viewport capture.

The demo repository contains:

- 3 commits, all annotated;
- 44 attributed lines with complete coverage;
- 4 author classes in use: human, AI, human-override, and zero untracked;
- 3 agents (droid, codex, claude) and 3 representative model labels;
- 3 sessions and 2 files.

Agent and model labels are representative metadata, not claims that those
models generated the sample code. In `git-byline-stats.png`, commit hashes are
shortened for display; the full command prints complete object IDs.

Assets contain no customer code, private paths, prompts, transcripts, or
credentials.
