import {useState, type CSSProperties} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import auditGif from '@site/../docs/assets/git-byline-audit.gif';
import dashboardGif from '@site/../docs/assets/git-byline-dashboard.gif';
import statsGif from '@site/../docs/assets/git-byline-stats.gif';
import tourGif from '@site/../docs/assets/git-byline-tour.gif';
import Icon from './Icon';
import Reveal from './Reveal';
import {Section, SectionHeader} from './Section';
import Window from './Window';
import styles from './Recordings.module.css';

// The README animations. Alt text matches the README, and the sizes are
// the recorded frame sizes, so the page reserves space before they load.
const chapters = [
  {
    src: statsGif,
    width: 1250,
    height: 630,
    layout: 'side',
    command: 'git byline stats HEAD~6..HEAD',
    title: 'Who wrote what, per person and per agent',
    text: 'Aggregates notes over any revision range without reading a single blob: author classes, agents, models, people, sessions, files, and commits, in deterministic order. The JSON output feeds your own tooling.',
    code: 'git byline stats --json HEAD~10..HEAD',
    link: '/docs/guide#who-wrote-what-per-person-and-per-agent',
    alt: 'git byline stats aggregating a commit range into totals, agents, models, authors, sessions, files, and commits, then the same data as JSON',
  },
  {
    src: auditGif,
    width: 1250,
    height: 162,
    layout: 'stacked',
    command: 'git byline check',
    title: 'A gate, not a dashboard nobody opens',
    text: 'check is the gate, verify is the proof, and disclosure is the artifact. Flags only: no configuration file and no new format to maintain.',
    code: 'git byline check --max-ai-percent 30   # exit 1 on violation\ngit byline verify --deep               # prove notes against real blobs\ngit byline disclosure --format cyclonedx --output sbom.json',
    link: '/docs/guide#a-gate-not-a-dashboard-nobody-opens',
    alt: 'git byline check failing an AI share policy with exit code 1, passing at a higher limit, then verify and a disclosure export read back with jq',
  },
  {
    src: dashboardGif,
    width: 1280,
    height: 820,
    layout: 'side',
    command: 'git byline dashboard',
    title: 'A local dashboard, no hosted service',
    text: 'One self-contained HTML file: attribution classes, contribution sources, evidence health, and per-line provenance. No CDN, font, image, script, or API is loaded, so it opens the same way offline in five years.',
    code: 'git byline dashboard --range HEAD~10..HEAD',
    link: '/docs/guide#local-dashboard-no-hosted-service',
    alt: 'The self-contained git-byline dashboard switching between attribution classes, contribution sources, evidence health, per-line provenance, and the range report with its commit trend and people breakdown',
  },
] as const;

type MediaProps = {src: string; width: number; height: number; alt: string};

// A GIF cannot be paused, so with reduced motion the recording waits for a
// click instead of playing on its own.
function Media({src, width, height, alt}: Readonly<MediaProps>) {
  const [playing, setPlaying] = useState(false);
  return (
    <div
      className={clsx(styles.player, playing && styles.playing)}
      style={{'--ratio': `${width} / ${height}`} as CSSProperties}>
      <img className={styles.media} src={src} width={width} height={height} loading="lazy" decoding="async" alt={alt} />
      <button type="button" className={styles.play} onClick={() => setPlaying(true)}>
        <Icon name="play" />
        Play recording
      </button>
    </div>
  );
}

export default function Recordings() {
  const {siteConfig} = useDocusaurusContext();
  const repoUrl = String(siteConfig.customFields?.repoUrl);
  return (
    <Section>
      <SectionHeader eyebrow="Recorded, not mocked" title="Real output from the real binary">
        Every frame comes from the binary built from this repository, run against a demo repository built from real
        hook events. No mock output, no retouching.
      </SectionHeader>
      <Reveal className={styles.tour}>
        <Window accent title="git byline blame · stats · check">
          <Media
            src={tourGif}
            width={1250}
            height={486}
            alt="A four-view tour: line attribution with human identity, agent model, and human-override ranges; a close-up of untracked merge content next to two named people; a range aggregate with per-person totals; and the policy gate failing with exit code 1, then passing, then exporting Git AI authorship"
          />
        </Window>
        <p className={styles.caption}>
          Four views, two of them close-ups: line attribution, who wrote what, the range aggregate, and the policy
          gate with its exit code.
        </p>
      </Reveal>
      <div className={styles.chapters}>
        {chapters.map((chapter) => (
          <div key={chapter.command} className={clsx(styles.chapter, styles[chapter.layout])}>
            <Reveal className={styles.copy}>
              <p className={styles.eyebrow}>{chapter.command}</p>
              <h3 className={styles.title}>{chapter.title}</h3>
              <p className={styles.text}>{chapter.text}</p>
              <pre className={styles.code}>
                <code>{chapter.code}</code>
              </pre>
              <Link className={styles.more} to={chapter.link}>
                Read more in the guide
                <Icon name="arrowRight" />
              </Link>
            </Reveal>
            <Reveal className={styles.frame} delay={120}>
              <Window title={chapter.command}>
                <Media src={chapter.src} width={chapter.width} height={chapter.height} alt={chapter.alt} />
              </Window>
            </Reveal>
          </div>
        ))}
      </div>
      <p className={styles.footnote}>
        Reproduce every recording with{' '}
        <Link href={`${repoUrl}/blob/main/docs/assets/demo/record-media.sh`}>docs/assets/demo/record-media.sh</Link>.
      </p>
    </Section>
  );
}
