package app

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/report"
)

const checkMaxCommits = 10_000

type checkOptions struct {
	maxAI           int
	maxUntracked    int
	requireNote     bool
	json            bool
	hasMaxAI        bool
	hasMaxUntracked bool
}

type policyViolation struct {
	Rule          string  `json:"rule"`
	Commit        string  `json:"commit,omitempty"`
	Message       string  `json:"message"`
	ActualPercent float64 `json:"actual_percent"`
	LimitPercent  int     `json:"limit_percent"`
}

type checkResult struct {
	Version int    `json:"version"`
	From    string `json:"from"`
	To      string `json:"to"`
	Commits struct {
		Total     int `json:"total"`
		Annotated int `json:"annotated"`
	} `json:"commits"`
	Totals report.Totals `json:"totals"`
	// Authors reports human and human-override lines per identity. The
	// text report stays a terse gate, so this is JSON only.
	Authors    []report.AuthorTotals `json:"authors"`
	Violations []policyViolation     `json:"violations"`
	Warnings   []string              `json:"warnings,omitempty"`
}

func runCheck(env *Env, command *command, args []string) (int, error) {
	options, rest, err := parseCheckArgs(args)
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
	aggregate, err := report.Collect(repo, from, to, 0)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result := checkResult{
		Version:    1,
		From:       from,
		To:         to,
		Totals:     aggregate.Totals,
		Authors:    append([]report.AuthorTotals(nil), aggregate.Authors...),
		Violations: make([]policyViolation, 0),
		Warnings:   append([]string(nil), aggregate.Warnings...),
	}
	if result.Authors == nil {
		result.Authors = make([]report.AuthorTotals, 0)
	}
	result.Commits.Total = aggregate.Commits.Total
	result.Commits.Annotated = aggregate.Commits.Annotated
	result.Violations = append(result.Violations, policyViolations(aggregate, options)...)
	if options.requireNote {
		missing, err := missingNoteViolations(repo, from, to)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		result.Violations = append(result.Violations, missing...)
	}
	sort.Slice(result.Violations, func(i, j int) bool {
		if result.Violations[i].Rule != result.Violations[j].Rule {
			return result.Violations[i].Rule < result.Violations[j].Rule
		}
		if result.Violations[i].Commit != result.Violations[j].Commit {
			return result.Violations[i].Commit < result.Violations[j].Commit
		}
		return result.Violations[i].Message < result.Violations[j].Message
	})
	if options.json {
		if err := writeJSON(env, result); err != nil {
			return operationalError(env, command.name, err)
		}
	} else {
		writeWarnings(env, result.Warnings)
		writeCheckText(env, result)
	}
	if len(result.Violations) > 0 {
		return ExitFailure, fmt.Errorf("policy check failed with %d violation(s)", len(result.Violations))
	}
	return ExitSuccess, nil
}

func parseCheckArgs(args []string) (checkOptions, []string, error) {
	options := checkOptions{maxAI: -1, maxUntracked: -1}
	var rest []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			if options.json {
				return checkOptions{}, nil, errors.New("--json specified more than once")
			}
			options.json = true
		case arg == "--require-note":
			if options.requireNote {
				return checkOptions{}, nil, errors.New("--require-note specified more than once")
			}
			options.requireNote = true
		case arg == "-h" || arg == "--help":
			return checkOptions{}, nil, flag.ErrHelp
		case arg == "--max-ai-percent":
			if options.hasMaxAI {
				return checkOptions{}, nil, errors.New("--max-ai-percent specified more than once")
			}
			value, next, err := nextFlagValue(args, index, arg)
			if err != nil {
				return checkOptions{}, nil, err
			}
			options.maxAI, err = parsePercent(value, arg)
			if err != nil {
				return checkOptions{}, nil, err
			}
			options.hasMaxAI = true
			index = next
		case strings.HasPrefix(arg, "--max-ai-percent="):
			if options.hasMaxAI {
				return checkOptions{}, nil, errors.New("--max-ai-percent specified more than once")
			}
			value := strings.TrimPrefix(arg, "--max-ai-percent=")
			var err error
			options.maxAI, err = parsePercent(value, "--max-ai-percent")
			if err != nil {
				return checkOptions{}, nil, err
			}
			options.hasMaxAI = true
		case arg == "--max-untracked-percent":
			if options.hasMaxUntracked {
				return checkOptions{}, nil, errors.New("--max-untracked-percent specified more than once")
			}
			value, next, err := nextFlagValue(args, index, arg)
			if err != nil {
				return checkOptions{}, nil, err
			}
			options.maxUntracked, err = parsePercent(value, arg)
			if err != nil {
				return checkOptions{}, nil, err
			}
			options.hasMaxUntracked = true
			index = next
		case strings.HasPrefix(arg, "--max-untracked-percent="):
			if options.hasMaxUntracked {
				return checkOptions{}, nil, errors.New("--max-untracked-percent specified more than once")
			}
			value := strings.TrimPrefix(arg, "--max-untracked-percent=")
			var err error
			options.maxUntracked, err = parsePercent(value, "--max-untracked-percent")
			if err != nil {
				return checkOptions{}, nil, err
			}
			options.hasMaxUntracked = true
		default:
			if strings.HasPrefix(arg, "-") {
				return checkOptions{}, nil, fmt.Errorf("unknown flag %q", arg)
			}
			rest = append(rest, arg)
		}
	}
	return options, rest, nil
}

