import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import HeroWindow from './HeroWindow';
import Icon from './Icon';
import styles from './Hero.module.css';

export default function Hero() {
  const {siteConfig} = useDocusaurusContext();
  const version = String(siteConfig.customFields?.version);
  const repoUrl = String(siteConfig.customFields?.repoUrl);
  return (
    <header className={styles.hero}>
      <div className={styles.backdrop} aria-hidden="true" />
      <div className={styles.grid} aria-hidden="true" />
      <div className="container">
        <div className={styles.intro}>
          <Link className={styles.badge} to="/docs/github-action">
            <span className={styles.badgeTag}>v{version}</span>
            <span>Forge merge attribution is on GitHub Marketplace</span>
            <Icon name="arrowRight" className={styles.badgeArrow} />
          </Link>
          <h1 className={styles.title}>
            Line-level proof of <span className={clsx('gradient-text', styles.accent)}>who wrote your code</span>
          </h1>
          <p className={styles.subtitle}>
            Which person, which agent, which model. git-byline observes edits as they happen and keeps line-level
            provenance in Git notes. No cloud, no account, no telemetry.
          </p>
          <div className={styles.actions}>
            <Link className="byline-button byline-button--primary" to="/docs/install">
              Install git-byline
              <Icon name="arrowRight" />
            </Link>
            <Link className="byline-button byline-button--ghost" href={repoUrl}>
              <Icon name="github" />
              View on GitHub
            </Link>
          </div>
          <p className={styles.meta}>MIT licensed · one pure Go binary · macOS, Linux, and Windows</p>
        </div>
        <HeroWindow />
      </div>
    </header>
  );
}
