import {readdirSync} from 'node:fs';
import path from 'node:path';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';
import type {PrismTheme} from 'prism-react-renderer';
import manifest from '../.release-please-manifest.json';
import repoLinks from './src/remark/repoLinks';

const repoUrl = 'https://github.com/comarch/git-byline';
const repoRoot = path.resolve(__dirname, '..');
const version = manifest['.'];

type Page = {id: string; slug: string; title?: string};

// Repository files outside docs/ that the site renders in place. Every
// Markdown file directly under docs/ is published too, so the site and
// GitHub always show the same text.
const rootPages: Record<string, Page> = {
  // The README heading is the project name, which makes a poor page title.
  'README.md': {id: 'guide', slug: '/guide', title: 'Product guide'},
  'docs/README.md': {id: 'overview', slug: '/'},
  'action/README.md': {id: 'github-action', slug: '/github-action'},
  'marketplace/harness/README.md': {id: 'agent-templates', slug: '/agent-templates'},
  'CONTRIBUTING.md': {id: 'contributing', slug: '/contributing'},
  'SECURITY.md': {id: 'security-policy', slug: '/security-policy'},
  'CODE_OF_CONDUCT.md': {id: 'code-of-conduct', slug: '/code-of-conduct'},
  'CHANGELOG.md': {id: 'changelog', slug: '/changelog'},
};

function docsPage(file: string): Page {
  const id = file.replace(/\.md$/, '').toLowerCase().replaceAll('_', '-');
  return {id, slug: `/${id}`};
}

const pages = new Map<string, Page>(Object.entries(rootPages));
for (const file of readdirSync(path.join(repoRoot, 'docs'))) {
  const rel = `docs/${file}`;
  if (file.endsWith('.md') && !pages.has(rel)) {
    pages.set(rel, docsPage(file));
  }
}
const publishedFiles = [...pages.keys()].sort();

// Colors come from the palette in docs/DESIGN.md, so code on the site
// reads like the terminal output of the binary.
const prismTheme: PrismTheme = {
  plain: {color: '#FFFFFF', backgroundColor: '#0D0D0D'},
  styles: [
    {types: ['comment', 'prolog', 'doctype', 'cdata'], style: {color: '#A6A6A6', fontStyle: 'italic'}},
    {types: ['keyword', 'selector', 'tag', 'important', 'atrule'], style: {color: '#FF80CD'}},
    {types: ['string', 'char', 'attr-value', 'regex', 'url'], style: {color: '#00FFFF'}},
    {types: ['function', 'class-name', 'builtin'], style: {color: '#BFBFFF'}},
    {types: ['number', 'boolean', 'constant', 'symbol'], style: {color: '#B280DF'}},
    {types: ['property', 'attr-name', 'key'], style: {color: '#D8BFEF'}},
    {types: ['operator', 'punctuation', 'entity'], style: {color: '#BFBFBF'}},
    {types: ['variable', 'parameter'], style: {color: '#FFFFFF'}},
    {types: ['deleted'], style: {color: '#FF8080'}},
    {types: ['inserted'], style: {color: '#00AAAA'}},
  ],
};

