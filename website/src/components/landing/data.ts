import demo from '@site/src/data/demo.json';

export type Kind = 'human' | 'ai' | 'human_override' | 'untracked';

export type Counts = Record<Kind, number> & {lines: number};

export type BlameLine = {
  kind: Kind;
  label: string;
  number: number;
  code: string;
};

export type BlameFile = {
  path: string;
  lines: BlameLine[];
  counts: Counts;
};

export type Commit = Counts & {
  id: string;
  short: string;
  subject: string;
  author: string;
  date: string;
};

export type Step = {
  command: string;
  output: string;
  exit: number;
};

export const kinds: Kind[] = ['human', 'ai', 'human_override', 'untracked'];

export const kindLabel: Record<Kind, string> = {
  human: 'human',
  ai: 'ai',
  human_override: 'human-override',
  untracked: 'untracked',
};

// State and agent tones are the palette from docs/DESIGN.md.
export const kindColor: Record<Kind, string> = {
  human: '#B280DF',
  ai: '#00FFFF',
  human_override: '#FF009B',
  untracked: '#A6A6A6',
};

const agentColors: Record<string, string> = {
  droid: '#00FFFF',
  factory: '#00FFFF',
  claude: '#FF80CD',
  codex: '#00AAAA',
  gemini: '#D8BFEF',
  copilot: '#8080FF',
  vscode: '#BFBFFF',
  windsurf: '#BFBFFF',
  cursor: '#FF8080',
  grok: '#FF009B',
};

export function agentColor(agent: string): string {
  return agentColors[agent] ?? kindColor.untracked;
}

export function share(part: number, whole: number): number {
  return whole === 0 ? 0 : (part / whole) * 100;
}

export function formatShare(part: number, whole: number): string {
  return `${share(part, whole).toFixed(1)}%`;
}

const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

// UTC keeps the server render and the browser render identical.
export function shortDate(iso: string): string {
  const date = new Date(iso);
  return `${months[date.getUTCMonth()]} ${date.getUTCDate()}`;
}

export function describe(counts: Counts): string {
  return kinds
    .filter((kind) => counts[kind] > 0)
    .map((kind) => `${kindLabel[kind]} ${counts[kind]}`)
    .join(', ');
}

function kindOf(label: string): Kind {
  switch (label.split(':')[0]) {
    case 'human':
      return 'human';
    case 'ai':
      return 'ai';
    case 'human-override':
      return 'human_override';
    default:
      return 'untracked';
  }
}

// A blame row looks like "ai:droid/model   12 | code". If the format ever
// changes, the build fails here instead of publishing a wrong page.
const blameRow = /^(\S+)\s+(\d+) \|(?: (.*))?$/;

function parseBlame(output: string): BlameLine[] {
  return output.split('\n').map((row) => {
    const match = blameRow.exec(row);
    if (!match) {
      throw new Error(`unexpected git byline blame row: ${row}`);
    }
    return {kind: kindOf(match[1]), label: match[1], number: Number(match[2]), code: match[3] ?? ''};
  });
}

function countKinds(lines: BlameLine[]): Counts {
  const counts: Counts = {human: 0, ai: 0, human_override: 0, untracked: 0, lines: lines.length};
  for (const line of lines) {
    counts[line.kind] += 1;
  }
  return counts;
}

export const range = demo.range;
export const stats = demo.stats;
export const totals: Counts = demo.stats.totals;
export const terminal: Record<'gate' | 'verify' | 'disclosure' | 'json', Step[]> = demo.terminal;

export const blameFiles: BlameFile[] = demo.blame.map((file) => {
  const lines = parseBlame(file.output);
  return {path: file.path, lines, counts: countKinds(lines)};
});

const logEntries = new Map(demo.log.map((entry) => [entry.commit, entry]));

// Oldest first, the order a timeline reads.
export const commits: Commit[] = demo.stats.commit
  .map((commit) => {
    const entry = logEntries.get(commit.commit);
    if (!entry) {
      throw new Error(`commit ${commit.commit} is missing from the demo log`);
    }
    return {
      id: commit.commit,
      short: commit.commit.slice(0, 7),
      subject: entry.subject,
      author: entry.author,
      date: commit.timestamp,
      human: commit.human,
      ai: commit.ai,
      human_override: commit.human_override,
      untracked: commit.untracked,
      lines: commit.lines,
    };
  })
  .sort((a, b) => a.date.localeCompare(b.date));
