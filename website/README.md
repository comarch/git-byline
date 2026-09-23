# git-byline website

The landing page and documentation site published at
<https://comarch.github.io/git-byline/>. It is a Docusaurus project that lives
next to the Go module and never ships in release archives.

## Layout

- `src/pages/index.tsx` and `src/components/landing/` - the landing page.
- `src/data/demo.json` - real `git byline` output that feeds the landing page
  charts and terminal panels.
- `docusaurus.config.ts` and `sidebars.ts` - site configuration and the docs
  sidebar.
- `src/remark/repoLinks.ts` - rewrites repository-relative links for the site.
- `static/` - files copied as they are, like the logo.

The docs are not copied into this directory. The site renders the repository
Markdown in place: every file directly under `docs/`, plus the root pages
listed in `rootPages` in `docusaurus.config.ts` (the README as the product
guide, the GitHub Action and agent template READMEs, the contributing,
security, and conduct policies, and the changelog). GitHub and the site
always show the same text.

## Develop

Requires Node.js 20 or newer. CI uses Node.js 24.

```bash
cd website
npm ci --ignore-scripts
npm start            # dev server with live reload
npm run typecheck
npm run build        # production build in build/
npm run serve        # serve the production build locally
```

`npm run build` fails on broken links, broken anchors, and broken Markdown
links, so a moved file or a renamed heading is caught before it ships.

## Add or move a page

- A new Markdown file directly under `docs/` is published automatically. Add
  it to `sidebars.ts`, or it has no sidebar entry.
- A Markdown file anywhere else needs an entry in `rootPages` first.
- Document IDs keep their directory, like `docs/install` or
  `action/github-action`, because the docs plugin reads the whole repository.

Write links the way GitHub expects them. `repoLinks.ts` keeps links between
published pages, points links to other repository files at GitHub, and drops
remote images such as the README badges.

## Refresh the demo data

`src/data/demo.json` is generated, never edited by hand:

```bash
npm run demo-data
```

The script builds the binary from this checkout, builds the demo repository
from real hook events with `docs/assets/demo/build-demo-repo.sh`, and records
what `stats`, `blame`, `check`, `verify`, and `disclosure` print. It needs Go,
Git, jq, and Python 3. Refresh it when the output format of those commands
changes, and review the diff like any other change.

## Privacy

The site keeps the promise of the tool: no analytics, no tracking, no web
fonts, no CDN, and no third-party script or stylesheet. Text uses locally
installed fonts only, charts are inline SVG, and recordings come from
`docs/assets/`.
Keep new dependencies and embeds to the same rule.

## Deploy

`.github/workflows/pages.yml` builds the site on every pull request and
deploys `main` to GitHub Pages. The Pages source must be set to GitHub
Actions, see `docs/REPOSITORY_SETTINGS.md`.
