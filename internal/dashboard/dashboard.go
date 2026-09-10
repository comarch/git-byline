// Package dashboard renders self-contained local attribution reports.
package dashboard

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"html/template"
	"math"
	"sort"
	"strings"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
)

const (
	maxFiles       = 500
	maxLines       = 100_000
	maxSourceBytes = 16 << 20
	maxHTMLBytes   = 64 << 20
)

var palette = []string{
	"tone-orange",
	"tone-coral",
	"tone-green",
	"tone-purple",
	"tone-cyan",
	"tone-pink",
	"tone-yellow",
	"tone-indigo",
}

// Report contains all local data rendered into one HTML file.
type Report struct {
	Commit string
	Status provenance.StatusResult
	Files  []provenance.BlameResult
}

type pageView struct {
	Commit           string
	CommitShort      string
	Files            []fileView
	Sources          []sourceView
	TotalLines       int
	AILines          int
	HumanLines       int
	UntrackedLines   int
	AIPercent        float64
	AIGap            float64
	HumanPercent     float64
	HumanGap         float64
	UntrackedPercent float64
	UntrackedGap     float64
	UntrackedOffset  float64
	AIOffset         float64
	FileCount        int
	ModelCount       int
	Status           provenance.StatusResult
	Warnings         []string
}

type fileView struct {
	ID             string
	Active         bool
	Path           string
	BlobShort      string
	Lines          []lineView
	Sources        []sourceView
	TotalLines     int
	AILines        int
	HumanLines     int
	UntrackedLines int
	AIPercent      float64
	Warnings       []string
}

type lineView struct {
	Number    int
	Content   string
	Source    string
	Tone      string
	ShowLabel bool
}

type sourceView struct {
	Label   string
	Kind    string
	Tone    string
	Lines   int
	Percent float64
}

type sourceCount struct {
	label string
	kind  string
	lines int
	key   string
}

// Render returns deterministic, self-contained HTML for report.
func Render(report Report) ([]byte, error) {
	view, err := buildView(report)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := reportTemplate.Execute(&output, view); err != nil {
		return nil, fmt.Errorf("render dashboard: %w", err)
	}
	if output.Len() > maxHTMLBytes {
		return nil, fmt.Errorf("dashboard HTML exceeds %d bytes", maxHTMLBytes)
	}
	return output.Bytes(), nil
}

