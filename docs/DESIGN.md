# Design

git-byline renders two visual surfaces: the self-contained HTML report from
`git byline dashboard` and the colored terminal output from
`git byline blame`. Both use one palette, so a line keeps its color whether
it is read in a terminal or in a browser.

Two product constraints shape everything below. The report must stay a single
offline file, so no font, image, stylesheet, or script is ever fetched. And
color must never be the only signal, because attribution is evidence: every
state also prints its name.

## Palette

The four attribution states each own one hue. The mapping is fixed, and a
regression test fails the build if a color outside the scale appears.

| State | Token | Value | Contrast on `#1A1A1A` |
| --- | --- | --- | ---: |
| `ai` | cyan | `#00FFFF` | 13.88:1 |
| `human` | violet 200 | `#B280DF` | 5.84:1 |
| `human-override` | magenta | `#FF009B` | 4.75:1 |
| `untracked` | grey 500 | `#A6A6A6` | 7.15:1 |

`human` uses the lighter violet step because the base violet `#6400BE`
reaches only 2.29:1 against black, below the 4.5:1 floor.

Surfaces:

| Role | Value | Note |
| --- | --- | --- |
| Canvas | `#000000` | |
| Card | `#1A1A1A` | |
| Raised card | `#333333` | |
| Border | `#404040` | |
| Text | `#FFFFFF` | 21:1 on canvas |
| Muted text | `#BFBFBF` | 9.46:1 on a card |
| Faint text | `#A6A6A6` | 7.15:1 on a card |
| Alert | `#FF4040` | 5.02:1 on a card |

The scale carries only cyan, blue, violet, magenta, red, and grey. There is
no green, orange, or yellow, which is why success reads as cyan and failure
reads as red.

Per-agent tones stay inside the same families, and each one clears 4.5:1 on a
card:

| Agent | Value | Contrast |
| --- | --- | ---: |
| droid, factory | `#00FFFF` | 13.88:1 |
| claude | `#FF80CD` | 7.64:1 |
| codex | `#00AAAA` | 6.08:1 |
| gemini | `#D8BFEF` | 10.46:1 |
| copilot | `#8080FF` | 5.35:1 |
| vscode, windsurf | `#BFBFFF` | 10.04:1 |
| cursor | `#FF8080` | 7.17:1 |
| grok | `#FF009B` | 4.75:1 |

An unknown agent name hashes into the same list, so an unexpected agent can
never introduce a color from outside the palette.

## Gradient

One gradient appears per report, as a 3 px rule under the header:

```css
linear-gradient(90deg, #00FFFF 0%, #6400BE 50%, #FF0000 100%)
```

Stop order, membership, and even distribution are fixed. The three
decorative header dots reuse the same three stop colors in the same order.
They are decorative only and marked `aria-hidden`.

## Typography

```css
font-family: "Cera Pro", Inter, Arial, sans-serif;
```

The first two families are preferences for readers who already have them
installed. No `@font-face` rule and no font file exist in this repository, so
the report renders with whatever is available locally and stays offline.

| Element | Size | Weight | Line height | Tracking |
| --- | --- | --- | --- | --- |
| Report title | `clamp(1.5rem, 2.4vw, 2.5rem)` | 800 | 1.2 | -0.02em |
| KPI number | 2.5rem | 800 | 1.1 | -0.02em |
| Section heading | 1.25rem | 700 | inherited | -0.02em |
| Body | 1rem | 400 | 1.4 | -0.01em |
| Monospace data | 0.82rem | 400 | inherited | inherited |

Body text stays at 16 px with 140 percent line height. Information is not
shrunk to fit a tighter layout; a dense table scrolls instead.

Line data, counts, and object IDs use a system monospace stack, because the
display families above are proportional.

## Terminal output

`git byline blame` uses the same mapping. With 24-bit color the exact hex
values are emitted; without it, the nearest of the basic sixteen colors is
used and documented as an approximation, not a palette value.

| State | 24-bit sequence | Value |
| --- | --- | --- |
| `ai` | `38;2;0;255;255` | `#00FFFF` |
| `human` | `38;2;178;128;223` | `#B280DF` |
| `human-override` | `38;2;255;0;155` | `#FF009B` |
| `untracked` | `38;2;166;166;166` | `#A6A6A6` |

24-bit output is selected from `COLORTERM`. Color is dropped entirely when
output is not a terminal, when `NO_COLOR` is set, when `TERM` is `dumb`, or
when `--color=never` is passed.

The recorded README animations use a terminal theme built from the same
palette. Slots the palette does not define, such as the ANSI green and
yellow positions, take the nearest available step instead of a new color.

## Accessibility

Measured on the rendered report at a 1280 by 820 viewport:

- normal text contrast at least 4.5:1, verified for every token above;
- body text 16 px, line height 1.4;
- file picker height exactly 44 px, the minimum touch target;
- visible `:focus-visible` outline in cyan with a 2 px offset;
- `prefers-reduced-motion` honored;
- no horizontal overflow at 375, 768, 1024, and 1440 px;
- the report opens at the top of the page, with no scroll jump;
- every state carries a text label beside its color.

## Verification

Rendering contracts are pinned by tests in `internal/dashboard` and
`internal/app`:

- every tone class used by a template is declared by the stylesheet;
- every tone class resolves to a palette token;
- the gradient string is present and unchanged;
- the type stack is present;
- the accessibility guardrails above are present;
- colors from outside the palette are absent;
- the terminal escape sequences match the declared hex values.

Re-measure contrast and responsive behavior after any visual change. The
report is one offline HTML file, so a browser opened on the generated file is
the complete test environment.
