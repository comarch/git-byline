// Package report aggregates attribution notes over Git commit ranges.
package report

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

const (
	maxAggregateCommits   = 10_000
	maxAggregateFileLines = 100_000
)

const attributionNotesRef = "refs/notes/byline"

// Totals counts lines by attribution class.
type Totals struct {
	Human         int `json:"human"`
	AI            int `json:"ai"`
	Untracked     int `json:"untracked"`
	HumanOverride int `json:"human_override"`
	Lines         int `json:"lines"`
}

// ModelTotals contains totals for one model under one agent.
type ModelTotals struct {
	Model string `json:"model"`
	Totals
}

// AgentTotals contains totals for one agent and its models.
type AgentTotals struct {
	Agent string `json:"agent"`
	Totals
	Models []ModelTotals `json:"models"`
}

// FileTotals contains totals for one repository-relative path.
type FileTotals struct {
	Path string `json:"path"`
	Totals
}

// SessionTotals contains totals for one attribution session.
type SessionTotals struct {
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Model   string `json:"model"`
	Totals
}

// CommitTotals contains totals and the committer timestamp for one commit.
type CommitTotals struct {
	Commit    string `json:"commit"`
	Timestamp string `json:"timestamp"`
	Totals
}

// Aggregate is the deterministic result of collecting a commit range.
type Aggregate struct {
	Version int    `json:"version"`
	From    string `json:"from"`
	To      string `json:"to"`
	Commits struct {
		Total     int `json:"total"`
		Annotated int `json:"annotated"`
	} `json:"commits"`
	Totals   Totals          `json:"totals"`
	Agents   []AgentTotals   `json:"agents"`
	Files    []FileTotals    `json:"files"`
	Sessions []SessionTotals `json:"sessions"`
	Commit   []CommitTotals  `json:"commit"`
	Warnings []string        `json:"warnings,omitempty"`
}

// Collect aggregates valid attribution notes in a first-parent commit range.
// It reads note objects and commit metadata only. It never reads blobs.
func Collect(repo *gitcmd.Repo, from, to string, limit int) (Aggregate, error) {
	if repo == nil {
		return Aggregate{}, errors.New("report repository is nil")
	}
	if limit < 0 {
		return Aggregate{}, errors.New("report commit limit cannot be negative")
	}
	if limit > maxAggregateCommits {
		return Aggregate{}, fmt.Errorf(
			"report commit limit %d exceeds maximum %d; use a narrower range or a limit of %d or less",
			limit,
			maxAggregateCommits,
			maxAggregateCommits,
		)
	}
	if limit == 0 {
		limit = maxAggregateCommits
	}
	commits, err := repo.RevList(from, to, limit+1)
	if err != nil {
		return Aggregate{}, fmt.Errorf("collect commit range: %w", err)
	}
	if len(commits) > limit {
		return Aggregate{}, fmt.Errorf(
			"commit range exceeds %d commits; narrow the range or pass a smaller revision range",
			limit,
		)
	}

	noteCommits, err := repo.NoteCommits(attributionNotesRef)
	if err != nil {
		return Aggregate{}, fmt.Errorf("list attribution notes: %w", err)
	}
	annotated := make(map[string]bool, len(noteCommits))
	for _, commit := range noteCommits {
		annotated[commit] = true
	}

	result := Aggregate{
		Version:  model.NoteVersion,
		From:     from,
		To:       to,
		Agents:   make([]AgentTotals, 0),
		Files:    make([]FileTotals, 0),
		Sessions: make([]SessionTotals, 0),
		Commit:   make([]CommitTotals, 0),
	}
	result.Commits.Total = len(commits)
	files := map[string]*FileTotals{}
	agents := map[string]*AgentTotals{}
	models := map[string]map[string]*ModelTotals{}
	sessions := map[string]*SessionTotals{}
	for _, commit := range commits {
		if !annotated[commit] {
			continue
		}
		data, found, err := repo.ReadNote(commit)
		if err != nil {
			return Aggregate{}, fmt.Errorf("read attribution note on %s: %w", commit, err)
		}
		if !found {
			result.Warnings = append(result.Warnings, fmt.Sprintf("attribution note listed for %s but is missing", commit))
			continue
		}
		note, err := notes.Decode(data)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"ignored attribution note on %s: invalid note (%s)",
				commit,
				noteDecodeErrorClass(err),
			))
			continue
		}
		timestamp, err := repo.CommitTime(commit)
		if err != nil {
			return Aggregate{}, fmt.Errorf("read commit time for %s: %w", commit, err)
		}
		result.Commits.Annotated++
		commitTotals := CommitTotals{Commit: commit, Timestamp: timestamp}
		paths := make([]string, 0, len(note.Files))
		for path := range note.Files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			file := note.Files[path]
			value := files[path]
			if value == nil {
				value = &FileTotals{Path: path}
				files[path] = value
			}
			for _, item := range file.Ranges {
				count, err := rangeLineCount(item)
				if err != nil {
					return Aggregate{}, fmt.Errorf("aggregate %s %s: %w", commit, path, err)
				}
				if err := addTotals(&result.Totals, item.Attribution, count); err != nil {
					return Aggregate{}, fmt.Errorf("aggregate %s %s: %w", commit, path, err)
				}
				if err := addTotals(&value.Totals, item.Attribution, count); err != nil {
					return Aggregate{}, fmt.Errorf("aggregate %s %s: %w", commit, path, err)
				}
				if err := addTotals(&commitTotals.Totals, item.Attribution, count); err != nil {
					return Aggregate{}, fmt.Errorf("aggregate %s %s: %w", commit, path, err)
				}
				if err := addAgentTotals(agents, models, sessions, item.Attribution, count); err != nil {
					return Aggregate{}, fmt.Errorf("aggregate %s %s: %w", commit, path, err)
				}
			}
		}
		result.Commit = append(result.Commit, commitTotals)
	}
	result.Agents = sortedAgents(agents, models)
	result.Files = sortedFiles(files)
	result.Sessions = sortedSessions(sessions)
	sort.Slice(result.Commit, func(i, j int) bool {
		if result.Commit[i].Commit != result.Commit[j].Commit {
			return result.Commit[i].Commit < result.Commit[j].Commit
		}
		return result.Commit[i].Timestamp < result.Commit[j].Timestamp
	})
	result.Warnings = uniqueWarnings(result.Warnings)
	return result, nil
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

