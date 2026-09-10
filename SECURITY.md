# Security policy

## Supported versions

Security fixes target the latest release and the default branch.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Default branch | Yes |
| Older releases | No |

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability.

Use [GitHub private vulnerability reporting](https://github.com/comarch/git-byline/security/advisories/new).
Include:

- affected release or commit;
- minimal synthetic or redacted reproduction;
- expected security impact;
- suggested mitigation, when known.

Do not include credentials, raw hook payloads, file contents, prompts,
transcripts, private URLs, personal data, or unrelated diagnostics.

## Response targets

These are response targets, not disclosure deadlines.

| Severity | Acknowledge | Triage | Fix or mitigation |
| --- | --- | --- | --- |
| Critical | 2 business days | 5 business days | As soon as practical |
| High | 3 business days | 7 business days | Next patch release |
| Medium | 5 business days | 15 business days | Planned patch |
| Low | 10 business days | 30 business days | Planned maintenance |

## Scope

Source code, workflows, generated instructions, release assets, hook
installation, local checkpoint storage, Git object retention, and attribution
notes are in scope.

The production binary must never make network connections. Telemetry, update
checks, remote lookups, downloads, and cloud synchronization by the binary are
forbidden. The managed `pre-push` hook may invoke Git to publish attribution
notes unless installation used `--local-notes`. Any other network behavior is
a security defect.

Checkpoint metadata and notes must not contain raw hook input, prompts,
transcripts, environment dumps, authorization data, or file content. Snapshot
blobs contain local file content and must stay protected from unintended ref or
artifact publication.

Third-party platforms and agent products remain outside project control unless
the defect is in git-byline integration behavior.

## Disclosure

Please allow time to investigate and release a fix before public disclosure.
Security advisories credit reporters who request credit. Details stay private
until coordinated disclosure is safe.
