# Documentation assets

## `integrations.png`

Composed for git-byline from these public sources:

- Factory organization avatar from [Factory-AI](https://github.com/Factory-AI);
- VS Code icon from the
  [official VS Code repository](https://github.com/microsoft/vscode/blob/main/resources/win32/code_150x150.png);
- Claude Code, GitHub Copilot, Cursor, Codex, Gemini, Windsurf, and Grok icons
  from [Lobe Icons](https://github.com/lobehub/lobe-icons), static SVG package
  version 1.94.0, distributed under MIT.

Brand names and marks remain property of their respective owners. Their
appearance documents supported integrations and does not imply endorsement.

## Demo animations

`git-byline-tour.gif`, `git-byline-stats.gif`, and `git-byline-audit.gif`
are real terminal recordings of the binary built from this repository. The
colors are the ones `git byline blame` prints itself, taken from the
palette in [design](../DESIGN.md). `git-byline-dashboard.gif` switches between four viewport captures
of the two HTML files that `git byline dashboard` writes for the same
repository. No frame is retouched and no output is edited by hand.

The tour is the hero animation. It is assembled from four separate
recordings on one 1250 by 486 canvas: two at font size 15 and two at font
size 22. A larger font fits fewer rows and columns into the same pixel
frame, so those views read as a close-up without resampling a single glyph.
Each view is composed to fill its rows, which is why the commands differ
per view.

Reproduce everything with:

```sh
docs/assets/demo/record-media.sh
```

The script needs `go`, `vhs`, `ttyd`, `ffmpeg`, and `jq`. It builds the
binary, builds the demo repository with
`docs/assets/demo/build-demo-repo.sh`, records the terminal tapes, and
encodes the GIFs. Dashboard frames come from a headless browser at a 1280
by 820 viewport, saved as `dash-1.png` to `dash-4.png` in the work
directory; the script encodes them when they are present.

Frame sizes are chosen so the tallest view of each recording fills the
frame, which is why the terminal animations have different heights.

The terminal theme is built from the same palette. Slots the palette does
not define, such as the ANSI green and yellow positions, take the nearest
available step instead of a new color.

## Demo repository

`build-demo-repo.sh` creates a synthetic repository whose attribution is
real: every state comes from agent-v1 hook events sent to
`git byline checkpoint` and from annotating actual commits. It contains:

- 7 commits including one merge, all annotated;
- all four attribution states, including untracked merge content;
- 3 agents (droid, codex, claude) and 3 representative model labels;
- 4 sessions and 3 files;
- 2 human identities, `john.doe` and `maya.chen`, from the commit authors.

Agent, model, and person names are representative metadata. They are not
claims that those models generated the sample code, and the two example
identities use `example.com` addresses. In the aggregate views a file can
report more lines than it has, because a range report counts each commit
that carries the file.

Assets contain no customer code, private paths, prompts, transcripts, or
credentials.
