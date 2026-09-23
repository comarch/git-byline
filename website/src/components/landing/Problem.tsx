import type {CSSProperties, ReactNode} from 'react';
import Icon, {type IconName} from './Icon';
import Reveal from './Reveal';
import {Section, SectionHeader} from './Section';
import {kindColor, kindLabel, type Kind} from './data';
import styles from './Problem.module.css';

type Answer = boolean | ReactNode;

const questions: {question: string; blame: Answer; byline: Answer}[] = [
  {question: 'Which commit touched this line?', blame: true, byline: true},
  {question: 'Was it written by a person or an agent?', blame: false, byline: 'yes, per line'},
  {
    question: 'Which person wrote it?',
    blame: 'commit author, whole commit',
    byline: (
      <>
        per line, <code>human:john.doe</code>
      </>
    ),
  },
  {question: 'Which agent and model produced it?', blame: false, byline: 'yes, with session and timestamp'},
  {
    question: 'Did a person rewrite agent output?',
    blame: false,
    byline: (
      <>
        <code>human-override</code> keeps the AI origin
      </>
    ),
  },
  {
    question: 'What is genuinely unknown?',
    blame: 'nothing is marked unknown',
    byline: (
      <>
        <code>untracked</code>, never guessed
      </>
    ),
  },
];

// Labels are real lines from the demo repository.
const states: {kind: Kind; icon: IconName; meaning: string; carries: string; example: string}[] = [
  {
    kind: 'human',
    icon: 'lines',
    meaning: 'No supported agent checkpoint claimed the final transition.',
    carries: 'line range, person identity',
    example: 'human:john.doe',
  },
  {
    kind: 'ai',
    icon: 'sparkle',
    meaning: 'A supported hook observed an agent edit.',
    carries: 'agent, model, session, timestamps',
    example: 'ai:droid/claude-sonnet-4-5',
  },
  {
    kind: 'human_override',
    icon: 'userEdit',
    meaning: 'A person replaced AI output. The AI origin stays visible.',
    carries: 'person, agent, model, session, timestamps',
    example: 'human-override:john.doe/droid/claude-sonnet-4-5',
  },
  {
    kind: 'untracked',
    icon: 'question',
    meaning: 'Evidence is missing or ambiguous, so nothing is guessed.',
    carries: 'line range',
    example: 'untracked',
  },
];

function AnswerCell({value}: Readonly<{value: Answer}>) {
  if (value === true) {
    return (
      <span className={styles.yes}>
        <Icon name="check" />
        <span className="sr-only">yes</span>
      </span>
    );
  }
  if (value === false) {
    return (
      <span className={styles.no}>
        <Icon name="cross" />
        <span className="sr-only">no</span>
      </span>
    );
  }
  return <>{value}</>;
}

export default function Problem() {
  return (
    <Section id="features">
      <SectionHeader eyebrow="The missing layer" title="Git knows who committed a line. Not who wrote it.">
        One commit can mix lines a person typed, lines an agent generated, and lines nobody can vouch for.
        git-byline records the difference when the edit happens.
      </SectionHeader>
      <div className={styles.layout}>
        <Reveal className={styles.card}>
          <table className={styles.table}>
            <caption className="sr-only">What git blame and git-byline can answer</caption>
            <thead>
              <tr>
                <th scope="col">Question</th>
                <th scope="col">
                  <code>git blame</code>
                </th>
                <th scope="col">git-byline</th>
              </tr>
            </thead>
            <tbody>
              {questions.map((row) => (
                <tr key={row.question}>
                  <th scope="row">{row.question}</th>
                  <td className={styles.blame}>
                    <AnswerCell value={row.blame} />
                  </td>
                  <td className={styles.byline}>
                    <AnswerCell value={row.byline} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Reveal>
        <div className={styles.states}>
          {states.map((state, index) => (
            <Reveal
              key={state.kind}
              className={styles.state}
              delay={index * 90}
              style={{'--tone': kindColor[state.kind]} as CSSProperties}>
              <div className={styles.stateHead}>
                <Icon name={state.icon} className={styles.stateIcon} />
                <code className={styles.stateName}>{kindLabel[state.kind]}</code>
              </div>
              <p className={styles.meaning}>{state.meaning}</p>
              <p className={styles.carries}>Carries {state.carries}</p>
              <code className={styles.example}>{state.example}</code>
            </Reveal>
          ))}
        </div>
      </div>
    </Section>
  );
}
