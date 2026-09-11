package dashboard

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/report"
)

const maxRangeCommits = 10_000

type rangePageView struct {
	Version        int
	Range          string
	CommitCount    int
	AnnotatedCount int
	TotalLines     int
	AILines        int
	AIPercent      float64
	FileCount      int
	AgentCount     int
	ModelCount     int
	Authors        []rangeTotalsView
	Files          []rangeTotalsView
	Agents         []rangeTotalsView
	Models         []rangeTotalsView
	Trend          []rangeTrendView
	Warnings       []string
}

type rangeTotalsView struct {
	Name          string
	Lines         int
	Human         int
	AI            int
	HumanOverride int
	Untracked     int
	Percent       float64
}

type rangeTrendView struct {
	Commit        string
	CommitShort   string
	Timestamp     string
	timestamp     time.Time
	Lines         int
	Human         int
	AI            int
	HumanOverride int
	Untracked     int
}

// RenderRange returns deterministic, self-contained HTML for a commit range.
// Unlike Render, it never receives or emits source lines.
func RenderRange(aggregate report.Aggregate) ([]byte, error) {
	view, err := buildRangeView(aggregate)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := rangeReportTemplate.Execute(&output, view); err != nil {
		return nil, fmt.Errorf("render dashboard range: %w", err)
	}
	if output.Len() > maxHTMLBytes {
		return nil, fmt.Errorf("dashboard HTML exceeds %d bytes", maxHTMLBytes)
	}
	return output.Bytes(), nil
}

