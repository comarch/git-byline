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

`git-byline-demo.gif`, `git-byline-blame.png`, and `git-byline-stats.png` use
real CLI and JSON output from a temporary local repository. The controlled
demo sends synthetic hook events through supported Factory, Claude Code,
Codex, and Gemini CLI adapters. Agent and model labels are representative
metadata, not claims that those models generated the sample code.

The statistics are calculated from `git byline blame --json` and
`git byline status --json` output:

- 32 attributed lines;
- 21 AI-observed lines and 11 human/default lines;
- four agents and four model labels;
- complete 32/32 line coverage;
- zero pending checkpoints, pending files, and retained snapshots.

Assets contain no customer code, private paths, prompts, transcripts, or
credentials.
