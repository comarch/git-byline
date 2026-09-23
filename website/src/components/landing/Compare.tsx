import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Icon, {type IconName} from './Icon';
import Reveal from './Reveal';
import {Section, SectionHeader} from './Section';
import styles from './Compare.module.css';

type Mark = 'yes' | 'no' | 'unknown';
type Cell = {mark?: Mark; text?: ReactNode};
type ToolKey = 'byline' | 'gitai' | 'entire' | 'cursor';

// Every claim below comes from the public README, documentation, or source
// of each project. Re-check them before changing the date.
const checkedOn = '23 September 2026';

const tools: {key: ToolKey; name: string; meta: string; href?: string}[] = [
  {key: 'byline', name: 'git-byline', meta: 'MIT · Go'},
  {key: 'gitai', name: 'Git AI', meta: 'Apache-2.0 · Rust', href: 'https://github.com/git-ai-project/git-ai'},
  {key: 'entire', name: 'Entire', meta: 'MIT · Go', href: 'https://github.com/entireio/cli'},
  {key: 'cursor', name: 'Cursor Blame', meta: 'Proprietary', href: 'https://cursor.com/docs/integrations/cursor-blame'},
];

const unknown: Cell = {mark: 'unknown'};

const groups: {title: string; rows: {label: string; cells: Record<ToolKey, Cell>}[]}[] = [
  {
    title: 'Attribution',
    rows: [
      {
        label: 'Line-level AI attribution',
        cells: {
          byline: {mark: 'yes'},
          gitai: {mark: 'yes'},
          entire: {mark: 'yes', text: <code>entire blame</code>},
          cursor: {mark: 'yes', text: 'code written in Cursor'},
        },
      },
      {
        label: 'Agents with native hooks',
        cells: {
          byline: {text: '9, plus a JSON adapter'},
          gitai: {text: '14'},
          entire: {text: '8'},
          cursor: {text: 'Cursor only'},
        },
      },
      {
        label: 'Follows rebase and squash merges',
        cells: {
          byline: {mark: 'yes', text: 'Git hooks and CI'},
          gitai: {mark: 'yes', text: 'daemon and CI'},
          entire: unknown,
          cursor: unknown,
        },
      },
    ],
  },
  {
    title: 'Data boundary',
    rows: [
      {
        label: 'Prompts and transcripts',
        cells: {
          byline: {text: 'Never stored'},
          gitai: {text: 'Linked to every AI line, kept locally by default'},
          entire: {text: 'Full transcripts in Git refs, pushed with your code'},
          cursor: {text: 'Each line links to a chat summary'},
        },
      },
      {
        label: 'Telemetry',
        cells: {
          byline: {text: 'None'},
          gitai: {text: 'Error reports, on by default'},
          entire: {text: 'Off unless you opt in'},
          cursor: unknown,
        },
      },
      {
        label: 'Background activity',
        cells: {
          byline: {text: 'None'},
          gitai: {text: 'Daemon, daily hook check, auto-update'},
          entire: {text: 'Daily version check'},
          cursor: unknown,
        },
      },
      {
        label: 'Account',
        cells: {
          byline: {text: 'Not needed'},
          gitai: {text: 'Not for the CLI'},
          entire: {text: 'Not for the CLI'},
          cursor: {text: 'Enterprise plan'},
        },
      },
    ],
  },
  {
    title: 'What you get',
    rows: [
      {
        label: 'CI gate and SBOM disclosure',
        cells: {
          byline: {
            mark: 'yes',
            text: (
              <>
                <code>check</code>, CycloneDX, SPDX
              </>
            ),
          },
          gitai: unknown,
          entire: unknown,
          cursor: unknown,
        },
      },
      {
        label: 'Team analytics',
        cells: {
          byline: {mark: 'no', text: 'local HTML report'},
          gitai: {mark: 'yes', text: 'Git AI for Teams'},
          entire: {mark: 'yes', text: 'entire.io web app'},
          cursor: {mark: 'yes', text: 'team API'},
        },
      },
      {
        label: 'Editor integration',
        cells: {
          byline: {mark: 'no'},
          gitai: {mark: 'yes', text: 'VS Code family, Emacs'},
          entire: unknown,
          cursor: {mark: 'yes', text: 'built in'},
        },
      },
    ],
  },
];

