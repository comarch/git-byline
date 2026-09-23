import type {ReactNode} from 'react';
import clsx from 'clsx';
import styles from './Window.module.css';

type Props = {
  title: ReactNode;
  children: ReactNode;
  className?: string;
  accent?: boolean;
};

// Window frame shared by the hero, recordings, and code panels. The three
// dots reuse the gradient stops, like the header of the HTML report.
export default function Window({title, children, className, accent = false}: Readonly<Props>) {
  return (
    <div className={clsx(styles.window, accent && styles.accent, className)}>
      <div className={styles.bar}>
        <span className={styles.dots} aria-hidden="true">
          <span />
          <span />
          <span />
        </span>
        <span className={styles.title}>{title}</span>
        <span className={styles.spacer} aria-hidden="true" />
      </div>
      {children}
    </div>
  );
}