func buildView(report Report) (pageView, error) {
	if !model.ValidObjectID(report.Commit) {
		return pageView{}, errors.New("dashboard commit is invalid")
	}
	if len(report.Files) > maxFiles {
		return pageView{}, fmt.Errorf("dashboard has %d files, limit is %d", len(report.Files), maxFiles)
	}
	files := append([]provenance.BlameResult(nil), report.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
	totalLines := 0
	totalBytes := 0
	sourceTotals := map[string]*sourceCount{}
	models := map[string]bool{}
	var warnings []string
	for _, file := range files {
		if file.Version != model.NoteVersion {
			return pageView{}, fmt.Errorf("file %q has unsupported attribution version %d", file.File, file.Version)
		}
		if file.File == "" {
			return pageView{}, errors.New("dashboard file path is empty")
		}
		if !model.ValidObjectID(file.Blob) {
			return pageView{}, fmt.Errorf("file %q has invalid blob", file.File)
		}
		if file.Commit != report.Commit {
			return pageView{}, fmt.Errorf("file %q belongs to commit %s, want %s", file.File, file.Commit, report.Commit)
		}
		totalLines += len(file.Lines)
		totalBytes += len(file.File) + len(file.Blob)
		for index, line := range file.Lines {
			if line.Number != index+1 {
				return pageView{}, fmt.Errorf("file %q line %d has number %d", file.File, index+1, line.Number)
			}
			if err := model.ValidateAttribution(line.Attribution); err != nil {
				return pageView{}, fmt.Errorf("file %q line %d: %w", file.File, line.Number, err)
			}
			totalBytes += len(line.Content) + len(line.Attribution.Agent) + len(line.Attribution.Model) +
				len(line.Attribution.Session) + len(line.Attribution.TS)
			key, label, kind := sourceOf(line.Attribution)
			value := sourceTotals[key]
			if value == nil {
				value = &sourceCount{key: key, label: label, kind: kind}
				sourceTotals[key] = value
			}
			value.lines++
			if line.Attribution.Author == model.AuthorAI {
				modelName := line.Attribution.Model
				if modelName == "" {
					modelName = "unknown"
				}
				models[line.Attribution.Agent+"\x00"+modelName] = true
			}
		}
		warnings = append(warnings, file.Warnings...)
	}
	if totalLines > maxLines {
		return pageView{}, fmt.Errorf("dashboard has %d lines, limit is %d", totalLines, maxLines)
	}
	if totalBytes > maxSourceBytes {
		return pageView{}, fmt.Errorf("dashboard content and metadata exceed %d bytes", maxSourceBytes)
	}
	if report.Status.Head != "" && report.Status.Head != report.Commit {
		return pageView{}, fmt.Errorf("dashboard status HEAD %s does not match report commit %s", report.Status.Head, report.Commit)
	}
	warnings = append(warnings, report.Status.Warnings...)
	toneBySource := assignTones(sourceTotals)
	sources := makeSourceViews(sourceTotals, toneBySource, totalLines)
	view := pageView{
		Commit:      report.Commit,
		CommitShort: shortID(report.Commit),
		Sources:     sources,
		TotalLines:  totalLines,
		FileCount:   len(files),
		ModelCount:  len(models),
		Status:      report.Status,
		Warnings:    uniqueStrings(warnings),
	}
	for _, source := range sources {
		switch source.Kind {
		case string(model.AuthorAI):
			view.AILines += source.Lines
		case string(model.AuthorHuman):
			view.HumanLines += source.Lines
		case string(model.AuthorUntracked):
			view.UntrackedLines += source.Lines
		}
	}
	view.AIPercent = percent(view.AILines, totalLines)
	view.HumanPercent = percent(view.HumanLines, totalLines)
	view.UntrackedPercent = percent(view.UntrackedLines, totalLines)
	view.AIGap = 100 - view.AIPercent
	view.HumanGap = 100 - view.HumanPercent
	view.UntrackedGap = 100 - view.UntrackedPercent
	view.UntrackedOffset = -view.HumanPercent
	view.AIOffset = -(view.HumanPercent + view.UntrackedPercent)
	for index, file := range files {
		item := fileView{
			ID:         fmt.Sprintf("file-%d", index),
			Active:     index == 0,
			Path:       file.File,
			BlobShort:  shortID(file.Blob),
			TotalLines: len(file.Lines),
			Warnings:   uniqueStrings(file.Warnings),
		}
		fileSources := map[string]*sourceCount{}
		previous := ""
		for _, line := range file.Lines {
			key, label, kind := sourceOf(line.Attribution)
			value := fileSources[key]
			if value == nil {
				value = &sourceCount{key: key, label: label, kind: kind}
				fileSources[key] = value
			}
			value.lines++
			item.Lines = append(item.Lines, lineView{
				Number:    line.Number,
				Content:   line.Content,
				Source:    label,
				Tone:      toneBySource[key],
				ShowLabel: key != previous,
			})
			previous = key
		}
		item.Sources = makeSourceViews(fileSources, toneBySource, len(file.Lines))
		for _, source := range item.Sources {
			switch source.Kind {
			case string(model.AuthorAI):
				item.AILines += source.Lines
			case string(model.AuthorHuman):
				item.HumanLines += source.Lines
			case string(model.AuthorUntracked):
				item.UntrackedLines += source.Lines
			}
		}
		item.AIPercent = percent(item.AILines, item.TotalLines)
		view.Files = append(view.Files, item)
	}
	return view, nil
}

func sourceOf(value model.Attribution) (key, label, kind string) {
	kind = string(value.Author)
	switch value.Author {
	case model.AuthorAI:
		modelName := value.Model
		if modelName == "" {
			modelName = "unknown"
		}
		key = kind + "\x00" + value.Agent + "\x00" + modelName
		label = value.Agent + "/" + modelName
	case model.AuthorHuman:
		key = kind
		label = "human/default"
	case model.AuthorUntracked:
		key = kind
		label = "untracked"
	default:
		key = kind
		label = kind
	}
	return key, label, kind
}

func assignTones(values map[string]*sourceCount) map[string]string {
	ordered := sortedSourceCounts(values)
	result := map[string]string{}
	for _, value := range ordered {
		switch value.kind {
		case string(model.AuthorHuman):
			result[value.key] = "tone-human"
		case string(model.AuthorUntracked):
			result[value.key] = "tone-untracked"
		default:
			result[value.key] = aiTone(value.key)
		}
	}
	return result
}

func aiTone(key string) string {
	parts := strings.Split(key, "\x00")
	if len(parts) > 1 {
		switch parts[1] {
		case "droid", "factory":
			return "tone-orange"
		case "claude":
			return "tone-coral"
		case "codex":
			return "tone-green"
		case "gemini":
			return "tone-purple"
		case "copilot":
			return "tone-indigo"
		case "vscode", "windsurf":
			return "tone-cyan"
		case "cursor":
			return "tone-yellow"
		case "grok":
			return "tone-pink"
		}
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return palette[int(hash.Sum32())%len(palette)]
}

func makeSourceViews(values map[string]*sourceCount, tones map[string]string, total int) []sourceView {
	ordered := sortedSourceCounts(values)
	result := make([]sourceView, 0, len(ordered))
	for _, value := range ordered {
		result = append(result, sourceView{
			Label:   value.label,
			Kind:    value.kind,
			Tone:    tones[value.key],
			Lines:   value.lines,
			Percent: percent(value.lines, total),
		})
	}
	return result
}

func sortedSourceCounts(values map[string]*sourceCount) []*sourceCount {
	result := make([]*sourceCount, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := sourceRank(result[i].kind), sourceRank(result[j].kind)
		if left != right {
			return left < right
		}
		if result[i].lines != result[j].lines {
			return result[i].lines > result[j].lines
		}
		if result[i].label != result[j].label {
			return result[i].label < result[j].label
		}
		return result[i].key < result[j].key
	})
	return result
}

func sourceRank(kind string) int {
	switch kind {
	case string(model.AuthorHuman):
		return 0
	case string(model.AuthorAI):
		return 1
	case string(model.AuthorUntracked):
		return 2
	default:
		return 3
	}
}

func percent(value, total int) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(value)*1000/float64(total)) / 10
}