func rangeLineCount(value model.Range) (int, error) {
	if value.Start < 1 || value.End < value.Start {
		return 0, errors.New("invalid range bounds")
	}
	if value.End > maxAggregateFileLines {
		return 0, fmt.Errorf("range endpoint %d exceeds %d-line limit", value.End, maxAggregateFileLines)
	}
	return value.End - value.Start + 1, nil
}

func addTotals(totals *Totals, attribution model.Attribution, count int) error {
	var category *int
	switch attribution.Author {
	case model.AuthorHuman:
		category = &totals.Human
	case model.AuthorAI:
		category = &totals.AI
	case model.AuthorUntracked:
		category = &totals.Untracked
	case model.AuthorHumanOverride:
		category = &totals.HumanOverride
	default:
		return fmt.Errorf("unsupported author %q", attribution.Author)
	}
	lines, err := checkedAdd(totals.Lines, count)
	if err != nil {
		return err
	}
	classTotal, err := checkedAdd(*category, count)
	if err != nil {
		return err
	}
	totals.Lines = lines
	*category = classTotal
	return nil
}

func checkedAdd(current, value int) (int, error) {
	if current < 0 || value < 0 {
		return 0, errors.New("line total cannot be negative")
	}
	maxInt := int(^uint(0) >> 1)
	if current > maxInt-value {
		return 0, errors.New("line total overflows int")
	}
	return current + value, nil
}

func addAgentTotals(
	agents map[string]*AgentTotals,
	models map[string]map[string]*ModelTotals,
	sessions map[string]*SessionTotals,
	attribution model.Attribution,
	count int,
) error {
	if attribution.Author != model.AuthorAI && attribution.Author != model.AuthorHumanOverride {
		return nil
	}
	agent := agents[attribution.Agent]
	if agent == nil {
		agent = &AgentTotals{Agent: attribution.Agent}
		agents[attribution.Agent] = agent
		models[attribution.Agent] = map[string]*ModelTotals{}
	}
	if err := addTotals(&agent.Totals, attribution, count); err != nil {
		return err
	}
	modelName := attribution.Model
	if modelName == "" {
		modelName = "unknown"
	}
	modelTotals := models[attribution.Agent][modelName]
	if modelTotals == nil {
		modelTotals = &ModelTotals{Model: modelName}
		models[attribution.Agent][modelName] = modelTotals
	}
	if err := addTotals(&modelTotals.Totals, attribution, count); err != nil {
		return err
	}
	if attribution.Session == "" {
		return nil
	}
	key := attribution.Session + "\x00" + attribution.Agent + "\x00" + modelName
	session := sessions[key]
	if session == nil {
		session = &SessionTotals{
			Session: attribution.Session,
			Agent:   attribution.Agent,
			Model:   modelName,
		}
		sessions[key] = session
	}
	return addTotals(&session.Totals, attribution, count)
}

func sortedAgents(values map[string]*AgentTotals, models map[string]map[string]*ModelTotals) []AgentTotals {
	agents := make([]AgentTotals, 0, len(values))
	for _, value := range values {
		agent := *value
		for _, modelTotals := range models[value.Agent] {
			agent.Models = append(agent.Models, *modelTotals)
		}
		sort.Slice(agent.Models, func(i, j int) bool {
			return agent.Models[i].Model < agent.Models[j].Model
		})
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Agent < agents[j].Agent })
	return agents
}

func sortedFiles(values map[string]*FileTotals) []FileTotals {
	files := make([]FileTotals, 0, len(values))
	for _, value := range values {
		files = append(files, *value)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func sortedSessions(values map[string]*SessionTotals) []SessionTotals {
	sessions := make([]SessionTotals, 0, len(values))
	for _, value := range values {
		sessions = append(sessions, *value)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Session != sessions[j].Session {
			return sessions[i].Session < sessions[j].Session
		}
		if sessions[i].Agent != sessions[j].Agent {
			return sessions[i].Agent < sessions[j].Agent
		}
		return sessions[i].Model < sessions[j].Model
	})
	return sessions
}

func uniqueWarnings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
