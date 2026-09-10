# git-byline plugin

Know which committed lines came from humans and AI without sending code,
prompts, or transcripts to a separate analytics service. Attribution notes
follow ordinary pushes to the selected Git remote by default.

This Factory plugin installs and configures git-byline without a manual binary
installation step.

## Quick start

```text
droid plugin marketplace add comarch/git-byline
droid plugin install git-byline@git-byline --scope user
```

Start Droid in a Git repository and run `/git-byline-setup`. Then work
normally and inspect a committed file:

```sh
git byline blame path/to/file
git byline dashboard
git byline status
```

## Capabilities

- `/git-byline-setup` selects the operating-system installer.
- Release archives are verified against `checksums.txt`.
- Binary version is checked before installation.
- Local agent and Git hooks are activated without replacing unrelated config.
- The `git-byline` skill documents status, blame, and local dashboard
  workflows.

The plugin setup installs a local runtime because Git hooks must keep working
outside an active Factory session. No daemon, account, telemetry, or cloud
storage is added. Git hook installation publishes attribution notes on
ordinary pushes by default. Use `--local-notes` to disable automatic note
sharing. Users can still push the notes ref explicitly.

See the [product overview](https://github.com/comarch/git-byline),
[installation guide](https://github.com/comarch/git-byline/blob/main/docs/INSTALL.md),
and [security model](https://github.com/comarch/git-byline/blob/main/docs/SECURITY_MODEL.md).
