# Why git-byline

AI-assisted development creates a provenance gap. A developer can review,
stage, and commit code written partly by a person and partly by one or more
agents. Git records one commit author, but that identity cannot explain how
each line entered the repository.

git-byline closes that gap with a deliberately small product boundary:

- observe supported agent edits through local hooks;
- attach complete line ranges to commits through Git notes;
- show `human`, `ai:<agent>/<model>`, `human-override:<agent>/<model>`, or
  `untracked` for every text line;
- keep source snapshots local and share Git note metadata with normal pushes
  after Git hooks are installed;
- store no prompts, transcripts, raw hook payloads, or environment dumps.

## Business outcomes

### More focused review

Reviewers can identify agent-edited ranges in a mixed commit. Provenance does
not replace review. It gives reviewers better context for deciding where
extra scrutiny, testing, or domain-owner review is useful.

### Durable due diligence

Attribution is attached to exact Git commits and blob IDs, not a temporary
editor session. Versioned JSON output supports local analysis and policy
checks. This helps teams preserve evidence during handovers, incident review,
regulated change processes, and AI adoption experiments.

### Smaller data boundary

The production binary opens no network connection. It needs no account, API
key, service, daemon, or telemetry endpoint. Prompts and transcripts stay
outside the data model. A managed pre-push hook uses Git to publish attribution
notes by default; `--local-notes` disables that sharing.

### Tool choice without one analytics vendor

Native project hooks are generated for Factory, Claude Code, GitHub Copilot,
VS Code Agent, Cursor, Codex, Gemini CLI, Windsurf, and Grok. The attribution
format stays the same across supported agents.

## Market landscape

Different products answer different questions. Product capabilities change,
so verify linked vendor documentation before a procurement decision.

| Approach | Primary question | Granularity | Evidence model | Operational model |
| --- | --- | --- | --- | --- |
| [Standard Git blame](https://git-scm.com/docs/git-blame) | Which commit last changed this line? | Line to commit | Commit graph | Built into Git |
| Commit trailers such as `Co-authored-by` or `Assisted-by` | Was AI involved in this commit? | Whole commit | Declared metadata | Team convention |
| [GitHub Copilot usage metrics](https://docs.github.com/en/copilot/concepts/copilot-usage-metrics/copilot-metrics) and similar dashboards | How is an assistant used across a team? | User, organization, or aggregate events | Platform usage events | Vendor account, dashboard, or API |
| [Git AI](https://github.com/git-ai-project/git-ai) and prompt-linked provenance tools | Which agent, model, and prompt produced code? | Line-level and lifecycle context | Hooks, checkpoints, Git metadata, prompt links | Broader provenance and observability workflow |
| **git-byline** | Which committed lines were observed as human, AI, or unknown? | **Line-level** | **Local hooks, Git blobs, and Git notes** | **One binary, Git note sharing, no account, daemon, telemetry, or prompts** |

### Closest category peer: Git AI

Git AI and git-byline share important ideas: agent checkpoints, line-level
provenance, and Git notes. Their product boundaries differ.

Git AI presents prompt-linked provenance and prompt-to-production
observability. git-byline intentionally excludes prompts, transcripts, cloud
sync, hosted analytics, accounts, daemons, and binary network calls. Choose
the broader model when prompt context and lifecycle analytics are required.
Choose git-byline when local operation, prompt exclusion, and a small trust
boundary matter more.

## Why hook evidence beats detection

A classifier inspects finished code and estimates whether it looks
AI-generated. That estimate can be wrong, cannot reliably identify the exact
agent session, and becomes weaker after human edits or refactoring.

git-byline records the transition when an edit happens:

1. A pre-tool hook snapshots selected paths as human input.
2. The agent changes the files.
3. A post-tool hook snapshots resulting paths with agent and model metadata.
4. The Git hook projects those transitions onto committed blobs.
5. Ambiguous or unsupported history becomes `untracked`.

No content-based classifier is involved.

Exact lines retain attribution. Between two already matched exact anchors,
git-byline also pairs still-unmatched lines when their leading and trailing
whitespace-stripped keys are equal. This second layer handles formatter-only
indentation changes while preserving order. It stops outside those anchors.
There is no tokenization, similarity threshold, or semantic comparison.
Anything beyond whitespace-only equality between matched anchors remains
untracked. An explicit transition fallback applies only to lines introduced by
that transition. The matcher does not guess.

## Honest trade-offs

git-byline stays narrow by design:

- no organization dashboard or productivity score;
- no prompt replay or semantic explanation of generated code;
- no automatic reconstruction of old history;
- no attribution for binary files or unsupported agent event surfaces;
- no automatic fetching of Git notes in another clone;
- no proof that `human` lines were physically typed by a person.

`human` means no supported agent checkpoint claimed the final transition.
`ai` means a supported hook observed an agent edit. `untracked` means evidence
was missing or ambiguous. `human-override` means a human snapshot replaced
lines from the most recent AI transition, so it carries that AI agent, model,
session, and timestamp metadata. These are provenance states, not legal
conclusions or cryptographic identity proofs.

## Good fit

Choose git-byline when:

- source and development context must stay local;
- commits often mix human and agent edits;
- reviewers need exact line ranges, not an adoption percentage;
- several supported agents must produce one attribution format;
- Git-native, scriptable evidence is more useful than a hosted dashboard.

Choose another approach when:

- prompt history is required for replay or evaluation;
- executive dashboards and fleet-wide aggregation are the main outcome;
- historical code must be estimated without edit-time hooks;
- automatic cross-repository note synchronization is required.

## Verify the fit

Install git-byline in a temporary repository, make one agent edit and one
manual edit, commit both, then compare:

```sh
git blame path/to/file
git byline blame path/to/file
git byline blame --json path/to/file
```

Read [installation](INSTALL.md), [compatibility](COMPATIBILITY.md),
[architecture](ARCHITECTURE.md), and the [security model](SECURITY_MODEL.md)
before wider adoption.
