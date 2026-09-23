import {useState, type CSSProperties} from 'react';
import clsx from 'clsx';
import ClassBar from './ClassBar';
import Terminal from './Terminal';
import Window from './Window';
import {blameFiles, commits, kindColor, kindLabel, kinds, range, stats, terminal, type BlameFile} from './data';
import styles from './HeroWindow.module.css';

const views = [
  {id: 'blame', label: 'Blame'},
  {id: 'gate', label: 'Policy gate'},
  {id: 'audit', label: 'Verify and disclose'},
] as const;

type View = (typeof views)[number]['id'];

function Blame({file}: {file: BlameFile}) {
  // Sized from the widest label, like the terminal report.
  const labelWidth = Math.max(...file.lines.map((line) => line.label.length));
  return (
    <div className={styles.blame}>
      <div className={styles.command}>
        <span className={styles.prompt} aria-hidden="true">
          $
        </span>{' '}
        git byline blame {file.path}
      </div>
      <div className={styles.legend}>
        {kinds
          .filter((kind) => file.counts[kind] > 0)
          .map((kind) => (
            <span key={kind} className={styles.legendItem}>
              <span className={styles.swatch} style={{background: kindColor[kind]}} />
              {kindLabel[kind]} <b>{file.counts[kind]}</b>
            </span>
          ))}
      </div>
      <div className={styles.lines} style={{'--label': `${labelWidth}ch`} as CSSProperties}>
        {file.lines.map((line, index) => (
          <div
            key={`${file.path}:${line.number}`}
            className={styles.line}
            style={{'--tone': kindColor[line.kind], '--index': index} as CSSProperties}>
            <span className={styles.label}>{line.label}</span>
            <span className={styles.number}>{line.number}</span>
            <code className={styles.code}>{line.code}</code>
          </div>
        ))}
      </div>
    </div>
  );
}

export default function HeroWindow() {
  const [view, setView] = useState<View>('blame');
  const [fileIndex, setFileIndex] = useState(0);

  function openFile(index: number) {
    setFileIndex(index);
    setView('blame');
  }

  return (
    <Window accent className={styles.window} title="git byline · demo repository">
      <div className={styles.app}>
        <aside className={styles.sidebar}>
          <p className={styles.group}>Files at HEAD</p>
          <div className={styles.files}>
            {blameFiles.map((file, index) => {
              const active = view === 'blame' && index === fileIndex;
              return (
                <button
                  key={file.path}
                  type="button"
                  className={clsx(styles.file, active && styles.active)}
                  aria-pressed={active}
                  onClick={() => openFile(index)}>
                  <span className={styles.fileHead}>
                    <span>{file.path}</span>
                    <span className={styles.fileCount}>{file.lines.length}</span>
                  </span>
                  <ClassBar counts={file.counts} />
                </button>
              );
            })}
          </div>
          <p className={styles.group}>Commits</p>
          <ol className={styles.commits}>
            {[...commits].reverse().map((commit) => (
              <li key={commit.id} title={commit.subject}>
                <code>{commit.short}</code>
                <span>{commit.subject}</span>
              </li>
            ))}
          </ol>
        </aside>
        <div className={styles.main}>
          <div className={styles.toolbar}>
            <div className={styles.views}>
              {views.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className={clsx(styles.view, view === item.id && styles.viewActive)}
                  aria-pressed={view === item.id}
                  onClick={() => setView(item.id)}>
                  {item.label}
                </button>
              ))}
            </div>
            <span className={styles.range}>{range}</span>
          </div>
          <div className={styles.panel}>
            {view === 'blame' && <Blame file={blameFiles[fileIndex]} />}
            {view === 'gate' && <Terminal steps={terminal.gate} />}
            {view === 'audit' && <Terminal steps={[...terminal.verify, ...terminal.disclosure]} />}
          </div>
          <div className={styles.status}>
            <span>
              <span className={styles.dot} />
              refs/notes/byline
            </span>
            <span>
              {stats.commits.annotated} of {stats.commits.total} commits annotated
            </span>
          </div>
        </div>
      </div>
    </Window>
  );
}