func nextFlagValue(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], index + 1, nil
}

func parsePercent(value, name string) (int, error) {
	percent, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer from 0 to 100: %w", name, err)
	}
	if percent < 0 || percent > 100 {
		return 0, fmt.Errorf("%s must be between 0 and 100", name)
	}
	return percent, nil
}

func policyViolations(aggregate report.Aggregate, options checkOptions) []policyViolation {
	result := make([]policyViolation, 0, 2)
	if options.maxAI >= 0 && exceedsPercent(aggregate.Totals.AI, aggregate.Totals.Lines, options.maxAI) {
		result = append(result, policyViolation{
			Rule:          "max-ai-percent",
			Message:       fmt.Sprintf("AI attribution is %.2f%%, above maximum %d%% (%d of %d lines)", percentOf(aggregate.Totals.AI, aggregate.Totals.Lines), options.maxAI, aggregate.Totals.AI, aggregate.Totals.Lines),
			ActualPercent: percentOf(aggregate.Totals.AI, aggregate.Totals.Lines),
			LimitPercent:  options.maxAI,
		})
	}
	if options.maxUntracked >= 0 && exceedsPercent(aggregate.Totals.Untracked, aggregate.Totals.Lines, options.maxUntracked) {
		result = append(result, policyViolation{
			Rule:          "max-untracked-percent",
			Message:       fmt.Sprintf("untracked attribution is %.2f%%, above maximum %d%% (%d of %d lines)", percentOf(aggregate.Totals.Untracked, aggregate.Totals.Lines), options.maxUntracked, aggregate.Totals.Untracked, aggregate.Totals.Lines),
			ActualPercent: percentOf(aggregate.Totals.Untracked, aggregate.Totals.Lines),
			LimitPercent:  options.maxUntracked,
		})
	}
	return result
}

func missingNoteViolations(repo *gitcmd.Repo, from, to string) ([]policyViolation, error) {
	commits, err := repo.RevList(from, to, checkMaxCommits+1)
	if err != nil {
		return nil, fmt.Errorf("list policy commits: %w", err)
	}
	if len(commits) > checkMaxCommits {
		return nil, fmt.Errorf("policy range exceeds %d commits; narrow the range", checkMaxCommits)
	}
	noteCommits, err := repo.NoteCommits(bylineNotesRef)
	if err != nil {
		return nil, fmt.Errorf("list attribution notes: %w", err)
	}
	annotated := make(map[string]bool, len(noteCommits))
	for _, commit := range noteCommits {
		annotated[commit] = true
	}
	result := make([]policyViolation, 0)
	for _, commit := range commits {
		if !annotated[commit] {
			result = append(result, policyViolation{
				Rule:    "require-note",
				Commit:  commit,
				Message: fmt.Sprintf("commit %s has no attribution note", commit),
			})
			continue
		}
		data, found, err := repo.ReadNote(commit)
		if err != nil {
			return nil, fmt.Errorf("read attribution note on %s: %w", commit, err)
		}
		if !found {
			result = append(result, policyViolation{
				Rule:    "require-note",
				Commit:  commit,
				Message: fmt.Sprintf("commit %s has no attribution note", commit),
			})
			continue
		}
		if _, err := notes.Decode(data); err != nil {
			result = append(result, policyViolation{
				Rule:   "require-note",
				Commit: commit,
				Message: fmt.Sprintf(
					"commit %s has an invalid attribution note (%s)",
					commit,
					noteDecodeErrorClass(err),
				),
			})
		}
	}
	return result, nil
}

func exceedsPercent(value, total, limit int) bool {
	if total <= 0 || value <= 0 {
		return false
	}
	return int64(value)*100 > int64(limit)*int64(total)
}

func percentOf(value, total int) float64 {
	if total <= 0 || value <= 0 {
		return 0
	}
	return float64(value) * 100 / float64(total)
}

func writeCheckText(env *Env, result checkResult) {
	fmt.Fprintf(env.Stdout, "Range: %s\n", displayRevisionRange(result.From, result.To))
	fmt.Fprintf(env.Stdout, "Commits: %d total, %d annotated\n", result.Commits.Total, result.Commits.Annotated)
	fmt.Fprintf(env.Stdout, "Lines: %d (AI %.2f%%, untracked %.2f%%)\n",
		result.Totals.Lines,
		percentOf(result.Totals.AI, result.Totals.Lines),
		percentOf(result.Totals.Untracked, result.Totals.Lines))
	if len(result.Violations) == 0 {
		fmt.Fprintln(env.Stdout, "policy passed")
		return
	}
	for _, violation := range result.Violations {
		fmt.Fprintf(env.Stdout, "FAIL %s: %s\n", violation.Rule, violation.Message)
	}
	fmt.Fprintf(env.Stdout, "policy failed with %d violation(s)\n", len(result.Violations))
}