const config: Config = {
  title: 'git-byline',
  tagline:
    'Line-level proof of who wrote your code - which person, which agent, which model - without sending anything anywhere.',
  favicon: 'img/logo.svg',

  future: {
    v4: true,
    faster: false,
  },

  url: 'https://comarch.github.io',
  baseUrl: '/git-byline/',
  trailingSlash: false,
  organizationName: 'comarch',
  projectName: 'git-byline',

  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',
  onDuplicateRoutes: 'throw',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  customFields: {
    repoUrl,
    version,
  },

  // Without JavaScript nothing observes the scroll position, so content
  // that fades or grows in on scroll must start visible.
  headTags: [
    {
      tagName: 'noscript',
      attributes: {},
      innerHTML:
        '<style>.reveal,.reveal .grow-x,.reveal .grow-y,.reveal .grow-spin{opacity:1!important;transform:none!important}</style>',
    },
  ],

  markdown: {
    format: 'detect',
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
    parseFrontMatter: async (params) => {
      const result = await params.defaultParseFrontMatter(params);
      const rel = path.relative(repoRoot, params.filePath).split(path.sep).join('/');
      const page = pages.get(rel);
      if (page) {
        result.frontMatter = {...page, ...result.frontMatter};
      }
      return result;
    },
  },

  themes: ['@docusaurus/theme-mermaid'],

  presets: [
    [
      'classic',
      {
        docs: {
          path: '..',
          include: publishedFiles,
          routeBasePath: 'docs',
          sidebarPath: './sidebars.ts',
          editUrl: ({docPath}) => `${repoUrl}/edit/main/${docPath}`,
          beforeDefaultRemarkPlugins: [[repoLinks, {repoRoot, repoUrl, publishedFiles}]],
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'https://raw.githubusercontent.com/comarch/git-byline/main/docs/assets/social-preview.png',
    metadata: [
      {
        name: 'keywords',
        content: 'git, git blame, AI code attribution, provenance, AI coding agents, git notes, compliance',
      },
    ],
    colorMode: {
      defaultMode: 'dark',
      disableSwitch: true,
      respectPrefersColorScheme: false,
    },
    navbar: {
      title: 'git-byline',
      logo: {
        alt: 'git-byline mark',
        src: 'img/logo.svg',
        width: 28,
        height: 28,
      },
      items: [
        {to: '/#features', label: 'Features', position: 'left', activeBaseRegex: '^$'},
        {to: '/#compare', label: 'Compare', position: 'left', activeBaseRegex: '^$'},
        {type: 'docSidebar', sidebarId: 'docs', label: 'Docs', position: 'left'},
        {to: '/docs/install', label: 'Install', position: 'left'},
        {to: '/docs/changelog', label: 'Changelog', position: 'left'},
        {href: `${repoUrl}/releases/tag/v${version}`, label: `v${version}`, position: 'right'},
        {
          href: repoUrl,
          position: 'right',
          className: 'navbar-github-link',
          'aria-label': 'git-byline on GitHub',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Product',
          items: [
            {label: 'Features', to: '/#features'},
            {label: 'How it compares', to: '/#compare'},
            {label: 'Installation', to: '/docs/install'},
            {label: 'Product guide', to: '/docs/guide'},
            {label: 'Changelog', to: '/docs/changelog'},
          ],
        },
        {
          title: 'Documentation',
          items: [
            {label: 'Why git-byline', to: '/docs/why-git-byline'},
            {label: 'Architecture', to: '/docs/architecture'},
            {label: 'Security model', to: '/docs/security-model'},
            {label: 'Compatibility', to: '/docs/compatibility'},
          ],
        },
        {
          title: 'Community',
          items: [
            {label: 'GitHub', href: repoUrl},
            {label: 'Discussions', href: `${repoUrl}/discussions`},
            {label: 'Report a bug', href: `${repoUrl}/issues/new/choose`},
            {label: 'Security advisories', href: `${repoUrl}/security/advisories/new`},
          ],
        },
      ],
      copyright: 'Copyright (c) 2026 Comarch S.A. Released under the MIT License.',
    },
    prism: {
      theme: prismTheme,
      darkTheme: prismTheme,
      additionalLanguages: ['bash', 'powershell', 'diff'],
    },
    mermaid: {
      theme: {light: 'base', dark: 'base'},
      options: {
        fontFamily: '"Cera Pro", Inter, ui-sans-serif, system-ui, sans-serif',
        themeVariables: {
          darkMode: true,
          background: '#000000',
          primaryColor: '#1A1A1A',
          primaryTextColor: '#FFFFFF',
          primaryBorderColor: '#00FFFF',
          secondaryColor: '#333333',
          tertiaryColor: '#1A1A1A',
          lineColor: '#A6A6A6',
          textColor: '#FFFFFF',
          edgeLabelBackground: '#000000',
        },
      },
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
