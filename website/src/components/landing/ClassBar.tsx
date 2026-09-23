import clsx from 'clsx';
import {describe, kindColor, kinds, type Counts} from './data';
import styles from './ClassBar.module.css';

type Props = {
  counts: Counts;
  className?: string;
};

// One thin bar split by attribution state, sized by line count.
export default function ClassBar({counts, className}: Props) {
  return (
    <span className={clsx(styles.bar, className)} role="img" aria-label={describe(counts)}>
      {kinds
        .filter((kind) => counts[kind] > 0)
        .map((kind) => (
          <span key={kind} style={{flexGrow: counts[kind], background: kindColor[kind]}} />
        ))}
    </span>
  );
}
