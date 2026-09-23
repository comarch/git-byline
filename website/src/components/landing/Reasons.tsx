import type {ReactNode} from 'react';
import Icon, {type IconName} from './Icon';
import Reveal from './Reveal';
import {Section, SectionHeader} from './Section';
import styles from './Reasons.module.css';

const reasons: {icon: IconName; title: string; text: ReactNode}[] = [
  {
    icon: 'eye',
    title: 'Evidence, not heuristics',
    text: 'Pre-edit and post-edit snapshots establish provenance when the edit happens. Nothing is inferred from code style, tokens, or classifiers.',
  },
  {
    icon: 'shield',
    title: 'A gate for CI',
    text: (
      <>
        <code>git byline check</code> fails a build on an AI or untracked share limit, with a stable exit code and JSON
        output.
      </>
    ),
  },
  {
    icon: 'fileCheck',
    title: 'Proof an auditor accepts',
    text: (
      <>
        Disclosure documents in native JSON, CycloneDX 1.6, or SPDX 3.0.1, plus <code>verify --deep</code>, which proves
        them against the actual blobs.
      </>
    ),
  },
  {
    icon: 'cloudOff',
    title: 'Ready for air-gapped estates',
    text: (
      <>
        No cloud, account, daemon, or telemetry. Commands run offline except explicit <code>update</code> and{' '}
        <code>version --check</code>, and notes travel only with your own <code>git push</code>.
      </>
    ),
  },
  {
    icon: 'lock',
    title: 'No vendor in the data path',
    text: 'Prompts and transcripts are never stored. Metadata travels only through Git, to the remote you already trust.',
  },
  {
    icon: 'chip',
    title: 'One pure Go binary',
    text: 'No CGo and no language runtime. Linux, macOS, and Windows on amd64 and arm64.',
  },
];

export default function Reasons() {
  return (
    <Section>
      <SectionHeader eyebrow="Why teams deploy it" title="Answers with evidence, not estimates">
        How much of this release is AI-written? Where should review effort go? Can we show an auditor? Answer from
        observed provenance, with nothing new to host.
      </SectionHeader>
      <div className={styles.grid}>
        {reasons.map((reason, index) => (
          <Reveal key={reason.title} delay={(index % 3) * 90}>
            <div className={styles.card}>
              <span className={styles.icon}>
                <Icon name={reason.icon} />
              </span>
              <h3 className={styles.title}>{reason.title}</h3>
              <p className={styles.text}>{reason.text}</p>
            </div>
          </Reveal>
        ))}
      </div>
    </Section>
  );
}
