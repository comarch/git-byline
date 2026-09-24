# Rollout on self-hosted GitLab

This runbook takes git-byline from a pilot to a wider rollout on a
self-hosted GitLab instance. It is for the engineer who owns the pilot and
for the maintainers of the pilot projects.

git-byline has no server. The rollout puts one binary and managed Git hooks
on each developer machine, and one CI job with one token in each GitLab
project. Attribution travels with the repository as Git notes in
`refs/notes/byline`.

## What was verified

Steps marked **Verify in pilot** have not run on a real GitLab instance yet.
Each one names a check from the [pilot checklist](#pilot-checklist). The rest
was verified outside GitLab:

- The generated job ran in its pinned runner image against a simulated
  project: a bare repository with `refs/merge-requests/<iid>/head` refs. It
  downloaded the release from GitHub and passed the checksum and version
  checks. The results marked "Simulated" in
  [merge settings](#merge-settings) come from these runs.
- The `oauth2` basic auth header of the job was checked against a local
  HTTP server, not against GitLab.
- The developer commands ran with the binary in temporary repositories.

Use a git-byline release newer than v1.4.2, on developer machines and in the
job. The GitLab job of v1.4.2 fails outside this repository
([#50](https://github.com/comarch/git-byline/issues/50)), sends the token in
a header that GitLab accepts only for REST calls
([#51](https://github.com/comarch/git-byline/issues/51)), and leaves squash
merges `untracked` ([#52](https://github.com/comarch/git-byline/issues/52)).
v1.4.2 also writes guessed notes after fast-forward pulls
([#53](https://github.com/comarch/git-byline/issues/53)).

## What the rollout changes

| Where | What | Who |
| --- | --- | --- |
| Developer machine | The `git-byline` binary, installed for the current user | Developer |
| Each clone | Managed blocks in the `post-checkout`, `post-commit`, `post-merge`, `post-rewrite`, `pre-push`, and `reference-transaction` hooks | Developer |
| Agent configuration | One hook per coding agent | Developer |
| Repository | `.gitlab/ci/git-byline.yml` and its `include` entry in `.gitlab-ci.yml` | Maintainer |
| GitLab project | A project access token in the `GITLAB_TOKEN` CI/CD variable | Maintainer |
| GitLab remote | `refs/notes/byline`, pushed by developers and by the job | Automatic |

No daemon, account, or telemetry is added. Checkpoints, snapshots, and state
stay in each clone. Only `refs/notes/byline` reaches GitLab.

## Decide before the pilot

### Note sharing

Everyone who can read a repository can read its notes. Notes hold repository
paths, line ranges, agent and model names, human identity tokens, session
identifiers, timestamps, and blob IDs. They hold no prompts, transcripts, or
file content. Review this with your security or privacy contact before the
pilot. The [security model](SECURITY_MODEL.md) has the details.

A developer can install hooks with `--local-notes` to keep notes local. Their
merge requests then reach GitLab without notes, so everyone else sees their
lines as `untracked`.

### Merge settings

The job reconstructs attribution for the commit GitLab writes to the default
branch. The result depends on the merge method and the squash setting under
Settings > Merge requests:

| Merge method | Squash | Result on the default branch | Evidence |
| --- | --- | --- | --- |
| Merge commit | Off | Kept. The job writes the merge commit note from the merge request commits | Simulated |
| Merge commit | On | Kept when the merge commit message names the merge request, also after the default branch moved | Simulated |
| Merge commit with semi-linear history | Off or on | Expected as for merge commit | **Verify in pilot**, P5 |
| Fast-forward merge | Off | Kept. The commits keep the notes their authors pushed, and the job writes nothing | Simulated |
| Fast-forward merge | On | Kept only when the squash commit template holds `%{reference}`, otherwise `untracked` | Simulated |

For a squash merge, the job reads the `group/project!123` reference in the
merged commit message and fetches that merge request head. It uses the head
only when its diff equals the diff the merged commit brings. The default
merge commit template ends with `See merge request %{reference}`. The default
squash commit template holds only `%{title}`, so with the fast-forward method
add `%{reference}` to it (**Verify in pilot**, P4). A message that names
another merge request is refused, and its lines stay `untracked`.

Two rules hold for every method:

- Rebase merge requests locally, with `git rebase` or `git pull --rebase`.
  The Rebase button and the `/rebase` quick action rebase on the server,
  where no hook carries the notes to the new commits.
- Without the job, GitLab merge commits and squash merges show their lines
  as `untracked`. Only fast-forward merges without squash keep attribution
  on their own.

### Runner and network

The job expects a Linux runner, amd64 or arm64, with a Docker-compatible
executor. It pulls `buildpack-deps:trixie-scm` from Docker Hub by digest,
and it downloads the release archive and `checksums.txt` from GitHub over
HTTPS. It clones the full history (`GIT_DEPTH: "0"`) and has a 15 minute
timeout (**Verify in pilot**, P8). For a closed network, mirror both
downloads as described in
[mirrors for closed networks](#mirrors-for-closed-networks).

The job sends the token in an HTTP basic auth header with every Git request
to `CI_PROJECT_URL`, or to `GIT_BYLINE_REMOTE_URL` when it is set, so that
address must use HTTPS. The job also sets
`GIT_CONFIG_NOSYSTEM`, `GIT_CONFIG_GLOBAL`, and `GIT_CONFIG_SYSTEM` for the
whole job, so system and global Git configuration do not apply. If the
instance or a mirror uses a certificate from an internal certificate
authority, check that the runner still clones the project and that the job
still trusts the certificate (**Verify in pilot**, P6).

### Pilot scope

- One or two projects. Cover each merge method your teams use.
- Three to five developers who already use a supported coding agent.
- At least two weeks of normal work, so the notes routine meets real team
  traffic.
- One pilot owner who reads job logs, runs the checks, and collects notes
  conflicts.

## Set up the project

A maintainer runs these steps once per project, in a clone with git-byline
newer than v1.4.2 installed.

1. Generate the job file. `install-hooks` detects only github.com and
   gitlab.com remotes, so on a self-hosted instance it writes no job file.

   ```sh
   git byline ci install --provider gitlab
   ```

   The command prints `created .gitlab/ci/git-byline.yml`. It never
   replaces a different existing file.

2. Include the job from `.gitlab-ci.yml`, next to any existing entries:

   ```yaml
   include:
     - local: .gitlab/ci/git-byline.yml
   ```

   The job runs in the `.post` stage of push pipelines on the default
   branch. Merge request and fork pipelines never run it. GitLab does not
   create a pipeline that holds only `.pre` and `.post` jobs, so the
   default-branch push pipeline needs at least one other job, and
   `workflow:rules` must allow that pipeline (**Verify in pilot**, P1).

3. Create a project access token under Settings > Access tokens, with the
   Developer role, only the `write_repository` scope, and an expiration
   date. Record the date. When the token expires, the job fails until you
   rotate it (**Verify in pilot**, P2).

4. Add the token under Settings > CI/CD > Variables as `GITLAB_TOKEN`,
   protected and masked. A protected variable reaches only protected
   branches, so the default branch must be protected. Do not
   enable `CI_DEBUG_TRACE` for this job. GitLab masks the raw token, not the
   encoded header the job builds from it.

5. Commit both files in a merge request. Do not edit
   `.gitlab/ci/git-byline.yml` by hand. `ci install` and `uninstall` treat
   the file as managed only while it matches the template, apart from the
   release tag on its `GIT_BYLINE_VERSION` line.

6. Merge one test merge request and [read the job log](#read-the-job-log).
   Check that push rules accept the notes commits (**Verify in pilot**, P3).

### Mirrors for closed networks

Project or group CI/CD variables override the download settings of the job
without editing the file (**Verify in pilot**, P6):

| Variable | Default | Use |
| --- | --- | --- |
| `GIT_BYLINE_VERSION` | The release that generated the file | Pin another release tag newer than v1.4.2, like `v1.4.3` |
| `GIT_BYLINE_RELEASES_URL` | `https://github.com/comarch/git-byline/releases/download` | An internal mirror of the release assets |

The job downloads `$GIT_BYLINE_RELEASES_URL/<tag>/<archive>` and
`$GIT_BYLINE_RELEASES_URL/<tag>/checksums.txt`, the same layout as the
GitHub release. The archive name is
`git-byline_<version>_linux_<arch>.tar.gz`. Mirror the files unmodified. The
job verifies the archive against that `checksums.txt`, so a mirror moves
trust for both files to the mirror.

For the image, prefer a Docker Hub mirror on the runner, so the job file
stays unchanged. Otherwise override the image in `.gitlab-ci.yml`. GitLab
merges a job defined there over the included job with the same name. Copy
the `image` line from the generated file and change only the registry, so
the digest pin stays:

```yaml
include:
  - local: .gitlab/ci/git-byline.yml

git-byline-reconstruct:
  image: registry.example.invalid/library/buildpack-deps:trixie-scm@sha256:<digest from the generated file>
```

Update the override when a release changes the digest. With the shell
executor GitLab ignores `image`, and the runner host needs Git, curl, tar,
awk, and GNU coreutils. The job has only run in its image.

If runners reach GitLab at another address than `CI_PROJECT_URL`, for
example through the runner `clone_url` setting, set `GIT_BYLINE_REMOTE_URL`
as a project variable to that HTTPS address (**Verify in pilot**, P6). The
job sends the token header to this address, so never set an `http://` URL.
The job does not check the scheme itself.

## Set up developer machines

1. Install the binary with one of the
   [installation paths](INSTALL.md#pick-your-path). The `/git-byline-setup`
   command of the agent packages also installs the Git hooks in the current
   repository and the agent hook. Without GitHub access, use the
   [checksum-first manual install](INSTALL.md#checksum-first-manual-install)
   with files from your mirror, and
   [update the same way](INSTALL.md#air-gapped-update).

2. Install the Git hooks in every pilot clone:

   ```sh
   git byline install-hooks --agent none --git --project
   ```

   The output ends with `no GitHub or GitLab remote detected`. On a
   self-hosted instance this line is expected, because the job file comes
   from the repository. Factory and Claude Code users who skipped the
   package add their agent hook once with
   `git byline install-hooks --agent droid --git --user`, or
   `--agent claude`. Other agents use the
   [agent templates](../marketplace/harness/README.md).

3. [Sync notes](#sync-notes) once, to fetch the notes others already pushed.

4. Run `git byline status` after the next commit. `Last annotated commit`
   equals `HEAD`, and `Annotation pending` is `false`. When the parent of
   the first annotated commit in a clone has no note yet, for example at the
   start of the pilot, the commit prints
   `warning: initializing attribution on a repository with existing history`.
   That is expected.

For clones made later, `git byline install-hooks --agent none --git
--template` adds the same hooks to every `git clone` and `git init`. See
[Git template](INSTALL.md#git-template).

### Hook managers

git-byline writes managed blocks into the hooks directory Git uses. A
`core.hooksPath` inside the repository works. A hooks path outside the
repository, such as a global hooks directory for secret scanning, is
refused:

```text
git-byline install-hooks: refuse hooks directory outside repository: ...
```

A tool that regenerates hook files can drop the managed blocks. Run
`install-hooks` again after it, and check `git byline status` after the next
commit (**Verify in pilot**, P9).

## Daily routine

Developers commit and push as usual. The `pre-push` hook sends
`refs/notes/byline` before each branch push, without force. When another
clone or the job pushed notes since the last sync, the push stops before the
branch goes out:

```text
 ! [rejected]        refs/notes/byline -> refs/notes/byline (fetch first)
```

The reason can also read `(non-fast-forward)`. [Sync notes](#sync-notes),
then push again. Every reconstructed merge and every notes push of a
teammate moves the remote notes, so expect this often in an active project.
The pilot measures how often it happens and how long recovery takes.

After a `git pull` that fast-forwards, git-byline prints:

```text
warning: skipped annotate: HEAD fast-forwarded to existing commits; fetch refs/notes/byline for their attribution
```

Sync notes before the next commit. Otherwise lines the next commit inherits
from the pulled commits stay `untracked` in the files it changes.

Commit agent edits before a pull instead of stashing them. A fast-forward
pull that changes a stashed path drops the agent attribution of that path,
and `--autostash` counts as a stash
([#55](https://github.com/comarch/git-byline/issues/55)).

### Sync notes

Run all three lines:

```sh
git fetch origin refs/notes/byline:refs/notes/byline-remote
git notes --ref=refs/notes/byline merge refs/notes/byline-remote
git update-ref -d refs/notes/byline-remote
```

The merge fast-forwards when only the remote moved, and it writes a notes
merge commit when both sides moved. Until the first notes push reaches the
project, the fetch fails with `couldn't find remote ref refs/notes/byline`.

The merge never picks between two different notes for one commit. It stops
with:

```text
CONFLICT (content): Merge conflict in notes for object <commit>
```

Then run `git notes --ref=refs/notes/byline merge --abort` and the third
line. The local notes stay as they were. Send the commit ID to the pilot
owner, who decides which note is right. The developer then runs the three
lines again, with `-s theirs` added to the merge to keep the remote note, or
`-s ours` to keep the local one. Until then the developer cannot push.
Conflicts are not expected in normal work, so
[report each one](https://github.com/comarch/git-byline/issues/new/choose).

## Run the pilot

The pilot owner checks this every few days:

- Each default-branch pipeline after a merge ran `git-byline-reconstruct`,
  and the job passed.
- Failed jobs are retried. The job runs only after the earlier stages
  succeed, so a failed test job on the default branch skips it too. A later
  pipeline never reconstructs an earlier merge.
- Every default-branch commit since the pilot started has a note. Sync
  notes first, set `start` to the default-branch commit where the pilot
  began, and replace `main` with your default branch:

  ```sh
  git byline check "$start..origin/main" --require-note
  git byline stats "$start..origin/main"
  git byline verify "$start..origin/main"
  ```

  `check` fails on each commit without a note and prints its ID. `stats`
  shows human, AI, and untracked lines. `verify` checks the notes against
  the commits. All three follow first-parent history, so they count the
  commits GitLab wrote to the default branch.
- Notes conflicts and notes push rejections that developers report.

### Read the job log

| Line in the job log | Meaning |
| --- | --- |
| `reconstructed squash attribution: 2 mapped, 1 notes written` | The merge result got its note from 2 merge request commits. The job reports squash mode for merge commits too |
| `[new ref] refs/merge-requests/7/head -> origin/git-byline-mr-head` | The job fetched the merge request head named in the message |
| `git-byline: merge request !7 does not match ...; using commit parents` | The named merge request brings a different diff, so squashed lines stay `untracked` |
| `warning: inconsistent CI merge ranges; no attribution notes written` | Expected after a fast-forward merge without squash. After a squash merge, the message named no merge request |
| `git-byline: set GITLAB_TOKEN to a token with the write_repository scope` | The variable is missing, or it is protected and the branch is not |
| `git-byline: checksum mismatch for ...` | The download does not match the release `checksums.txt` |
| `! [rejected] refs/notes/byline -> refs/notes/byline` | A developer pushed notes while the job ran. Retry the job. It fetches the latest notes first |

## Pilot checklist

Record a result for each check before a wider rollout.

| ID | Check | Pass when |
| --- | --- | --- |
| P1 | Pipeline | Merging a merge request starts a default-branch push pipeline, and `git-byline-reconstruct` passes in its `.post` stage |
| P2 | Token | The job fetches and pushes `refs/notes/byline` with no authentication error |
| P3 | Notes pushes | Developer pushes and the job push update `refs/notes/byline`. Push rules under Settings > Repository that reject unsigned commits or unknown authors can refuse notes commits. The job commits as `git-byline-ci@users.noreply.gitlab.com` |
| P4 | Squash merges | The log shows the merge request head fetch and `1 notes written`, and the commit templates hold `%{reference}` |
| P5 | Semi-linear history | A test merge with squash on and off gets a note, as in P4 |
| P6 | Network | The runner clones the project, pulls the image or its override, and downloads the release or its mirror copy. Git and curl trust the instance certificate. Variables for the mirror and the remote address take effect |
| P7 | Pipeline cancellation | Two merges in quick succession both run the job. Check Auto-cancel redundant pipelines under Settings > CI/CD > General pipelines, `[skip ci]` in merge messages, and merge trains, which were not simulated |
| P8 | Duration | The full-history clone and the job finish well inside the 15 minute timeout |
| P9 | Hook managers | After the hook manager of the project runs, `git byline status` still shows `HEAD` annotated after a commit |
| P10 | Notes removal | Rollback only: `git push origin --delete refs/notes/byline` removes the notes ref from GitLab |

## Roll out further

When the pilot checklist passes:

- Set `GIT_BYLINE_RELEASES_URL` once as a group CI/CD variable, if you use a
  mirror. Projects inherit it. Leave `GIT_BYLINE_VERSION` to the job file,
  so the job script and the binary come from the same release.
- Give each project its own project access token. One group access token
  would let a single leaked secret write to every project in the group.
- Add the developer setup and the notes sync to your onboarding.
- Schedule `git byline update` on developer machines, see
  [automatic updates](INSTALL.md#automatic-updates).

### Upgrade the job

A change of only the release tag on the `GIT_BYLINE_VERSION` line keeps the
file managed. When a release changes more of the template, `ci install`
prints `refusing to replace existing CI workflow`. Generate the file again
with the new release and review the diff in a merge request:

```sh
git rm .gitlab/ci/git-byline.yml
git byline ci install --provider gitlab
git add .gitlab/ci/git-byline.yml
```

## Roll back

Pick the level you need. None of these steps rewrite branches.

### One developer

Run this in each clone:

```sh
git byline uninstall --agent none --git
```

Use `--agent droid --user` or `--agent claude --user` to remove the agent
hook too. `uninstall --git` also deletes `.gitlab/ci/git-byline.yml` from the
worktree while the file matches the template. Restore it with
`git restore .gitlab/ci/git-byline.yml`, and do not commit the deletion.

### One project

In one merge request, delete `.gitlab/ci/git-byline.yml` and remove its
`include` entry from `.gitlab-ci.yml`. Deleting only the file leaves an
`include` that GitLab cannot resolve, and the pipeline configuration becomes
invalid. Then revoke
the project access token and delete the `GITLAB_TOKEN` variable.

From then on, GitLab merge commits and squash merges show their lines as
`untracked`. Fast-forward merges without squash keep the notes developers
push.

### Notes on GitLab

Uninstalling stops new attribution and deletes no notes. Existing notes stay
readable in every clone that fetched them. To remove them from GitLab too:

```sh
git push origin --delete refs/notes/byline
```

Any clone that still has the ref can push it back, and clones with managed
hooks do so on their next push. Uninstall the hooks first
(**Verify in pilot**, P10).

## Known limits

- Commits made on GitLab itself pass no local hook: the web editor, the Web
  IDE, applied review suggestions, conflict resolution in the merge request,
  and the server-side rebase. Their lines are `untracked`.
- In merge requests from forks, the author's hooks push notes to the fork,
  so the merged lines are `untracked` in the project.
- A merge whose pipeline was skipped or cancelled, or whose failed job was
  not retried, has no reconstructed note.
- Developers with `--local-notes` publish no notes.
- The full behavior matrix is in [compatibility](COMPATIBILITY.md) and in
  the [README limitations](../README.md#limitations).
