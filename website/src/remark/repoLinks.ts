import {existsSync, statSync} from 'node:fs';
import path from 'node:path';

// Only the mdast fields this plugin reads or writes.
type MdNode = {
  type: string;
  depth?: number;
  url?: string;
  value?: string;
  children?: MdNode[];
};

type Options = {
  repoRoot: string;
  repoUrl: string;
  publishedFiles: readonly string[];
};

const urlScheme = /^[a-z][a-z\d+.-]*:/i;
const remoteUrl = /^https?:\/\//i;

// The site renders repository Markdown in place. Relative links to files
// the site does not publish would break, so they are pointed at the file on
// GitHub instead. Remote images, the README badges, are dropped so the site
// never loads a third-party resource.
export default function repoLinks(options: Options) {
  const published = new Set(options.publishedFiles);
  return (tree: MdNode, file: {path?: string}): void => {
    if (!file.path) {
      return;
    }
    const dir = path.dirname(file.path);
    // Docusaurus renders the h1 as the page title without an anchor, so a
    // GitHub link to it, like #git-byline in the README, points at the top
    // of the page instead.
    const title = tree.children?.find((node) => node.type === 'heading' && node.depth === 1);
    const titleAnchor = title ? `#${slug(textOf(title))}` : undefined;
    prune(tree);
    visit(tree, (node) => {
      if ((node.type === 'link' || node.type === 'definition') && node.url) {
        node.url = node.url === titleAnchor ? '#' : rewrite(node.url, dir, options, published);
      }
    });
  };
}

function textOf(node: MdNode): string {
  if (node.type === 'text' || node.type === 'inlineCode') {
    return node.value ?? '';
  }
  return (node.children ?? []).map(textOf).join('');
}

// GitHub heading slugs: lowercase, punctuation dropped, spaces to hyphens.
function slug(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{M}\p{N}\p{Pc} -]/gu, '')
    .replace(/ /g, '-');
}

function rewrite(url: string, dir: string, options: Options, published: Set<string>): string {
  if (url.startsWith('#') || url.startsWith('/') || urlScheme.test(url)) {
    return url;
  }
  const cut = url.search(/[?#]/);
  const target = cut === -1 ? url : url.slice(0, cut);
  const suffix = cut === -1 ? '' : url.slice(cut);
  const abs = path.resolve(dir, decodeURI(target));
  const rel = path.relative(options.repoRoot, abs).split(path.sep).join('/');
  // Published pages are resolved by Docusaurus; missing files stay as they
  // are so the broken link check fails the build.
  if (rel.startsWith('..') || published.has(rel) || !existsSync(abs)) {
    return url;
  }
  const kind = statSync(abs).isDirectory() ? 'tree' : 'blob';
  return `${options.repoUrl}/${kind}/main/${rel}${suffix}`;
}

function isBlank(node: MdNode): boolean {
  return node.type === 'text' && !node.value?.trim();
}

function prune(node: MdNode): void {
  if (!node.children) {
    return;
  }
  for (const child of node.children) {
    prune(child);
  }
  node.children = node.children.filter((child) => {
    if (child.type === 'image') {
      return !remoteUrl.test(child.url ?? '');
    }
    if (child.type === 'link' || child.type === 'paragraph') {
      return !(child.children ?? []).every(isBlank);
    }
    return true;
  });
}

function visit(node: MdNode, fn: (node: MdNode) => void): void {
  fn(node);
  for (const child of node.children ?? []) {
    visit(child, fn);
  }
}
