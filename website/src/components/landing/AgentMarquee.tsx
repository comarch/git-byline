import type {CSSProperties} from 'react';
import {agentColor} from './data';
import styles from './AgentMarquee.module.css';

// The nine surfaces with native hooks, and the label each one writes.
const agents = [
  {name: 'Factory', label: 'factory'},
  {name: 'Claude Code', label: 'claude'},
  {name: 'GitHub Copilot', label: 'copilot'},
  {name: 'VS Code Agent', label: 'vscode'},
  {name: 'Cursor', label: 'cursor'},
  {name: 'Codex', label: 'codex'},
  {name: 'Gemini CLI', label: 'gemini'},
  {name: 'Windsurf', label: 'windsurf'},
  {name: 'Grok', label: 'grok'},
];

export default function AgentMarquee() {
  return (
    <section className={styles.strip} aria-labelledby="agents-title">
      <p id="agents-title" className={styles.caption}>
        Native hooks for nine coding agents, one attribution format
      </p>
      <div className={styles.viewport}>
        <ul className={styles.track}>
          {/* The second copy makes the loop seamless; readers hear one. */}
          {[...agents, ...agents].map((agent, index) => (
            <li
              key={index}
              className={styles.agent}
              aria-hidden={index >= agents.length ? true : undefined}
              style={{'--tone': agentColor(agent.label)} as CSSProperties}>
              <span className={styles.dot} />
              <span className={styles.name}>{agent.name}</span>
              <code className={styles.label}>ai:{agent.label}</code>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
