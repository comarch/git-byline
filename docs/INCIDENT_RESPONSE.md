# Incident response

## Scope

Use this runbook for project code, malicious notes, hook mutation, dependency
compromise, workflow compromise, credential exposure, or a bad release.

## Triage

1. Move sensitive discussion to GitHub private vulnerability reporting.
2. Record time, affected versions, reporter, current evidence, and owner.
3. Classify impact on source, local data, workflows, credentials, or releases.
4. Do not copy credentials, private repository content, prompts, transcripts,
   or personal data into the incident record.

## Containment

- Disable affected workflow and auto-merge paths.
- Pause release publication.
- Revoke exposed credentials.
- Quarantine the affected action, dependency, tag, or archive.
- Preserve redacted logs and hashes.
- Mark a bad release as withdrawn when practical.

## Remediation

- Reproduce with a minimal synthetic fixture.
- Identify root cause and affected versions.
- Add a regression test or detection rule.
- Fix source and configuration.
- Run the complete validation contract.
- Rebuild and inspect release artifacts.
- Publish a corrective release.

## Recovery

- Restore automation only after required checks pass.
- Review branch rules, environments, access, and bypass actors.
- Rotate credentials again if scope remains uncertain.
- Update the security model and this runbook.
- Notify affected users with direct upgrade or mitigation steps.

## Bad release

Do not delete a tag that users may already depend on. Mark the release as
withdrawn, explain impact, and publish a patch release. Delete an unpublished
draft only when no user could have consumed it.

## Ownership

Primary maintainer: `@mrwogu`.

Before public release, repository access must include a second recovery owner
for security advisories, release environment settings, and credential
revocation. That owner is configured in GitHub, not embedded in source without
their consent.
