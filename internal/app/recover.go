package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
)

type recoverCommandResult struct {
	Version    int                        `json:"version"`
	DryRun     bool                       `json:"dry_run"`
	Preview    provenance.RecoveryReport  `json:"preview"`
	Annotation *provenance.AnnotateResult `json:"annotation,omitempty"`
}

func runRecover(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	drop := flags.Bool("drop", false, "discard unreachable checkpoints and retry annotation")
	all := flags.Bool("all", false, "with --drop, also discard parked checkpoints a branch still reaches")
	jsonOutput := flags.Bool("json", false, "print one machine-readable JSON object")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("recover takes no positional arguments"))
	}
	if *all && !*drop {
		return commandUsageError(env, command, errors.New("--all needs --drop"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	preview, err := provenance.PreviewRecovery(repo)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	commandResult := recoverCommandResult{
		Version: model.StateVersion,
		DryRun:  !*drop,
		Preview: preview,
	}
	if !*drop {
		return writeRecoverPreviewResult(env, command, commandResult, *jsonOutput)
	}
	return runRecoverDrop(env, command, repo, commandResult, *all, *jsonOutput)
}

func writeRecoverPreviewResult(
	env *Env,
	command *command,
	result recoverCommandResult,
	jsonOutput bool,
) (int, error) {
	if jsonOutput {
		if err := writeJSON(env, result); err != nil {
			return operationalError(env, command.name, err)
		}
	} else {
		writeRecoveryPreview(env.Stdout, result.Preview)
	}
	return ExitSuccess, nil
}

func runRecoverDrop(
	env *Env,
	command *command,
	repo *gitcmd.Repo,
	result recoverCommandResult,
	all bool,
	jsonOutput bool,
) (int, error) {
	preview := result.Preview
	drop := provenance.AnnotateDroppingParked
	if !all {
		if preview.BlockedCheckpoints > 0 {
			return operationalError(env, command.name, blockedRecoveryError(preview))
		}
		drop = provenance.AnnotateDroppingStranded
	}
	annotation, err := drop(repo)
	if err != nil {
		return annotateOperationalError(env, command.name, repo, err)
	}
	result.Annotation = &annotation
	if jsonOutput {
		if err := writeJSON(env, result); err != nil {
			return operationalError(env, command.name, err)
		}
		return ExitSuccess, nil
	}
	writeWarnings(env, annotation.Warnings)
	writeAnnotateResult(env.Stdout, annotation)
	return ExitSuccess, nil
}

func writeRecoveryPreview(out io.Writer, report provenance.RecoveryReport) {
	fmt.Fprintf(out, "Annotation pending: %t\n", report.AnnotationPending)
	fmt.Fprintf(out, "Unrelated checkpoints: %d\n", report.UnrelatedCheckpoints)
	fmt.Fprintf(out, "Stranded checkpoints: %d\n", report.StrandedCheckpoints)
	fmt.Fprintf(out, "Blocked checkpoints: %d\n", report.BlockedCheckpoints)
	for _, checkpoint := range report.Checkpoints {
		base := checkpoint.BaseCommit
		if base == "" {
			base = "(before first commit)"
		}
		branch := checkpoint.BranchRef
		if branch == "" {
			branch = "(legacy or detached)"
		}
		fmt.Fprintf(out, "Checkpoint %d: branch %s, base %s, object present %t, droppable %t\n",
			checkpoint.Seq, branch, base, checkpoint.ObjectPresent, checkpoint.Droppable)
		for _, branch := range checkpoint.Branches {
			fmt.Fprintf(out, "  Branch: %s\n", branch)
		}
	}
	if report.RecommendedAction != "" {
		fmt.Fprintf(out, "Recommended action: %s\n", report.RecommendedAction)
	}
	fmt.Fprintln(out, "No checkpoints changed.")
}

func blockedRecoveryError(report provenance.RecoveryReport) error {
	var details []string
	for _, checkpoint := range report.Checkpoints {
		if checkpoint.Droppable {
			continue
		}
		branch := checkpoint.BranchRef
		if branch == "" {
			branch = "(legacy or detached)"
		}
		details = append(details, fmt.Sprintf(
			"checkpoint %d branch %s base %s is reachable from %s",
			checkpoint.Seq,
			branch,
			checkpoint.BaseCommit,
			strings.Join(checkpoint.Branches, ", "),
		))
	}
	return fmt.Errorf(
		"refusing to drop %d blocked checkpoints\n%s\n"+
			"restore each checkpoint's recorded branch and base to consume it, "+
			"delete every listed branch and run: git-byline recover, "+
			"or discard all parked checkpoints with: git-byline recover --drop --all",
		report.BlockedCheckpoints,
		strings.Join(details, "\n"),
	)
}
