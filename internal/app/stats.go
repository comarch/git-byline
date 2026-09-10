package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/comarch/git-byline/internal/report"
)

func runStats(env *Env, command *command, args []string) (int, error) {
	jsonOutput, rest, err := parseJSONFlag(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, command.usage)
			return ExitSuccess, nil
		}
		return commandUsageError(env, command, err)
	}
	from, to, err := parseRevisionRange(rest, command.name)
	if err != nil {
		return commandUsageError(env, command, err)
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := report.Collect(repo, from, to, 0)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if jsonOutput {
		if err := writeJSON(env, result); err != nil {
			return operationalError(env, command.name, err)
		}
		return ExitSuccess, nil
	}
	writeWarnings(env, result.Warnings)
	writeStatsText(env.Stdout, result)
	return ExitSuccess, nil
}

func parseRevisionRange(args []string, commandName string) (string, string, error) {
	if len(args) == 0 {
		return "", "", nil
	}
	if len(args) > 1 {
		return "", "", fmt.Errorf("%s accepts at most one revision range", commandName)
	}
	value := args[0]
	if value == "" {
		return "", "", errors.New("revision range must not be empty")
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return "", "", errors.New("revision range contains whitespace or a control character")
		}
	}
	if strings.Contains(value, "...") {
		return "", "", errors.New("revision range must use two dots, not three")
	}
	index := strings.Index(value, "..")
	if index < 0 {
		return "", value, nil
	}
	from, to := value[:index], value[index+2:]
	if from == "" || to == "" || strings.Contains(to, "..") {
		return "", "", fmt.Errorf("invalid revision range %q", value)
	}
	return from, to, nil
}

func writeStatsText(out io.Writer, result report.Aggregate) {
	fmt.Fprintf(out, "Range: %s\n", displayRevisionRange(result.From, result.To))
	fmt.Fprintf(out, "Commits: %d total, %d annotated\n", result.Commits.Total, result.Commits.Annotated)
	fmt.Fprintf(out, "Lines: %d\n", result.Totals.Lines)
	fmt.Fprintf(out, "Human: %d\n", result.Totals.Human)
	fmt.Fprintf(out, "AI: %d\n", result.Totals.AI)
	fmt.Fprintf(out, "Untracked: %d\n", result.Totals.Untracked)
	fmt.Fprintf(out, "Human override: %d\n", result.Totals.HumanOverride)

	fmt.Fprintln(out, "Agents:")
	for _, agent := range result.Agents {
		fmt.Fprintf(out, "  %s: %d lines (human %d, AI %d, untracked %d, human override %d)\n",
			agent.Agent, agent.Lines, agent.Human, agent.AI, agent.Untracked, agent.HumanOverride)
		for _, model := range agent.Models {
			fmt.Fprintf(out, "    model %s: %d lines (human %d, AI %d, untracked %d, human override %d)\n",
				model.Model, model.Lines, model.Human, model.AI, model.Untracked, model.HumanOverride)
		}
	}

	fmt.Fprintln(out, "Sessions:")
	for _, session := range result.Sessions {
		fmt.Fprintf(out, "  %s (agent %s, model %s): %d lines (human %d, AI %d, untracked %d, human override %d)\n",
			session.Session, session.Agent, session.Model, session.Lines,
			session.Human, session.AI, session.Untracked, session.HumanOverride)
	}

	fmt.Fprintln(out, "Files:")
	for _, file := range result.Files {
		fmt.Fprintf(out, "  %s: %d lines (human %d, AI %d, untracked %d, human override %d)\n",
			file.Path, file.Lines, file.Human, file.AI, file.Untracked, file.HumanOverride)
	}

	fmt.Fprintln(out, "Commits:")
	for _, commit := range result.Commit {
		fmt.Fprintf(out, "  %s (%s): %d lines (human %d, AI %d, untracked %d, human override %d)\n",
			commit.Commit, commit.Timestamp, commit.Lines,
			commit.Human, commit.AI, commit.Untracked, commit.HumanOverride)
	}
}

func displayRevisionRange(from, to string) string {
	switch {
	case from != "" && to != "":
		return from + ".." + to
	case from != "":
		return from + "..HEAD"
	case to != "":
		return to
	default:
		return "HEAD"
	}
}