func buildRangeView(aggregate report.Aggregate) (rangePageView, error) {
	if aggregate.Version != model.NoteVersion {
		return rangePageView{}, fmt.Errorf(
			"dashboard range has unsupported report version %d",
			aggregate.Version,
		)
	}
	if aggregate.Commits.Total < 0 || aggregate.Commits.Annotated < 0 ||
		aggregate.Commits.Annotated > aggregate.Commits.Total {
		return rangePageView{}, errors.New("dashboard range has invalid commit counts")
	}
	if aggregate.Commits.Total > maxRangeCommits || len(aggregate.Commit) > maxRangeCommits {
		return rangePageView{}, fmt.Errorf(
			"dashboard range exceeds %d commits",
			maxRangeCommits,
		)
	}
	if err := validateRangeText("from revision", aggregate.From, true); err != nil {
		return rangePageView{}, err
	}
	if err := validateRangeText("to revision", aggregate.To, true); err != nil {
		return rangePageView{}, err
	}
	if err := validateRangeTotals("range totals", aggregate.Totals); err != nil {
		return rangePageView{}, err
	}

	sourceBytes := len(aggregate.From) + len(aggregate.To)
	files := make([]rangeTotalsView, 0, len(aggregate.Files))
	for _, file := range aggregate.Files {
		if err := validateRangeText("file path", file.Path, false); err != nil {
			return rangePageView{}, err
		}
		if err := validateRangeTotals("file "+file.Path, file.Totals); err != nil {
			return rangePageView{}, err
		}
		sourceBytes += len(file.Path)
		files = append(files, rangeTotalsView{
			Name:          file.Path,
			Lines:         file.Lines,
			Human:         file.Human,
			AI:            file.AI,
			HumanOverride: file.HumanOverride,
			Untracked:     file.Untracked,
			Percent:       percent(file.Lines, aggregate.Totals.Lines),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	agents := make([]rangeTotalsView, 0, len(aggregate.Agents))
	modelTotalsByName := map[string]report.Totals{}
	for _, agent := range aggregate.Agents {
		if err := validateRangeText("agent", agent.Agent, false); err != nil {
			return rangePageView{}, err
		}
		if err := validateRangeTotals("agent "+agent.Agent, agent.Totals); err != nil {
			return rangePageView{}, err
		}
		sourceBytes += len(agent.Agent)
		agents = append(agents, rangeTotalsView{
			Name:          agent.Agent,
			Lines:         agent.Lines,
			Human:         agent.Human,
			AI:            agent.AI,
			HumanOverride: agent.HumanOverride,
			Untracked:     agent.Untracked,
			Percent:       percent(agent.Lines, aggregate.Totals.Lines),
		})
		for _, modelTotal := range agent.Models {
			if err := validateRangeText("model", modelTotal.Model, false); err != nil {
				return rangePageView{}, err
			}
			if err := validateRangeTotals(
				"model "+agent.Agent+"/"+modelTotal.Model,
				modelTotal.Totals,
			); err != nil {
				return rangePageView{}, err
			}
			sourceBytes += len(modelTotal.Model)
			current := modelTotalsByName[modelTotal.Model]
			if err := addRangeTotals(&current, modelTotal.Totals); err != nil {
				return rangePageView{}, fmt.Errorf("aggregate model %q: %w", modelTotal.Model, err)
			}
			modelTotalsByName[modelTotal.Model] = current
		}
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })

	models := make([]rangeTotalsView, 0, len(modelTotalsByName))
	for name, totals := range modelTotalsByName {
		models = append(models, rangeTotalsView{
			Name:          name,
			Lines:         totals.Lines,
			Human:         totals.Human,
			AI:            totals.AI,
			HumanOverride: totals.HumanOverride,
			Untracked:     totals.Untracked,
			Percent:       percent(totals.Lines, aggregate.Totals.Lines),
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })

	trend := make([]rangeTrendView, 0, len(aggregate.Commit))
	for _, commit := range aggregate.Commit {
		if !model.ValidObjectID(commit.Commit) {
			return rangePageView{}, fmt.Errorf("dashboard range has invalid commit %q", commit.Commit)
		}
		if commit.Timestamp == "" {
			return rangePageView{}, fmt.Errorf("dashboard range commit %s has no timestamp", commit.Commit)
		}
		timestamp, err := time.Parse(time.RFC3339Nano, commit.Timestamp)
		if err != nil {
			return rangePageView{}, fmt.Errorf(
				"dashboard range commit %s has invalid timestamp: %w",
				commit.Commit,
				err,
			)
		}
		if err := validateRangeTotals("commit "+commit.Commit, commit.Totals); err != nil {
			return rangePageView{}, err
		}
		sourceBytes += len(commit.Commit) + len(commit.Timestamp)
		trend = append(trend, rangeTrendView{
			Commit:        commit.Commit,
			CommitShort:   shortID(commit.Commit),
			Timestamp:     commit.Timestamp,
			timestamp:     timestamp,
			Lines:         commit.Lines,
			Human:         commit.Human,
			AI:            commit.AI,
			HumanOverride: commit.HumanOverride,
			Untracked:     commit.Untracked,
		})
	}
	sort.Slice(trend, func(i, j int) bool {
		if !trend[i].timestamp.Equal(trend[j].timestamp) {
			return trend[i].timestamp.Before(trend[j].timestamp)
		}
		return trend[i].Commit < trend[j].Commit
	})

	authors := []rangeTotalsView{
		rangeTotalsView{Name: string(model.AuthorHuman), Lines: aggregate.Totals.Human},
		{Name: string(model.AuthorHumanOverride), Lines: aggregate.Totals.HumanOverride},
		{Name: string(model.AuthorAI), Lines: aggregate.Totals.AI},
		{Name: string(model.AuthorUntracked), Lines: aggregate.Totals.Untracked},
	}
	for index := range authors {
		authors[index].Percent = percent(authors[index].Lines, aggregate.Totals.Lines)
	}

	warnings := uniqueStrings(aggregate.Warnings)
	for _, warning := range warnings {
		if err := validateRangeText("warning", warning, false); err != nil {
			return rangePageView{}, err
		}
		sourceBytes += len(warning)
	}
	if sourceBytes > maxSourceBytes {
		return rangePageView{}, fmt.Errorf(
			"dashboard range content and metadata exceed %d bytes",
			maxSourceBytes,
		)
	}

	return rangePageView{
		Version:        aggregate.Version,
		Range:          displayRange(aggregate.From, aggregate.To),
		CommitCount:    aggregate.Commits.Total,
		AnnotatedCount: aggregate.Commits.Annotated,
		TotalLines:     aggregate.Totals.Lines,
		AILines:        aggregate.Totals.AI,
		AIPercent:      percent(aggregate.Totals.AI, aggregate.Totals.Lines),
		FileCount:      len(files),
		AgentCount:     len(agents),
		ModelCount:     len(models),
		Authors:        authors,
		Files:          files,
		Agents:         agents,
		Models:         models,
		Trend:          trend,
		Warnings:       warnings,
	}, nil
}

func validateRangeTotals(name string, totals report.Totals) error {
	fields := []struct {
		name  string
		value int
	}{
		{name: "human", value: totals.Human},
		{name: "ai", value: totals.AI},
		{name: "human override", value: totals.HumanOverride},
		{name: "untracked", value: totals.Untracked},
		{name: "lines", value: totals.Lines},
	}
	for _, field := range fields {
		if field.value < 0 {
			return fmt.Errorf("dashboard %s has negative %s lines", name, field.name)
		}
	}
	sum := 0
	for _, value := range []int{totals.Human, totals.AI, totals.HumanOverride, totals.Untracked} {
		var err error
		sum, err = checkedRangeAdd(sum, value)
		if err != nil {
			return fmt.Errorf("dashboard %s: %w", name, err)
		}
	}
	if sum != totals.Lines {
		return fmt.Errorf(
			"dashboard %s line total %d does not equal author total %d",
			name,
			totals.Lines,
			sum,
		)
	}
	return nil
}

func addRangeTotals(dst *report.Totals, src report.Totals) error {
	if err := validateRangeTotals("model totals", src); err != nil {
		return err
	}
	values := []*int{&dst.Human, &dst.AI, &dst.HumanOverride, &dst.Untracked, &dst.Lines}
	addends := []int{src.Human, src.AI, src.HumanOverride, src.Untracked, src.Lines}
	for index := range values {
		value, err := checkedRangeAdd(*values[index], addends[index])
		if err != nil {
			return err
		}
		*values[index] = value
	}
	return nil
}

func checkedRangeAdd(current, value int) (int, error) {
	if current < 0 || value < 0 {
		return 0, errors.New("line total cannot be negative")
	}
	maxInt := int(^uint(0) >> 1)
	if current > maxInt-value {
		return 0, errors.New("line total overflows int")
	}
	return current + value, nil
}

func validateRangeText(name, value string, emptyOK bool) error {
	if !emptyOK && value == "" {
		return fmt.Errorf("dashboard %s is empty", name)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("dashboard %s is not valid UTF-8", name)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return fmt.Errorf("dashboard %s contains a control character", name)
		}
	}
	return nil
}

func displayRange(from, to string) string {
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

var rangeReportTemplate = template.Must(template.New("dashboard-range").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">
  <title>git-byline dashboard - {{.Range}}</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #050b14;
      --surface: #0d1828;
      --surface-strong: #111f33;
      --border: #263952;
      --text: #f8fafc;
      --muted: #8fa3bd;
      --faint: #60738d;
      --green: #22c55e;
      --human: #60a5fa;
      --human-override: #f59e0b;
      --untracked: #94a3b8;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-width: 320px;
      color: var(--text);
      background:
        radial-gradient(circle at 82% 0%, rgba(249, 115, 22, 0.16), transparent 34rem),
        radial-gradient(circle at 8% 90%, rgba(37, 99, 235, 0.12), transparent 38rem),
        var(--bg);
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      display: flex;
      align-items: center;
      gap: 1rem;
      min-height: 5.5rem;
      padding: 1rem 2rem;
      border-bottom: 1px solid rgba(38, 57, 82, 0.8);
      background: rgba(5, 11, 20, 0.9);
    }
    h1, h2, h3, p { margin: 0; }
    h1 { font-size: clamp(1.35rem, 2.2vw, 2rem); letter-spacing: -0.03em; }
    h2 { font-size: 1.25rem; }
    .subtitle { color: var(--muted); margin-top: 0.3rem; }
    .range {
      margin-left: auto;
      padding: 0.65rem 1rem;
      border: 1px solid #334764;
      border-radius: 999px;
      color: #dce7f5;
      background: #17243a;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.8rem;
    }
    main { width: min(1500px, calc(100% - 2rem)); margin: 0 auto; padding: 1.5rem 0 3rem; }
    .card {
      border: 1px solid var(--border);
      border-radius: 1.15rem;
      background: rgba(13, 24, 40, 0.94);
      box-shadow: 0 1rem 3rem rgba(0, 0, 0, 0.18);
    }
    .kpis { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 1rem; }
    .kpi { padding: 1.25rem 1.35rem; }
    .kpi strong { display: block; font-size: 2rem; line-height: 1.1; }
    .kpi span { display: block; margin-top: 0.55rem; color: var(--muted); }
    .accent { color: #c084fc; }
    .healthy { color: var(--green); }
    .panel { margin-top: 1rem; padding: 1.5rem; }
    .breakdowns { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1rem; }
    .breakdowns .panel { margin-top: 1rem; }
    .table-scroll { overflow-x: auto; margin-top: 1rem; }
    table {
      width: 100%;
      border-collapse: collapse;
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 0.82rem;
    }
    th, td { padding: 0.55rem 0.65rem; border-bottom: 1px solid #1e3048; text-align: right; white-space: nowrap; }
    th:first-child, td:first-child { text-align: left; }
    thead th { color: var(--muted); font-weight: 600; }
    tbody tr:nth-child(even) { background: rgba(255, 255, 255, 0.012); }
    .muted { margin-top: 1rem; color: var(--muted); }
    .warning { margin-top: 1rem; padding: 0.8rem 1rem; border: 1px solid rgba(245, 158, 11, 0.45); border-radius: 0.8rem; color: #fcd34d; background: rgba(245, 158, 11, 0.08); }
    .tone-human { color: var(--human); }
    .tone-human-override { color: var(--human-override); }
    .tone-ai { color: #c084fc; }
    .tone-untracked { color: var(--untracked); }
    footer { display: flex; justify-content: space-between; gap: 1rem; padding-top: 1.5rem; color: var(--faint); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.78rem; }
    @media (max-width: 1000px) {
      .kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .breakdowns { grid-template-columns: 1fr; }
    }
    @media (max-width: 640px) {
      header { padding: 0.85rem 1rem; }
      .range { display: none; }
      main { width: min(100% - 1rem, 1500px); }
      .kpis { gap: 0.5rem; }
      .panel { padding: 1rem; }
    }
  </style>
</head>
<body>
  <header>
    <div>
      <h1>git-byline attribution dashboard</h1>
      <p class="subtitle">Local evidence aggregated from Git notes</p>
    </div>
    <div class="range">range {{.Range}}</div>
  </header>
  <main>
    <section class="kpis" aria-label="Range attribution summary">
      <div class="card kpi"><strong>{{.TotalLines}}</strong><span>attributed lines</span></div>
      <div class="card kpi"><strong class="accent">{{printf "%.1f" .AIPercent}}%</strong><span>AI observed</span></div>
      <div class="card kpi"><strong>{{.CommitCount}}</strong><span>commits</span></div>
      <div class="card kpi"><strong>{{.AnnotatedCount}}</strong><span>annotated commits</span></div>
      <div class="card kpi"><strong class="healthy">{{.FileCount}}</strong><span>files</span></div>
    </section>

    <section class="card panel">
      <h2>Commit trend</h2>
      <p class="subtitle">Line counts by committer timestamp. No source lines are rendered in range mode.</p>
      {{if .Trend}}
      <div class="table-scroll">
        <table aria-label="Attribution trend over commit timestamps">
          <thead><tr><th>Timestamp</th><th>Commit</th><th>Lines</th><th>Human</th><th>AI</th><th>Human override</th><th>Untracked</th></tr></thead>
          <tbody>
            {{range .Trend}}
            <tr><td>{{.Timestamp}}</td><td><code title="{{.Commit}}">{{.CommitShort}}</code></td><td>{{.Lines}}</td><td>{{.Human}}</td><td>{{.AI}}</td><td>{{.HumanOverride}}</td><td>{{.Untracked}}</td></tr>
            {{end}}
          </tbody>
        </table>
      </div>
      {{else}}<p class="muted">No annotated commits in this range.</p>{{end}}
    </section>

    <section class="breakdowns">
      <div class="card panel">
        <h2>Author classes</h2>
        <div class="table-scroll">
          <table aria-label="Attribution by author class">
            <thead><tr><th>Class</th><th>Lines</th><th>Share</th></tr></thead>
            <tbody>
              {{range .Authors}}
              <tr><td class="tone-{{.Name}}">{{.Name}}</td><td>{{.Lines}}</td><td>{{printf "%.1f" .Percent}}%</td></tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </div>
      <div class="card panel">
        <h2>Files</h2>
        {{if .Files}}
        <div class="table-scroll">
          <table aria-label="Attribution by file">
            <thead><tr><th>Path</th><th>Lines</th><th>Human</th><th>AI</th><th>Human override</th><th>Untracked</th><th>Share</th></tr></thead>
            <tbody>
              {{range .Files}}
              <tr><td><code>{{.Name}}</code></td><td>{{.Lines}}</td><td>{{.Human}}</td><td>{{.AI}}</td><td>{{.HumanOverride}}</td><td>{{.Untracked}}</td><td>{{printf "%.1f" .Percent}}%</td></tr>
              {{end}}
            </tbody>
          </table>
        </div>
        {{else}}<p class="muted">No files in this range.</p>{{end}}
      </div>
      <div class="card panel">
        <h2>Agents</h2>
        {{if .Agents}}
        <div class="table-scroll">
          <table aria-label="Attribution by agent">
            <thead><tr><th>Agent</th><th>Lines</th><th>Human</th><th>AI</th><th>Human override</th><th>Untracked</th><th>Share</th></tr></thead>
            <tbody>
              {{range .Agents}}
              <tr><td>{{.Name}}</td><td>{{.Lines}}</td><td>{{.Human}}</td><td>{{.AI}}</td><td>{{.HumanOverride}}</td><td>{{.Untracked}}</td><td>{{printf "%.1f" .Percent}}%</td></tr>
              {{end}}
            </tbody>
          </table>
        </div>
        {{else}}<p class="muted">No agents in this range.</p>{{end}}
      </div>
      <div class="card panel">
        <h2>Models</h2>
        {{if .Models}}
        <div class="table-scroll">
          <table aria-label="Attribution by model">
            <thead><tr><th>Model</th><th>Lines</th><th>Human</th><th>AI</th><th>Human override</th><th>Untracked</th><th>Share</th></tr></thead>
            <tbody>
              {{range .Models}}
              <tr><td>{{.Name}}</td><td>{{.Lines}}</td><td>{{.Human}}</td><td>{{.AI}}</td><td>{{.HumanOverride}}</td><td>{{.Untracked}}</td><td>{{printf "%.1f" .Percent}}%</td></tr>
              {{end}}
            </tbody>
          </table>
        </div>
        {{else}}<p class="muted">No models in this range.</p>{{end}}
      </div>
    </section>

    {{range .Warnings}}<div class="warning">{{.}}</div>{{end}}
    <footer><span>self-contained HTML / no network / no telemetry</span><span>range report v{{.Version}}</span></footer>
  </main>
</body>
</html>
`))
