import {useState, type CSSProperties, type ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import {BarList, Donut, LineMap, StackedBars, type BarRow} from './Charts';
import Icon, {type IconName} from './Icon';
import Reveal from './Reveal';
import {Section, SectionHeader} from './Section';
import {
  agentColor,
  blameFiles,
  commits,
  describe,
  formatShare,
  kindColor,
  kinds,
  range,
  share,
  stats,
  totals,
  type Counts,
  type Kind,
} from './data';
import styles from './Dashboard.module.css';

type Mode = 'lines' | 'share';

const kpis: {kind: Kind; label: string; icon: IconName; note: string}[] = [
  {kind: 'human', label: 'Human', icon: 'lines', note: `${totals.human} lines from ${stats.authors.length} people`},
  {kind: 'ai', label: 'AI', icon: 'sparkle', note: `${totals.ai} lines from ${stats.agents.length} agents`},
  {
    kind: 'human_override',
    label: 'Human override',
    icon: 'userEdit',
    note: `${totals.human_override} agent lines a person rewrote`,
  },
  {
    kind: 'untracked',
    label: 'Untracked',
    icon: 'question',
    note: `${totals.untracked} lines without evidence, never guessed`,
  },
];

function segments(counts: Counts): BarRow['segments'] {
  return kinds.map((kind) => ({kind, value: counts[kind]}));
}

function byValue(a: BarRow, b: BarRow): number {
  return b.value - a.value;
}

const agentRows = stats.agents
  .map(
    (agent): BarRow => ({
      key: agent.agent,
      label: (
        <>
          <span className={styles.dot} style={{background: agentColor(agent.agent)}} />
          {agent.agent}
        </>
      ),
      note: agent.models.map((model) => model.model).join(', '),
      value: agent.lines,
      segments: segments(agent),
    }),
  )
  .sort(byValue);

const peopleRows = stats.authors
  .map((author): BarRow => ({key: author.identity, label: author.identity, note: describe(author), value: author.lines, segments: segments(author)}))
  .sort(byValue);

const fileRows = stats.files
  .map((file): BarRow => ({key: file.path, label: file.path, note: describe(file), value: file.lines, segments: segments(file)}))
  .sort(byValue);

const sessionRows = stats.sessions
  .map(
    (session): BarRow => ({
      key: session.session,
      label: session.session,
      note: `${session.agent}/${session.model}`,
      value: session.lines,
      segments: segments(session),
    }),
  )
  .sort(byValue);

type CardProps = {
  title: string;
  subtitle?: ReactNode;
  action?: ReactNode;
  className?: string;
  delay?: number;
  children: ReactNode;
};

function Card({title, subtitle, action, className, delay, children}: Readonly<CardProps>) {
  return (
    <Reveal className={clsx(styles.card, className)} delay={delay}>
      <div className={styles.cardHead}>
        <div>
          <h3 className={styles.cardTitle}>{title}</h3>
          {subtitle && <p className={styles.cardSubtitle}>{subtitle}</p>}
        </div>
        {action}
      </div>
      {children}
    </Reveal>
  );
}

function Toggle({mode, onChange}: Readonly<{mode: Mode; onChange: (mode: Mode) => void}>) {
  const options: {value: Mode; label: string}[] = [
    {value: 'lines', label: 'Lines'},
    {value: 'share', label: 'Share'},
  ];
  return (
    <fieldset className={styles.toggle} aria-label="Chart unit">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className={clsx(styles.toggleButton, mode === option.value && styles.toggleActive)}
          aria-pressed={mode === option.value}
          onClick={() => onChange(option.value)}>
          {option.label}
        </button>
      ))}
    </fieldset>
  );
}

export default function Dashboard() {
  const {siteConfig} = useDocusaurusContext();
  const repoUrl = String(siteConfig.customFields?.repoUrl);
  const [mode, setMode] = useState<Mode>('lines');
  const [active, setActive] = useState(commits.length - 1);

  return (
    <Section>
      <SectionHeader eyebrow={`git byline stats ${range}`} title="Your AI share, at a glance">
        Aggregate attribution over any revision range without reading a single blob: people, agents, models,
        sessions, files, and commits.
      </SectionHeader>
      <div className={styles.grid}>
        {kpis.map((kpi, index) => (
          <Reveal
            key={kpi.kind}
            className={styles.kpi}
            delay={index * 80}
            style={{'--tone': kindColor[kpi.kind]} as CSSProperties}>
            <div className={styles.kpiHead}>
              <span>{kpi.label}</span>
              <Icon name={kpi.icon} />
            </div>
            <div className={styles.kpiValue}>{formatShare(totals[kpi.kind], totals.lines)}</div>
            <div className={styles.kpiNote}>{kpi.note}</div>
            <div className={styles.kpiTrack}>
              <span className="grow-x" style={{width: `${share(totals[kpi.kind], totals.lines)}%`}} />
            </div>
          </Reveal>
        ))}
        <Card
          className={styles.wide}
          title="Lines per commit"
          subtitle={`${stats.commits.annotated} of ${stats.commits.total} commits annotated`}
          action={<Toggle mode={mode} onChange={setMode} />}>
          <StackedBars commits={commits} mode={mode} active={active} onActive={setActive} />
        </Card>
        <Card className={styles.narrow} title="Attribution" subtitle="Every attributed line in the range" delay={80}>
          <Donut counts={totals} />
        </Card>
        <Card className={styles.wide} title="Line map" subtitle="The three files at HEAD, line by line">
          <LineMap files={blameFiles} />
        </Card>
        <Card className={styles.narrow} title="Agents and models" subtitle="AI and human-override lines" delay={80}>
          <BarList rows={agentRows} total={totals.lines} />
        </Card>
        <Card className={styles.third} title="People" subtitle="From the commit author of each line">
          <BarList rows={peopleRows} total={totals.lines} />
        </Card>
        <Card className={styles.third} title="Files" subtitle="Summed over the notes in the range" delay={80}>
          <BarList rows={fileRows} total={totals.lines} />
        </Card>
        <Card className={styles.third} title="Sessions" subtitle="One agent conversation each" delay={160}>
          <BarList rows={sessionRows} total={totals.lines} />
        </Card>
      </div>
      <p className={styles.footnote}>
        Every number above is real <code>git byline stats --json</code> output for the demo repository that{' '}
        <Link href={`${repoUrl}/blob/main/docs/assets/demo/build-demo-repo.sh`}>build-demo-repo.sh</Link> creates
        from real hook events.
      </p>
    </Section>
  );
}
