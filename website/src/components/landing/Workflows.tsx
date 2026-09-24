import {useEffect, useState, type CSSProperties, type ReactNode} from 'react';
import clsx from 'clsx';
import CodeBlock from '@theme/CodeBlock';
import Icon, {type IconName} from './Icon';
import Reveal from './Reveal';
import {Section, SectionHeader} from './Section';
import Window from './Window';
import {blameFiles, kindColor} from './data';
import styles from './Workflows.module.css';

const rewrites = [
  {operation: 'rebase, amend, cherry-pick', via: ['post-rewrite', 'post-commit']},
  {operation: 'merge, pull', via: ['post-merge'], note: 'first parent is authoritative'},
  {operation: 'fast-forward pull', via: ['reference-transaction'], note: 'pulled commits keep their original notes'},
  {operation: 'reset, branch switch', via: ['reference-transaction', 'post-checkout']},
  {operation: 'stash push, pop, apply', via: ['refs/notes/byline-stash']},
];

function RewritesPanel() {
  return (
    <div className={styles.panelBody}>
      <ul className={styles.events}>
        {rewrites.map((row) => (
          <li key={row.operation} className={styles.event}>
            <span className={styles.operation}>{row.operation}</span>
            <span className={styles.via}>
              {row.via.map((hook) => (
                <code key={hook}>{hook}</code>
              ))}
              {row.note && <span className={styles.viaNote}>{row.note}</span>}
            </span>
          </li>
        ))}
      </ul>
      <p className={styles.panelNote}>
        Unmatched rewritten content becomes <code>untracked</code>, never reassigned by guess.
      </p>
    </div>
  );
}

// The two hook events are the ones docs/assets/demo/build-demo-repo.sh
// sends; the blame lines below are what they produced.
function ShellPanel() {
  const invoice = blameFiles.find((file) => file.path === 'src/invoice.go');
  const written = invoice ? invoice.lines.filter((line) => line.label.startsWith('ai:codex/')) : [];
  const steps = [
    {tag: 'shell_pre', code: '{"type":"shell_pre","agent_name":"codex"}', text: 'records dirty paths and their blob IDs'},
    {tag: 'shell', text: 'codex writes src/invoice.go through a shell command'},
    {
      tag: 'shell_post',
      code: '{"type":"shell_post","agent_name":"codex","model":"gpt-5-codex","conversation_id":"pr-4812-codex"}',
      text: 'keeps only paths whose blob changed',
    },
  ];
  return (
    <div className={styles.panelBody}>
      <ol className={styles.timeline}>
        {steps.map((step) => (
          <li key={step.tag}>
            <span className={styles.tag}>{step.tag}</span>
            <span className={styles.stepText}>{step.text}</span>
            {step.code && <code className={styles.payload}>{step.code}</code>}
          </li>
        ))}
      </ol>
      <div className={styles.result}>
        <p className={styles.resultCommand}>
          <span aria-hidden="true">$</span> git byline blame src/invoice.go
        </p>
        {written.map((line) => (
          <div key={line.number} className={styles.resultLine} style={{'--tone': kindColor[line.kind]} as CSSProperties}>
            <span className={styles.resultLabel}>{line.label}</span>
            <span className={styles.resultNumber}>{line.number}</span>
            <code>{line.code}</code>
          </div>
        ))}
      </div>
    </div>
  );
}

// The calling workflow from action/README.md, unchanged.
const forgeWorkflow = `name: git-byline forge attribution

on:
  pull_request:
    types:
      - closed
    branches:
      - main

concurrency:
  group: git-byline-notes-\${{ github.repository }}-\${{ github.event.pull_request.base.ref }}
  cancel-in-progress: false

permissions:
  contents: write

jobs:
  reconstruct:
    if: github.event.pull_request.merged == true
    runs-on: ubuntu-latest
    steps:
      - uses: comarch/git-byline@v1`;

