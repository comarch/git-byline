# Security policy

## Project status

git-byline is in development and no version has been released yet. The
information below describes how security reports are handled today and how
the policy will look once releases exist.

## Supported versions

There are no released, supported versions yet. Support starts with the
first tagged release; until then, build from source at your own risk and
follow the main branch.

## Reporting a vulnerability

Do not open a public issue for a vulnerability. Instead:

- open a private report through GitHub security advisories on this
  repository (Report a vulnerability), or
- contact the maintainer directly through a private channel.

Include what you found, how you found it, and any proof of concept. Reports
are reviewed and assessed promptly. Concrete response time targets will be
published together with the first supported release.

## Scope

Everything in this repository is in scope, with one standing guarantee:
the git-byline binary never makes network calls. A subcommand that dials
out is a security bug by definition. Attribution data stays in the local
git repository; secrets in prompts or transcripts would therefore be a
design violation, which is why prompts and transcripts are opt-in and off
by default.

## Disclosure

Please allow reasonable time for a fix before public disclosure. Once a fix
is released, we will credit reporters who wish to be credited.
