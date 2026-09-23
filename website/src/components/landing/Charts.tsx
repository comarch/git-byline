import {useState, type CSSProperties, type ReactNode} from 'react';
import clsx from 'clsx';
import {
  describe,
  formatShare,
  kindColor,
  kindLabel,
  kinds,
  share,
  shortDate,
  type BlameFile,
  type BlameLine,
  type Commit,
  type Counts,
  type Kind,
} from './data';
import styles from './Charts.module.css';

const frame = {width: 640, height: 240, top: 12, right: 8, bottom: 8, left: 40};

type StackedBarsProps = {
  commits: Commit[];
  mode: 'lines' | 'share';
  active: number;
  onActive: (index: number) => void;
};

// Lines per commit, stacked by attribution state. The commit buttons under
// the plot double as the x axis and as the keyboard way into the data.
export function StackedBars({commits, mode, active, onActive}: StackedBarsProps) {
  const plotWidth = frame.width - frame.left - frame.right;
  const plotHeight = frame.height - frame.top - frame.bottom;
  const baseline = frame.top + plotHeight;
  const largest = Math.max(...commits.map((commit) => commit.lines));
  const max = mode === 'share' ? 100 : Math.max(10, Math.ceil(largest / 10) * 10);
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((step) => step * max);
  const slot = plotWidth / commits.length;
  const barWidth = Math.min(58, slot * 0.56);
  const selected = commits[active];
  const tickY = (tick: number) => baseline - (tick / max) * plotHeight;

  return (
    <div className={styles.stacked}>
      <div className={styles.plotWrap}>
        <svg className={styles.plot} viewBox={`0 0 ${frame.width} ${frame.height}`} aria-hidden="true">
          {ticks.map((tick) => (
            <line
              key={tick}
              className={styles.gridLine}
              x1={frame.left}
              x2={frame.width - frame.right}
              y1={tickY(tick)}
              y2={tickY(tick)}
            />
          ))}
          {commits.map((commit, index) => {
            const x = frame.left + slot * index + (slot - barWidth) / 2;
            const scale = mode === 'share' ? 100 / commit.lines : 1;
            let top = baseline;
            return (
              <g key={commit.id} onMouseEnter={() => onActive(index)}>
                <rect
                  className={clsx(styles.column, index === active && styles.columnActive)}
                  x={frame.left + slot * index + 4}
                  y={frame.top}
                  width={slot - 8}
                  height={plotHeight}
                  rx={8}
                />
                <g className="grow-y" style={{transitionDelay: `${index * 70}ms`}}>
                  {kinds.map((kind) => {
                    const height = ((commit[kind] * scale) / max) * plotHeight;
                    if (height <= 0) {
                      return null;
                    }
                    top -= height;
                    return (
                      <rect
                        key={kind}
                        x={x}
                        y={top + 1}
                        width={barWidth}
                        height={Math.max(height - 2, 1)}
                        rx={3}
                        fill={kindColor[kind]}
                      />
                    );
                  })}
                </g>
              </g>
            );
          })}
        </svg>
        {/* HTML, not SVG text, so the labels stay readable when the chart
            scales down on a phone. */}
        <div className={styles.ticks} style={{width: `${(frame.left / frame.width) * 100}%`}} aria-hidden="true">
          {ticks.map((tick) => (
            <span key={tick} style={{top: `${(tickY(tick) / frame.height) * 100}%`}}>
              {mode === 'share' ? `${tick}%` : tick}
            </span>
          ))}
        </div>
      </div>
      <div
        className={styles.axis}
        style={{
          paddingLeft: `${(frame.left / frame.width) * 100}%`,
          paddingRight: `${(frame.right / frame.width) * 100}%`,
        }}>
        {commits.map((commit, index) => (
          <button
            key={commit.id}
            type="button"
            className={clsx(styles.axisButton, index === active && styles.axisActive)}
            aria-pressed={index === active}
            onMouseEnter={() => onActive(index)}
            onFocus={() => onActive(index)}
            onClick={() => onActive(index)}>
            <code>{commit.short}</code>
            <span>{shortDate(commit.date)}</span>
          </button>
        ))}
      </div>
      <div className={styles.detail} aria-live="polite">
        <p className={styles.detailHead}>
          <code>{selected.short}</code>
          <span>{selected.subject}</span>
        </p>
        <p className={styles.detailMeta}>
          {selected.author}, {shortDate(selected.date)}, {selected.lines} lines
        </p>
        <ul className={styles.counts}>
          {kinds.map((kind) => (
            <li key={kind}>
              <span className={styles.swatch} style={{background: kindColor[kind]}} />
              {kindLabel[kind]} <b>{mode === 'share' ? formatShare(selected[kind], selected.lines) : selected[kind]}</b>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

// Share of all lines per state, drawn as dashed circle segments.
export function Donut({counts}: {counts: Counts}) {
  const radius = 76;
  const circumference = 2 * Math.PI * radius;
  const gap = 3;
  let offset = 0;
  return (
    <div className={styles.donutWrap}>
      <svg className={styles.donut} viewBox="0 0 200 200" role="img" aria-label={`${counts.lines} lines: ${describe(counts)}`}>
        <circle className={styles.donutTrack} cx="100" cy="100" r={radius} />
        <g className="grow-spin">
          {kinds.map((kind) => {
            const length = (counts[kind] / counts.lines) * circumference;
            const segment = (
              <circle
                key={kind}
                cx="100"
                cy="100"
                r={radius}
                fill="none"
                stroke={kindColor[kind]}
                strokeWidth={16}
                strokeDasharray={`${Math.max(length - gap, 0)} ${circumference}`}
                strokeDashoffset={-offset}
                transform="rotate(-90 100 100)"
              />
            );
            offset += length;
            return segment;
          })}
        </g>
        <text className={styles.donutValue} x="100" y="100" textAnchor="middle">
          {counts.lines}
        </text>
        <text className={styles.donutLabel} x="100" y="124" textAnchor="middle">
          lines
        </text>
      </svg>
      <ul className={styles.legend}>
        {kinds.map((kind) => (
          <li key={kind}>
            <span className={styles.swatch} style={{background: kindColor[kind]}} />
            <span className={styles.legendName}>{kindLabel[kind]}</span>
            <b>{counts[kind]}</b>
            <span className={styles.legendShare}>{formatShare(counts[kind], counts.lines)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// Each line of each file as a bar: indent and length follow the source,
// color follows attribution. A minimap of who wrote what.
export function LineMap({files}: {files: BlameFile[]}) {
  const [hover, setHover] = useState<{path: string; line: BlameLine} | null>(null);
  return (
    <div>
      <div className={styles.maps}>
        {files.map((file) => (
          <figure key={file.path} className={styles.map}>
            <figcaption className={styles.mapCaption}>
              <code>{file.path}</code>
              <span>{file.lines.length} lines</span>
            </figcaption>
            <div
              className={styles.mapLines}
              role="img"
              aria-label={`${file.path}: ${describe(file.counts)}`}
              onMouseLeave={() => setHover(null)}>
              {file.lines.map((line, index) => {
                const indent = /^\t*/.exec(line.code)?.[0].length ?? 0;
                const length = line.code.trim().length;
                const left = indent * 7;
                const width = length === 0 ? 3 : Math.min(100 - left, 6 + length * 1.25);
                return (
                  <span
                    key={line.number}
                    className={clsx(styles.mapLine, length === 0 && styles.mapBlank, 'grow-x')}
                    style={
                      {
                        '--tone': kindColor[line.kind],
                        marginLeft: `${left}%`,
                        width: `${width}%`,
                        transitionDelay: `${index * 14}ms`,
                      } as CSSProperties
                    }
                    onMouseEnter={() => setHover({path: file.path, line})}
                  />
                );
              })}
            </div>
          </figure>
        ))}
      </div>
      <p className={styles.mapDetail}>
        {hover ? (
          <>
            <code style={{color: kindColor[hover.line.kind]}}>{hover.line.label}</code>
            <span>
              {hover.path}:{hover.line.number}
            </span>
            <code className={styles.mapCode}>{hover.line.code.trim()}</code>
          </>
        ) : (
          <span>Point at a line to see who wrote it.</span>
        )}
      </p>
    </div>
  );
}

export type BarRow = {
  key: string;
  label: ReactNode;
  note?: ReactNode;
  value: number;
  segments: {kind: Kind; value: number}[];
};

// Horizontal bars, longest row first. Length is relative to the longest
// row; the percentage is the share of all lines in the range.
export function BarList({rows, total}: {rows: BarRow[]; total: number}) {
  const max = Math.max(...rows.map((row) => row.value));
  return (
    <ul className={styles.barList}>
      {rows.map((row, index) => (
        <li key={row.key}>
          <div className={styles.barHead}>
            <span className={styles.barLabel}>{row.label}</span>
            <span className={styles.barValue}>
              {row.value}
              <small>{formatShare(row.value, total)}</small>
            </span>
          </div>
          <div className={styles.barTrack}>
            <div
              className={clsx(styles.barFill, 'grow-x')}
              style={{width: `${share(row.value, max)}%`, transitionDelay: `${index * 90}ms`}}>
              {row.segments
                .filter((segment) => segment.value > 0)
                .map((segment) => (
                  <span
                    key={segment.kind}
                    title={`${kindLabel[segment.kind]} ${segment.value}`}
                    style={{flexGrow: segment.value, background: kindColor[segment.kind]}}
                  />
                ))}
            </div>
          </div>
          {row.note && <div className={styles.barNote}>{row.note}</div>}
        </li>
      ))}
    </ul>
  );
}
