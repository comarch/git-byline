import {useEffect, useState} from 'react';
import clsx from 'clsx';
import Icon from './Icon';
import styles from './CopyCommand.module.css';

type Props = {
  command: string;
  className?: string;
};

export default function CopyCommand({command, className}: Readonly<Props>) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) {
      return undefined;
    }
    const timer = window.setTimeout(() => setCopied(false), 1800);
    return () => window.clearTimeout(timer);
  }, [copied]);

  function copy() {
    navigator.clipboard?.writeText(command).then(
      () => setCopied(true),
      () => setCopied(false),
    );
  }

  return (
    <div className={clsx(styles.command, className)}>
      <span className={styles.prompt} aria-hidden="true">
        $
      </span>
      <code className={styles.text}>{command}</code>
      <button type="button" className={styles.button} onClick={copy} title="Copy command">
        <Icon name={copied ? 'check' : 'copy'} />
        <span className="sr-only">Copy command</span>
      </button>
      <span className="sr-only" aria-live="polite">
        {copied ? 'Copied to clipboard' : ''}
      </span>
    </div>
  );
}
