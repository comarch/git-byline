# git-byline plugin

Factory plugin for installing and using git-byline without a manual binary
installation step.

## Capabilities

- `/git-byline-setup` selects the operating-system installer.
- Release archives are verified against `checksums.txt`.
- Binary version is checked before installation.
- The `git-byline` skill documents status and blame workflows.

The plugin setup installs a local runtime because Git hooks must keep working
outside an active Factory session. No daemon, account, telemetry, or cloud
storage is added.