const items: {id: string; icon: IconName; title: string; text: ReactNode; window: string; panel: ReactNode}[] = [
  {
    id: 'rewrites',
    icon: 'branch',
    title: 'Survives history rewrites',
    text: 'Rebase, amend, cherry-pick, reset, branch switch, and stash transitions reproject attribution onto the new content through managed Git hooks.',
    window: 'managed Git hooks',
    panel: <RewritesPanel />,
  },
  {
    id: 'shell',
    icon: 'terminal',
    title: 'Files agents write through the shell',
    text: (
      <>
        <code>shell_pre</code> and <code>shell_post</code> checkpoints snapshot the dirty set and attribute only paths
        whose blobs actually changed, so unrelated human edits are not claimed.
      </>
    ),
    window: 'shell checkpoints',
    panel: <ShellPanel />,
  },
  {
    id: 'forge',
    icon: 'merge',
    title: 'Squash and rebase merges on the forge',
    text: 'Forge merges create commits that never passed a local hook. A least-privilege workflow rebuilds attribution from the pull request commits and pushes only refs/notes/byline.',
    window: 'forge attribution workflow',
    panel: <CodeBlock language="yaml">{forgeWorkflow}</CodeBlock>,
  },
  {
    id: 'interop',
    icon: 'exchange',
    title: 'Interoperate, do not lock in',
    text: 'Read and write the Git AI Standard v3 authorship format at refs/notes/ai, and write Agent Trace 0.1 records. Import never overwrites a different existing note.',
    window: 'export and import',
    panel: (
      <CodeBlock language="bash">
        {[
          'git byline export --format gitai --output authorship.txt',
          'git byline import --format gitai --range HEAD~5..HEAD --dry-run',
          'git byline export --format agent-trace --output trace.json',
        ].join('\n')}
      </CodeBlock>
    ),
  },
  {
    id: 'update',
    icon: 'download',
    title: 'Updates you can verify',
    text: 'update downloads the newest release from the pinned repository, checks it against checksums.txt, swaps the binary, and refreshes the managed hooks. Air-gapped machines stage the archive themselves.',
    window: 'git-byline update',
    panel: (
      <CodeBlock language="bash">
        {[
          'git-byline update              # verify, swap, refresh hooks',
          'git-byline update --dry-run    # verify without replacing anything',
          'git-byline version --check     # is a newer release out?',
          '',
          '# air-gapped: bring the archive and checksums.txt yourself',
          'git-byline update --archive git-byline_1.0.0_linux_amd64.tar.gz \\',
          '  --checksums checksums.txt',
        ].join('\n')}
      </CodeBlock>
    ),
  },
];

export default function Workflows() {
  const [active, setActive] = useState(0);
  const [auto, setAuto] = useState(true);

  // Auto-advance moves content on its own, so it stays off for readers who
  // ask for reduced motion.
  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setAuto(false);
    }
  }, []);

  function select(index: number) {
    setActive(index);
    setAuto(false);
  }

  const current = items[active];
  return (
    <Section>
      <SectionHeader eyebrow="Built for real Git" title="Attribution that survives real workflows">
        Rewrites, squash merges, shell commands, and other tools. Provenance follows the code instead of breaking on
        the first rebase.
      </SectionHeader>
      <div className={styles.layout}>
        <Reveal className={styles.list}>
          {items.map((item, index) => {
            const open = index === active;
            return (
              <div key={item.id} className={clsx(styles.item, open && styles.open)}>
                <button type="button" className={styles.trigger} aria-expanded={open} onClick={() => select(index)}>
                  <span className={styles.icon}>
                    <Icon name={item.icon} />
                  </span>
                  <span className={styles.itemTitle}>{item.title}</span>
                </button>
                <div className={styles.body}>
                  <div>
                    <p className={styles.text}>{item.text}</p>
                    {auto && open && (
                      <span
                        className={styles.progress}
                        onAnimationEnd={() => setActive((value) => (value + 1) % items.length)}
                      />
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </Reveal>
        <Reveal className={styles.panel} delay={120}>
          <Window title={current.window}>
            <div key={current.id} className={styles.panelContent}>
              {current.panel}
            </div>
          </Window>
        </Reveal>
      </div>
    </Section>
  );
}
