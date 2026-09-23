import clsx from 'clsx';
import type {Step} from './data';
import styles from './Terminal.module.css';

type Props = {
  steps: Step[];
  className?: string;
};

function toneOf(line: string): string | undefined {
  if (line.startsWith('FAIL') || line.startsWith('policy failed')) {
    return styles.bad;
  }
  if (line === 'policy passed' || line.startsWith('verified')) {
    return styles.good;
  }
  return undefined;
}

// Recorded command output. The text stays exactly as the binary printed
// it; only verdict lines get a color.
export default function Terminal({steps, className}: Props) {
  return (
    <div className={clsx(styles.terminal, className)}>
      {steps.map((step) => (
        <div key={step.command} className={styles.step}>
          <div className={styles.command}>
            <span className={styles.prompt} aria-hidden="true">
              $
            </span>
            <code>{step.command}</code>
          </div>
          {step.output && (
            <pre className={styles.output}>
              {step.output.split('\n').map((line, index) => (
                <span key={index} className={toneOf(line)}>
                  {line}
                  {'\n'}
                </span>
              ))}
            </pre>
          )}
          <span className={clsx(styles.exit, step.exit === 0 ? styles.ok : styles.fail)}>exit {step.exit}</span>
        </div>
      ))}
    </div>
  );
}
