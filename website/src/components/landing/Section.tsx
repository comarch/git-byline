import type {ReactNode} from 'react';
import clsx from 'clsx';
import useBrokenLinks from '@docusaurus/useBrokenLinks';
import Reveal from './Reveal';
import styles from './Section.module.css';

type SectionProps = {
  id?: string;
  className?: string;
  children: ReactNode;
};

export function Section({id, className, children}: Readonly<SectionProps>) {
  // React pages must report their anchors, or links such as /#features
  // fail the broken anchor check.
  useBrokenLinks().collectAnchor(id);
  return (
    <section id={id} className={clsx(styles.section, className)}>
      <div className="container">{children}</div>
    </section>
  );
}

type HeaderProps = {
  eyebrow?: ReactNode;
  title: ReactNode;
  children?: ReactNode;
};

export function SectionHeader({eyebrow, title, children}: Readonly<HeaderProps>) {
  return (
    <Reveal className={styles.header}>
      {eyebrow && <p className={styles.eyebrow}>{eyebrow}</p>}
      <h2 className={styles.title}>{title}</h2>
      {children && <p className={styles.lead}>{children}</p>}
    </Reveal>
  );
}
