# Interoperability

git-byline can exchange committed attribution with two public formats:

- Git AI Standard v3.0.0, stored in `refs/notes/ai`.
- Agent Trace 0.1.0, emitted as a JSON record.

All conversion is local. The binary does not fetch conversations, prompts,
models, or other remote resources.

## Git AI Standard v3

The implementation targets [Git AI Standard
v3.0.0](https://github.com/git-ai-project/git-ai/blob/main/specs/git_ai_standard_v3.0.0.md).
The note contains the standard attestation section followed by the JSON
metadata section. Export writes `authorship/3.0.0` and the selected commit
SHA as `base_commit_sha`.

The mapping is:

| Git AI record | git-byline attribution |
| --- | --- |
| `s_<14hex>::t_<14hex>` with a `sessions` record | `ai`, with `agent` from `agent_id.tool`, plus `model` and the `s_` session ID |
| `h_<14hex>` with a `humans` record | `human` |
| Bare 16-hex or 7-hex key with a `prompts` record | `ai`, with agent and model from the prompt record and the bare key as session |
| `human-override` range | `human`, using the deterministic synthetic human identity |
| No attestation for a committed line | `untracked` |

Import resolves each path against the committed blob. It rejects missing,
invalid, overlapping, or out-of-bounds ranges. Lines not present in the Git AI
attestation are added as `untracked`, so the resulting byline note keeps full
ordered coverage.

Git AI v3.0.0 implementations SHOULD accept 7-character legacy hash keys for
backward compatibility with versions before v1.0. git-byline accepts both
7-hex and 16-hex legacy keys.

When exporting, git-byline always derives a session ID as:

```text
s_ + first 14 hex characters of SHA-256(tool + ":" + conversation_id)
```

An input conversation ID already having the `s_` form is hashed again. Git AI
trace IDs use `t_` plus the first 14 hex characters of
`SHA-256("git-byline:checkpoint:" + checkpoint_sequence)`. This replaces the
Git AI specification's random trace ID so identical local state produces
identical output. Trace IDs are emitted only when the AI range matches a real
checkpoint sequence. An unmatched AI range is omitted from the Git AI
attestation and therefore imports as `untracked`. Human records use a
deterministic `git-byline` author identity because the byline note stores no
committer identity per line.

Git AI export quotes paths containing spaces, tabs, or newlines. It also quotes
paths beginning with a double quote so export and import preserve the path.

## Agent Trace 0.1

The writer targets [Agent Trace
0.1.0](https://agent-trace.dev/). It records:

- `vcs.type` as `git`;
- `vcs.revision` as the exported commit SHA;
- line ranges grouped into conversations by attribution;
- `ai`, `human`, and `unknown` contributors for AI, human, and untracked
  byline ranges;
- `human-override` ranges are exported as `human` because committed content is
  human-authored;
- deterministic UUID and commit timestamp values.

Agent Trace has no required storage location. The export keeps local agent and
session values in the optional `comarch.git-byline` metadata member. It never
stores prompts, transcripts, raw hook payloads, or file contents in the trace.

## Commands

Export `HEAD` to stdout:

```sh
git byline export --format gitai
git byline export --format agent-trace
```

Select a revision:

```sh
git byline export --format gitai --commit <rev>
```

Write a private, new file. Existing files are never replaced:

```sh
git byline export --format agent-trace --output trace.json
```

Import the Git AI note on `HEAD`:

```sh
git byline import --format gitai
```

Import a first-parent revision range without writing:

```sh
git byline import --format gitai --range <rev-range> --dry-run
```

Import writes only to `refs/notes/byline`. A different existing byline note is
never overwritten. The commit is skipped and a warning is written to stderr.
An identical existing note is left unchanged without a warning. Malformed or
incompatible Git AI notes are skipped with a warning.
