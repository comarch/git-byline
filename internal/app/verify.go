package app

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

const (
	verifyMaxCommits = 10_000
	verifyMaxLines   = 100_000
	verifyMaxBytes   = 16 << 20
	bylineNotesRef   = "refs/notes/byline"
)

type verifyIssue struct {
	Commit  string `json:"commit"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

type verifyResult struct {
	Version int    `json:"version"`
	From    string `json:"from"`
	To      string `json:"to"`
	Deep    bool   `json:"deep"`
	Commits struct {
		Total     int `json:"total"`
		Annotated int `json:"annotated"`
	} `json:"commits"`
	Issues []verifyIssue `json:"issues"`
}

func runVerify(env *Env, command *command, args []string) (int, error) {
	deep, jsonOutput, rest, err := parseVerifyArgs(args)
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
	result, err := verifyRepository(repo, from, to, deep)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if jsonOutput {
		if err := writeJSON(env, result); err != nil {
			return operationalError(env, command.name, err)
		}
	} else {
		writeVerifyText(env, result)
	}
	if len(result.Issues) > 0 {
		return ExitFailure, fmt.Errorf("verification failed with %d issue(s)", len(result.Issues))
	}
	return ExitSuccess, nil
}

func parseVerifyArgs(args []string) (bool, bool, []string, error) {
	var deep, jsonOutput bool
	var rest []string
	for _, arg := range args {
		switch arg {
		case "--deep":
			if deep {
				return false, false, nil, errors.New("--deep specified more than once")
			}
			deep = true
		case "--json":
			if jsonOutput {
				return false, false, nil, errors.New("--json specified more than once")
			}
			jsonOutput = true
		case "-h", "--help":
			return false, false, nil, flag.ErrHelp
		default:
			if strings.HasPrefix(arg, "-") {
				return false, false, nil, fmt.Errorf("unknown flag %q", arg)
			}
			rest = append(rest, arg)
		}
	}
	return deep, jsonOutput, rest, nil
}

func verifyRepository(repo *gitcmd.Repo, from, to string, deep bool) (verifyResult, error) {
	if repo == nil {
		return verifyResult{}, errors.New("verify repository is nil")
	}
	commits, err := repo.RevList(from, to, verifyMaxCommits+1)
	if err != nil {
		return verifyResult{}, fmt.Errorf("list verification commits: %w", err)
	}
	if len(commits) > verifyMaxCommits {
		return verifyResult{}, fmt.Errorf(
			"verification range exceeds %d commits; narrow the range",
			verifyMaxCommits,
		)
	}
	noteCommits, err := repo.NoteCommits(bylineNotesRef)
	if err != nil {
		return verifyResult{}, fmt.Errorf("list attribution notes: %w", err)
	}
	annotated := make(map[string]bool, len(noteCommits))
	for _, commit := range noteCommits {
		annotated[commit] = true
	}
	result := verifyResult{
		Version: 1,
		From:    from,
		To:      to,
		Deep:    deep,
		Issues:  make([]verifyIssue, 0),
	}
	result.Commits.Total = len(commits)
	for _, commit := range commits {
		if !annotated[commit] {
			continue
		}
		result.Commits.Annotated++
		data, found, err := repo.ReadNote(commit)
		if err != nil {
			return verifyResult{}, fmt.Errorf("read attribution note on %s: %w", commit, err)
		}
		if !found {
			result.Issues = append(result.Issues, verifyIssue{
				Commit:  commit,
				Message: "note listed but missing",
			})
			continue
		}
		issues, err := verifyNote(repo, commit, data, deep)
		if err != nil {
			return verifyResult{}, fmt.Errorf("verify attribution note on %s: %w", commit, err)
		}
		result.Issues = append(result.Issues, issues...)
	}
	sort.Slice(result.Issues, func(i, j int) bool {
		if result.Issues[i].Commit != result.Issues[j].Commit {
			return result.Issues[i].Commit < result.Issues[j].Commit
		}
		if result.Issues[i].Path != result.Issues[j].Path {
			return result.Issues[i].Path < result.Issues[j].Path
		}
		return result.Issues[i].Message < result.Issues[j].Message
	})
	return result, nil
}

func verifyNote(repo *gitcmd.Repo, commit string, data []byte, deep bool) ([]verifyIssue, error) {
	note, err := notes.Decode(data)
	if err != nil {
		return []verifyIssue{{
			Commit:  commit,
			Message: fmt.Sprintf("decode note: %s", noteDecodeErrorClass(err)),
		}}, nil
	}
	paths := make([]string, 0, len(note.Files))
	for path := range note.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	issues := make([]verifyIssue, 0)
	totalLines := 0
	var totalBytes int64
	for _, path := range paths {
		file := note.Files[path]
		normalized, normalizeErr := gitcmd.NormalizePath(path)
		if normalizeErr != nil {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("invalid path: %v", normalizeErr),
			})
			continue
		}
		if normalized != path {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: "path is not normalized",
			})
			continue
		}
		coverage, coverageErr := verifyRangeCoverage(file.Ranges)
		if coverageErr != nil {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("invalid ranges: %v", coverageErr),
			})
		}
		if coverage > 0 && totalLines > verifyMaxLines-coverage {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("note exceeds %d lines", verifyMaxLines),
			})
		} else {
			totalLines += coverage
		}

		blob, exists, blobErr := repo.BlobID(commit, path)
		if blobErr != nil {
			if isTreeEntryIssue(blobErr) {
				issues = append(issues, verifyIssue{
					Commit:  commit,
					Path:    path,
					Message: "non-blob tree entry",
				})
				continue
			}
			return nil, fmt.Errorf("inspect %q: %w", path, blobErr)
		}
		if !exists {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: "attributed path is missing from commit",
			})
			continue
		}
		if blob != file.Blob {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("note blob %s does not match commit blob %s", file.Blob, blob),
			})
			continue
		}
		if !deep {
			continue
		}
		size, sizeErr := repo.BlobSize(file.Blob)
		if sizeErr != nil {
			return nil, fmt.Errorf("inspect blob for %q: %w", path, sizeErr)
		}
		if size > verifyMaxBytes || totalBytes > verifyMaxBytes-size {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("note content exceeds %d bytes", verifyMaxBytes),
			})
			continue
		}
		totalBytes += size
		content, readErr := repo.ReadBlob(file.Blob)
		if readErr != nil {
			return nil, fmt.Errorf("read blob for %q: %w", path, readErr)
		}
		if int64(len(content)) != size {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("blob size changed from %d to %d bytes", size, len(content)),
			})
			continue
		}
		lines, splitErr := engine.SplitLines(content)
		if splitErr != nil {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("read blob lines: %v", splitErr),
			})
			continue
		}
		if len(lines) > verifyMaxLines {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("blob lines exceed %d lines", verifyMaxLines),
			})
			continue
		}
		if len(lines) != coverage {
			issues = append(issues, verifyIssue{
				Commit:  commit,
				Path:    path,
				Message: fmt.Sprintf("range coverage is %d lines, blob has %d", coverage, len(lines)),
			})
		}
	}
	return issues, nil
}

func isTreeEntryIssue(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "git returned invalid tree entry", "git returned non-blob tree entry":
		return true
	default:
		return false
	}
}

func noteDecodeErrorClass(err error) string {
	if err == nil {
		return "unknown"
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "unsupported note version"),
		strings.Contains(message, "version changed"):
		return "unsupported version"
	case strings.Contains(message, "unknown field"):
		return "unknown field"
	case strings.Contains(message, "exceeds"),
		strings.Contains(message, "more than"):
		return "size limit"
	case strings.Contains(message, "invalid character"),
		strings.Contains(message, "unexpected end"),
		strings.Contains(message, "multiple JSON values"):
		return "malformed JSON"
	default:
		return "validation"
	}
}

func verifyRangeCoverage(ranges []model.Range) (int, error) {
	if len(ranges) == 0 {
		return 0, nil
	}
	coverage := ranges[len(ranges)-1].End
	if coverage < 0 {
		return 0, errors.New("range endpoint is negative")
	}
	if err := model.ValidateRanges(ranges, coverage); err != nil {
		return coverage, err
	}
	return coverage, nil
}

func writeVerifyText(env *Env, result verifyResult) {
	if len(result.Issues) == 0 {
		fmt.Fprintf(env.Stdout, "verified %d annotated commits (%d commits total)\n",
			result.Commits.Annotated, result.Commits.Total)
		return
	}
	for _, issue := range result.Issues {
		if issue.Path == "" {
			fmt.Fprintf(env.Stdout, "FAIL %s: %s\n", issue.Commit, issue.Message)
			continue
		}
		fmt.Fprintf(env.Stdout, "FAIL %s %s: %s\n", issue.Commit, issue.Path, issue.Message)
	}
	fmt.Fprintf(env.Stdout, "verified %d annotated commits (%d commits total), %d issue(s)\n",
		result.Commits.Annotated, result.Commits.Total, len(result.Issues))
}
