package app

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/provenance"
)

// notesConflictHelp keeps the protections of the hook: the fetch reads the
// push URL and checks objects like the hook does, the empty --refmap stops a
// configured notes fetch refspec from overwriting local notes, and the
// manual strategy stops notes.mergeStrategy from picking a side. It still
// cannot stop what the hook refused, so the compare step comes first.
const notesConflictHelp = `git-byline %[1]s: %[2]v; nothing was changed
Fetch the remote notes and compare both versions of the note first:
  git -c fetch.fsckObjects=true fetch --no-tags --refmap= %[3]s +refs/notes/byline:refs/notes/byline-remote
  git notes --ref=refs/notes/byline show <commit>
  git notes --ref=refs/notes/byline-remote show <commit>
The merge below takes a remote change or removal without a conflict when
local notes left that note alone, and it fast-forwards when local notes are
behind. Run it only when the remote version is right, then push again:
  git notes --ref=refs/notes/byline merge --strategy=manual refs/notes/byline-remote
  (on a conflict, fix the files Git names, then run
   git notes --ref=refs/notes/byline merge --commit, or --abort to stop)
  git update-ref -d refs/notes/byline-remote
`

func runMergeNotes(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	remote := flags.String("remote", "", "remote name shown in the manual merge steps")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, fmt.Errorf("merge-notes takes no arguments, got %q", flags.Arg(0)))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := provenance.MergeRemoteNotes(repo)
	writeWarnings(env, result.Warnings)
	if errors.Is(err, gitcmd.ErrNotesConflict) {
		fmt.Fprintf(env.Stderr, notesConflictHelp, command.name, err, pushURLSource(*remote))
		return ExitFailure, fmt.Errorf("git-byline %s: %w", command.name, err)
	}
	if err != nil {
		return operationalError(env, command.name, err)
	}
	// The pre-push hook is the usual caller, so the name marks the line in
	// the push output.
	if result.FastForwarded {
		fmt.Fprintf(env.Stdout, "git-byline: updated attribution notes from the remote, %d added\n", result.Added)
	}
	if result.Merged {
		fmt.Fprintf(env.Stdout, "git-byline: merged remote attribution notes, %d added\n", result.Added)
	}
	return ExitSuccess, nil
}

// pushURLSource returns what the manual fetch reads. The hook compared
// against the push URL, so the steps read it too; with a separate pushurl
// the remote name would fetch the notes of another repository. The hook
// passes the push remote as given, which can be a URL with credentials, so
// anything beyond a plain remote name prints as a placeholder.
func pushURLSource(name string) string {
	for index, char := range name {
		plain := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
		if !plain && (index == 0 || !strings.ContainsRune("._-/", char)) {
			return "<push URL>"
		}
	}
	if name == "" {
		return "<push URL>"
	}
	return `"$(git remote get-url --push ` + name + `)"`
}
