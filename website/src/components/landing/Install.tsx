import {useState} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import CodeBlock from '@theme/CodeBlock';
import Icon from './Icon';
import Reveal from './Reveal';
import {Section} from './Section';
import Window from './Window';
import styles from './Install.module.css';

const raw = 'https://raw.githubusercontent.com/comarch/git-byline/main';

// Commands match the README install paths.
const paths = [
  {
    label: 'macOS and Linux',
    language: 'bash',
    code: `curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 \\\n  ${raw}/install.sh | bash`,
  },
  {
    label: 'Windows',
    language: 'powershell',
    code: `irm ${raw}/install.ps1 | iex`,
  },
  {
    label: 'Factory',
    language: 'bash',
    code: [
      'droid plugin marketplace add comarch/git-byline',
      'droid plugin install git-byline@git-byline --scope user',
      '',
      '# then, inside droid',
      '/git-byline-setup',
    ].join('\n'),
  },
  {
    label: 'Claude Code',
    language: 'text',
    code: ['/plugin marketplace add comarch/git-byline', '/plugin install git-byline@git-byline', '/git-byline-setup'].join(
      '\n',
    ),
  },
  {
    label: 'Gemini CLI',
    language: 'bash',
    code: [
      'gemini extensions install https://github.com/comarch/git-byline',
      '',
      '# restart Gemini CLI, then',
      '/git-byline-setup',
    ].join('\n'),
  },
  {
    label: 'Go',
    language: 'bash',
    code: [
      'git clone https://github.com/comarch/git-byline.git',
      'cd git-byline',
      'CGO_ENABLED=0 go build -trimpath -o git-byline ./cmd/git-byline',
      '',
      '# put git-byline on PATH, then',
      'git byline install-hooks --agent droid --git --user',
    ].join('\n'),
  },
];

const points = [
  'Checksum-verified release binary, no elevated privileges',
  'Factory and Claude Code get a user-level hook for every repository',
  'post-commit annotates, pre-push shares notes with your remote',
  '--local-notes keeps attribution notes on your machine',
];

export default function Install() {
  const [active, setActive] = useState(0);
  const path = paths[active];
  return (
    <Section className={styles.section}>
      <div className={styles.layout}>
        <Reveal className={styles.terminal}>
          <div className={styles.tabs}>
            {paths.map((item, index) => (
              <button
                key={item.label}
                type="button"
                className={clsx(styles.tab, index === active && styles.tabActive)}
                aria-pressed={index === active}
                onClick={() => setActive(index)}>
                {item.label}
              </button>
            ))}
          </div>
          <Window title="Terminal">
            <div className={styles.code}>
              <CodeBlock language={path.language}>{path.code}</CodeBlock>
            </div>
          </Window>
          <div className={styles.next}>
            <p className={styles.nextLabel}>Then work normally</p>
            <CodeBlock language="bash">{'git commit -m "feat: add example"\ngit byline blame src/example.go'}</CodeBlock>
          </div>
        </Reveal>
        <Reveal className={styles.copy} delay={120}>
          <p className={styles.eyebrow}>Install</p>
          <h2 className={styles.title}>Attributed from the next commit</h2>
          <p className={styles.text}>
            The installer verifies the release archive checksum and the binary version, then detects the coding
            agents on your machine. Run it inside a repository and the Git hooks land there too.
          </p>
          <ul className={styles.points}>
            {points.map((point) => (
              <li key={point}>
                <Icon name="check" />
                <span>{point}</span>
              </li>
            ))}
          </ul>
          <Link className={styles.more} to="/docs/install">
            All install paths, including air-gapped
            <Icon name="arrowRight" />
          </Link>
        </Reveal>
      </div>
    </Section>
  );
}
