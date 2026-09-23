import Link from '@docusaurus/Link';
import CopyCommand from './CopyCommand';
import Icon from './Icon';
import Reveal from './Reveal';
import styles from './CallToAction.module.css';

const installCommand =
  "curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 https://raw.githubusercontent.com/comarch/git-byline/main/install.sh | bash";

export default function CallToAction() {
  return (
    <section className={styles.cta}>
      <div className="container">
        <Reveal className={styles.card}>
          <div className={styles.glow} aria-hidden="true" />
          <h2 className={styles.title}>
            Know who wrote <span className="gradient-text">every line</span>.
          </h2>
          <p className={styles.text}>
            One command installs a checksum-verified binary and the hooks, so your next commit is attributed. No
            account, no daemon, no telemetry.
          </p>
          <CopyCommand className={styles.command} command={installCommand} />
          <div className={styles.actions}>
            <Link className="byline-button byline-button--primary" to="/docs/install">
              Installation guide
              <Icon name="arrowRight" />
            </Link>
            <Link className="byline-button byline-button--ghost" to="/docs/guide">
              Read the product guide
            </Link>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