const choices: Record<ToolKey, string> = {
  byline:
    'Pick it when prompts must never be stored, nothing may run in the background or phone home, and CI or an auditor needs a gate and an SBOM.',
  gitai:
    'Pick it when you want the prompt behind every AI line, the longest agent list, editor blame, and a team product on top.',
  entire: 'Pick it when you want whole agent sessions, prompts and transcripts included, versioned next to each commit.',
  cursor: 'Pick it when your whole team writes code in Cursor on the Enterprise plan.',
};

const markIcon: Record<Mark, IconName> = {yes: 'check', no: 'cross', unknown: 'minus'};
const markLabel: Record<Mark, string> = {yes: 'Yes', no: 'No', unknown: 'Not documented'};

function MarkIcon({mark, labelled = true}: Readonly<{mark: Mark; labelled?: boolean}>) {
  return (
    <span className={clsx(styles.mark, styles[mark])}>
      <Icon name={markIcon[mark]} />
      {labelled && <span className="sr-only">{markLabel[mark]}</span>}
    </span>
  );
}

function CellContent({cell}: Readonly<{cell: Cell}>) {
  return (
    <span className={styles.cell}>
      {cell.mark && <MarkIcon mark={cell.mark} />}
      {cell.text && <span>{cell.text}</span>}
    </span>
  );
}

export default function Compare() {
  return (
    <Section id="compare">
      <SectionHeader eyebrow="How it compares" title="Same question, smaller data boundary">
        Git AI, Entire, and Cursor Blame also attribute AI code line by line. They differ in what they collect, what
        runs in the background, and what they give back.
      </SectionHeader>
      <p className={styles.hint}>Scroll sideways to see all four tools.</p>
      <Reveal className={styles.card}>
        <table className={styles.table}>
          <caption className="sr-only">git-byline compared with Git AI, Entire, and Cursor Blame</caption>
          <thead>
            <tr>
              <th scope="col" className={styles.rowHead}>
                <span className="sr-only">Capability</span>
              </th>
              {tools.map((tool) => (
                <th key={tool.key} scope="col" className={clsx(tool.key === 'byline' && styles.featured)}>
                  <span className={styles.toolName}>
                    {tool.href ? <Link href={tool.href}>{tool.name}</Link> : tool.name}
                  </span>
                  <span className={styles.toolMeta}>{tool.meta}</span>
                </th>
              ))}
            </tr>
          </thead>
          {groups.map((group) => (
            <tbody key={group.title}>
              <tr className={styles.group}>
                <th scope="rowgroup" colSpan={tools.length + 1}>
                  <span>{group.title}</span>
                </th>
              </tr>
              {group.rows.map((row) => (
                <tr key={row.label}>
                  <th scope="row" className={styles.rowHead}>
                    {row.label}
                  </th>
                  {tools.map((tool) => (
                    <td key={tool.key} className={clsx(tool.key === 'byline' && styles.featured)}>
                      <CellContent cell={row.cells[tool.key]} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          ))}
        </table>
      </Reveal>
      <div className={styles.legend}>
        {(Object.keys(markLabel) as Mark[]).map((mark) => (
          <span key={mark} className={styles.legendItem}>
            <MarkIcon mark={mark} labelled={false} />
            {markLabel[mark]}
          </span>
        ))}
      </div>
      <div className={styles.choices}>
        {tools.map((tool, index) => (
          <Reveal
            key={tool.key}
            className={clsx(styles.choice, tool.key === 'byline' && styles.choiceFeatured)}
            delay={index * 80}>
            <h3 className={styles.choiceTitle}>{tool.name}</h3>
            <p className={styles.choiceText}>{choices[tool.key]}</p>
          </Reveal>
        ))}
      </div>
      <p className={styles.footnote}>
        Already on Git AI? git-byline imports and exports Git AI notes, so attribution history comes with you. See{' '}
        <Link to="/docs/interop">interop</Link>.
        <br />
        Checked on {checkedOn} against each project&apos;s public README, documentation, and source. These tools change
        fast, so check their docs before you decide. The longer version is in{' '}
        <Link to="/docs/why-git-byline#market-landscape">why git-byline</Link>.
      </p>
    </Section>
  );
}