func shortID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
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

var reportTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">
  <title>git-byline dashboard - {{.CommitShort}}</title>
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
      --untracked: #94a3b8;
      --orange: #f97316;
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
      position: sticky;
      top: 0;
      z-index: 10;
      display: flex;
      align-items: center;
      gap: 1rem;
      min-height: 5.5rem;
      padding: 1rem 2rem;
      border-bottom: 1px solid rgba(38, 57, 82, 0.8);
      background: rgba(5, 11, 20, 0.9);
      backdrop-filter: blur(18px);
    }
    .traffic { display: flex; gap: 0.55rem; }
    .traffic span { width: 0.78rem; height: 0.78rem; border-radius: 50%; }
    .traffic span:nth-child(1) { background: #ff5f57; }
    .traffic span:nth-child(2) { background: #febc2e; }
    .traffic span:nth-child(3) { background: #28c840; }
    h1, h2, h3, p { margin: 0; }
    h1 { font-size: clamp(1.35rem, 2.2vw, 2rem); letter-spacing: -0.03em; }
    h2 { font-size: 1.35rem; }
    h3 { font-size: 1rem; }
    .subtitle { color: var(--muted); margin-top: 0.3rem; }
    .commit {
      margin-left: auto;
      padding: 0.65rem 1rem;
      border: 1px solid #334764;
      border-radius: 999px;
      color: #dce7f5;
      background: #17243a;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.8rem;
    }
    .commit::before { content: ""; display: inline-block; width: 0.65rem; height: 0.65rem; margin-right: 0.65rem; border-radius: 50%; background: var(--green); }
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
    .overview { display: grid; grid-template-columns: minmax(18rem, 0.75fr) minmax(30rem, 1.25fr); gap: 1rem; margin-top: 1rem; }
    .panel { padding: 1.5rem; }
    .donut-wrap { display: grid; grid-template-columns: 13rem 1fr; align-items: center; gap: 1rem; margin-top: 1.25rem; }
    .donut { width: 13rem; height: 13rem; transform: rotate(-90deg); }
    .donut circle { fill: none; stroke-width: 10; }
    .donut .track { stroke: #19283d; }
    .donut .human { stroke: var(--human); }
    .donut .untracked { stroke: var(--untracked); }
    .donut .ai { stroke: #c084fc; stroke-linecap: round; }
    .donut-label { position: absolute; text-align: center; pointer-events: none; }
    .donut-label strong { display: block; font-size: 2.1rem; }
    .donut-label span { color: var(--muted); }
    .donut-box { position: relative; display: grid; place-items: center; }
    .legend { display: grid; gap: 0.8rem; }
    .legend-row, .source-head, .health-row { display: flex; align-items: center; gap: 0.65rem; }
    .legend-row strong, .source-head strong { margin-left: auto; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.82rem; }
    .dot { width: 0.65rem; height: 0.65rem; border-radius: 50%; background: currentColor; flex: 0 0 auto; }
    .source-list { display: grid; gap: 1rem; margin-top: 1.3rem; }
    .source-head { margin-bottom: 0.45rem; }
    .source-head span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .bar { display: block; width: 100%; height: 0.65rem; }
    .bar .track { fill: #19283d; }
    .bar .fill { fill: currentColor; }
    .health { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.8rem 1.5rem; margin-top: 1.2rem; }
    .health-row { min-height: 2.8rem; padding: 0 0.85rem; border: 1px solid #24364f; border-radius: 0.8rem; background: #101e31; }
    .health-row::before { content: "✓"; display: grid; place-items: center; width: 1.25rem; height: 1.25rem; border-radius: 50%; color: var(--green); background: rgba(34, 197, 94, 0.15); font-weight: 800; }
    .health-row code { margin-left: auto; color: var(--muted); }
    .warning { margin-top: 1rem; padding: 0.8rem 1rem; border: 1px solid rgba(245, 158, 11, 0.45); border-radius: 0.8rem; color: #fcd34d; background: rgba(245, 158, 11, 0.08); }
    .files-title { display: flex; align-items: center; gap: 1rem; margin: 1.5rem 0 1rem; }
    select {
      min-width: min(32rem, 60vw);
      padding: 0.7rem 2.5rem 0.7rem 0.85rem;
      color: var(--text);
      border: 1px solid var(--border);
      border-radius: 0.75rem;
      background: var(--surface-strong);
      font: inherit;
    }
    .file-view { display: none; grid-template-columns: 19rem minmax(0, 1fr); gap: 1rem; }
    .file-view.active { display: grid; }
    .file-summary { padding: 1.25rem; align-self: start; position: sticky; top: 7rem; }
    .file-summary code { color: var(--muted); word-break: break-all; }
    .file-summary .source-list { margin-top: 1.5rem; }
    .code-card { overflow: hidden; }
    .code-head { display: flex; align-items: center; gap: 1rem; min-height: 3.2rem; padding: 0 1rem; border-bottom: 1px solid var(--border); background: #101c2d; }
    .code-head code { color: #9fb0c5; }
    .code-head span { margin-left: auto; color: var(--faint); font-size: 0.8rem; }
    .code-scroll { overflow-x: auto; }
    table { width: 100%; border-collapse: collapse; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.82rem; }
    tr:nth-child(even) { background: rgba(255, 255, 255, 0.012); }
    td { height: 1.55rem; vertical-align: top; }
    .line-source { width: 0.28rem; background: currentColor; }
    .line-number { width: 4rem; padding: 0.22rem 0.85rem; color: #536781; text-align: right; user-select: none; }
    .line-code { min-width: 28rem; padding: 0.22rem 0.5rem; color: #d6e2f0; white-space: pre; }
    .line-label { width: 13rem; padding: 0.22rem 0.8rem; color: currentColor; white-space: nowrap; }
    footer { display: flex; justify-content: space-between; gap: 1rem; padding-top: 1.5rem; color: var(--faint); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.78rem; }
    .tone-human { color: #60a5fa; }
    .tone-untracked { color: #94a3b8; }
    .tone-orange { color: #f97316; }
    .tone-coral { color: #e98163; }
    .tone-green { color: #10b981; }
    .tone-purple { color: #a78bfa; }
    .tone-cyan { color: #22d3ee; }
    .tone-pink { color: #f472b6; }
    .tone-yellow { color: #facc15; }
    .tone-indigo { color: #818cf8; }
    @media (max-width: 1000px) {
      .kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .overview, .file-view { grid-template-columns: 1fr; }
      .file-summary { position: static; }
    }
    @media (max-width: 640px) {
      header { padding: 0.85rem 1rem; }
      .traffic, .commit { display: none; }
      main { width: min(100% - 1rem, 1500px); }
      .kpis { grid-template-columns: 1fr 1fr; gap: 0.5rem; }
      .overview { grid-template-columns: 1fr; }
      .panel { padding: 1rem; }
      .donut-wrap { grid-template-columns: 1fr; }
      .health { grid-template-columns: 1fr; }
      .files-title { align-items: stretch; flex-direction: column; }
      select { width: 100%; min-width: 0; }
    }
    @media print {
      header { position: static; }
      .file-view { display: grid; break-before: page; }
      .file-summary { position: static; }
      select { display: none; }
    }
  </style>
</head>
<body>
  <header>
    <div class="traffic" aria-hidden="true"><span></span><span></span><span></span></div>
    <div>
      <h1>git-byline attribution dashboard</h1>
      <p class="subtitle">Local evidence generated from Git notes at HEAD</p>
    </div>
    <div class="commit">commit {{.CommitShort}}</div>
  </header>
  <main>
    <section class="kpis" aria-label="Attribution summary">
      <div class="card kpi"><strong>{{.TotalLines}}</strong><span>attributed lines</span></div>
      <div class="card kpi"><strong class="accent">{{printf "%.1f" .AIPercent}}%</strong><span>AI observed</span></div>
      <div class="card kpi"><strong>{{.FileCount}}</strong><span>files</span></div>
      <div class="card kpi"><strong>{{.ModelCount}}</strong><span>AI sources</span></div>
      <div class="card kpi"><strong class="healthy">{{.Status.PendingCheckpoints}}</strong><span>pending checkpoints</span></div>
    </section>

    <section class="overview">
      <div class="card panel">
        <h2>Human vs AI</h2>
        <div class="donut-wrap">
          <div class="donut-box">
            <svg class="donut" viewBox="0 0 42 42" role="img" aria-label="{{printf "%.1f" .AIPercent}} percent AI observed">
              <circle class="track" cx="21" cy="21" r="15.9155"></circle>
              <circle class="human" cx="21" cy="21" r="15.9155" pathLength="100" stroke-dasharray="{{printf "%.1f" .HumanPercent}} {{printf "%.1f" .HumanGap}}"></circle>
              <circle class="untracked" cx="21" cy="21" r="15.9155" pathLength="100" stroke-dasharray="{{printf "%.1f" .UntrackedPercent}} {{printf "%.1f" .UntrackedGap}}" stroke-dashoffset="{{printf "%.1f" .UntrackedOffset}}"></circle>
              <circle class="ai" cx="21" cy="21" r="15.9155" pathLength="100" stroke-dasharray="{{printf "%.1f" .AIPercent}} {{printf "%.1f" .AIGap}}" stroke-dashoffset="{{printf "%.1f" .AIOffset}}"></circle>
            </svg>
            <div class="donut-label"><strong>{{printf "%.1f" .AIPercent}}%</strong><span>AI observed</span></div>
          </div>
          <div class="legend">
            <div class="legend-row tone-purple"><span class="dot"></span><span>AI observed</span><strong>{{.AILines}}</strong></div>
            <div class="legend-row tone-human"><span class="dot"></span><span>human/default</span><strong>{{.HumanLines}}</strong></div>
            <div class="legend-row tone-untracked"><span class="dot"></span><span>untracked</span><strong>{{.UntrackedLines}}</strong></div>
          </div>
        </div>
      </div>
      <div class="card panel">
        <h2>Contribution sources</h2>
        <div class="source-list">
          {{range .Sources}}
          <div class="{{.Tone}}">
            <div class="source-head"><span>{{.Label}}</span><strong>{{.Lines}} lines / {{printf "%.1f" .Percent}}%</strong></div>
            <svg class="bar" viewBox="0 0 100 8" preserveAspectRatio="none" aria-hidden="true"><rect class="track" width="100" height="8" rx="4"></rect><rect class="fill" width="{{printf "%.1f" .Percent}}" height="8" rx="4"></rect></svg>
          </div>
          {{end}}
        </div>
      </div>
    </section>

    <section class="card panel">
      <h2>Evidence health</h2>
      <div class="health">
        <div class="health-row"><span>HEAD annotated</span><code>{{if eq .Status.Head .Status.LastAnnotatedCommit}}yes{{else}}no{{end}}</code></div>
        <div class="health-row"><span>Pending checkpoints</span><code>{{.Status.PendingCheckpoints}}</code></div>
        <div class="health-row"><span>Pending files</span><code>{{.Status.PendingFiles}}</code></div>
        <div class="health-row"><span>Retained snapshots</span><code>{{.Status.RetainedSnapshots}}</code></div>
      </div>
      {{range .Warnings}}<div class="warning">{{.}}</div>{{end}}
    </section>

    <div class="files-title">
      <h2>Line provenance</h2>
      <select id="file-picker" aria-label="Choose attributed file">
        {{range .Files}}<option value="{{.ID}}">{{.Path}} - {{.TotalLines}} lines</option>{{end}}
      </select>
    </div>

    {{range .Files}}
    <section class="file-view{{if .Active}} active{{end}}" id="{{.ID}}" data-file-view>
      <aside class="card file-summary">
        <h3>{{.Path}}</h3>
        <p class="subtitle">{{.TotalLines}} lines / blob <code>{{.BlobShort}}</code></p>
        <div class="source-list">
          {{range .Sources}}
          <div class="{{.Tone}}">
            <div class="source-head"><span>{{.Label}}</span><strong>{{.Lines}} / {{printf "%.1f" .Percent}}%</strong></div>
            <svg class="bar" viewBox="0 0 100 8" preserveAspectRatio="none" aria-hidden="true"><rect class="track" width="100" height="8" rx="4"></rect><rect class="fill" width="{{printf "%.1f" .Percent}}" height="8" rx="4"></rect></svg>
          </div>
          {{end}}
        </div>
        {{range .Warnings}}<div class="warning">{{.}}</div>{{end}}
      </aside>
      <div class="card code-card">
        <div class="code-head"><code>{{.Path}}</code><span>human / agent / model</span></div>
        <div class="code-scroll">
          <table aria-label="Line attribution for {{.Path}}">
            <tbody>
              {{range .Lines}}
              <tr>
                <td class="line-source {{.Tone}}"></td>
                <td class="line-number">{{.Number}}</td>
                <td class="line-code">{{.Content}}</td>
                <td class="line-label {{.Tone}}">{{if .ShowLabel}}{{.Source}}{{end}}</td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </div>
    </section>
    {{end}}

    <footer><span>self-contained HTML / no network / no telemetry</span><span>{{.FileCount}} attributed files / commit {{.CommitShort}}</span></footer>
  </main>
  <script>
    (() => {
      const picker = document.getElementById("file-picker");
      const views = Array.from(document.querySelectorAll("[data-file-view]"));
      const show = (id) => {
        const selected = views.some((view) => view.id === id) ? id : views[0]?.id;
        views.forEach((view) => view.classList.toggle("active", view.id === selected));
        if (selected) {
          picker.value = selected;
          history.replaceState(null, "", "#" + selected);
        }
      };
      picker.addEventListener("change", () => show(picker.value));
      window.addEventListener("hashchange", () => show(location.hash.slice(1)));
      show(location.hash.slice(1));
    })();
  </script>
</body>
</html>
`))
