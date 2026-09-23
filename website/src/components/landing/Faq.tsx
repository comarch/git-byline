import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Icon from './Icon';
import Reveal from './Reveal';
import {Section, SectionHeader} from './Section';
import styles from './Faq.module.css';

const questions: {question: string; answer: ReactNode}[] = [
  {
    question: 'Is git-byline an AI detector?',
    answer: (
      <>
        No. It records agent edits when they happen, through the hooks of supported agents. It never guesses from code
        style, tokens, or statistical classifiers. When evidence is missing, the line is <code>untracked</code>.
      </>
    ),
  },
  {
    question: 'What leaves my machine?',
    answer: (
      <>
        Checkpoints and snapshot blobs stay local. After <code>install-hooks --git</code>, the managed{' '}
        <code>pre-push</code> hook publishes <code>refs/notes/byline</code> to the same remote on ordinary pushes: line
        ranges, paths, agent and model names, human identity tokens, session identifiers, and timestamps. Install with{' '}
        <code>--local-notes</code> to keep notes local.
      </>
    ),
  },
  {
    question: 'Are prompts or transcripts stored?',
    answer: (
      <>
        No. Checkpoint JSON and notes never contain raw hook payloads, prompts, transcripts, tool responses, or
        environment dumps. Snapshot blobs contain file content and stay in your local object database unless you
        transfer those refs yourself. See the <Link to="/docs/security-model">security model</Link>.
      </>
    ),
  },
  {
    question: 'Which agents are supported?',
    answer: (
      <>
        Factory, Claude Code, GitHub Copilot, VS Code Agent, Cursor, Codex, Gemini CLI, Windsurf, and Grok, through
        native project hooks. Other tools can report edits through the generic <code>agent-v1</code> JSON adapter.
        Details are in <Link to="/docs/compatibility">compatibility</Link>.
      </>
    ),
  },
  {
    question: 'Does attribution survive rebase and squash merges?',
    answer: (
      <>
        Yes. Managed hooks reproject attribution for rebase, amend, cherry-pick, reset, branch switch, and stash. Squash
        and rebase merges on GitHub or GitLab are rebuilt by a workflow or the <Link to="/docs/github-action">
        marketplace action</Link>. Content that cannot be matched becomes <code>untracked</code>.
      </>
    ),
  },
  {
    question: 'Can I enforce an AI share limit in CI?',
    answer: (
      <>
        Yes. <code>git byline check --max-ai-percent 30</code> exits with status 1 on a violation and 0 on success. Add{' '}
        <code>--max-untracked-percent</code>, <code>--require-note</code>, or <code>--json</code> as needed. Flags only,
        no configuration file.
      </>
    ),
  },
  {
    question: 'Does it work in air-gapped environments?',
    answer: (
      <>
        Yes. Every command runs offline except <code>update</code> and <code>version --check</code>. On an air-gapped
        machine, download the release archive and <code>checksums.txt</code> yourself, then run{' '}
        <code>git-byline update --archive FILE --checksums checksums.txt</code>.
      </>
    ),
  },
  {
    question: 'Is the disclosure output a compliance certificate?',
    answer: (
      <>
        No. It is machine-readable input for an AI content disclosure process, in native JSON, CycloneDX 1.6, or SPDX
        3.0.1 with the AI profile. <code>verify --deep</code> proves it against the actual blobs.
      </>
    ),
  },
  {
    question: 'How is it different from Git AI?',
    answer: (
      <>
        Both use agent checkpoints, line-level provenance, and Git notes. Git AI adds prompt-linked provenance and
        lifecycle observability. git-byline excludes prompts, transcripts, cloud sync, and hosted analytics, and covers
        history rewrites, shell-written files, forge merges, policy gates, and disclosure output. It also reads and
        writes the Git AI format; see <Link to="/docs/interop">interop</Link>.
      </>
    ),
  },
];

export default function Faq() {
  const {siteConfig} = useDocusaurusContext();
  const repoUrl = String(siteConfig.customFields?.repoUrl);
  return (
    <Section>
      <SectionHeader eyebrow="FAQ" title="Questions, answered" />
      <div className={styles.list}>
        {questions.map((item, index) => (
          <Reveal key={item.question} delay={Math.min(index, 4) * 50}>
            <details className={styles.item}>
              <summary className={styles.question}>
                <span>{item.question}</span>
                <Icon name="chevronDown" className={styles.chevron} />
              </summary>
              <p className={styles.answer}>{item.answer}</p>
            </details>
          </Reveal>
        ))}
      </div>
      <p className={styles.more}>
        Still deciding? Read <Link to="/docs/why-git-byline">why git-byline</Link> or ask in{' '}
        <Link href={`${repoUrl}/discussions`}>GitHub Discussions</Link>.
      </p>
    </Section>
  );
}
