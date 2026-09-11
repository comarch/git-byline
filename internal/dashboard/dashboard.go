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

// palette holds the agent tone classes. Every value is a declared palette
// token and clears 4.5:1 against the card background.
var palette = []string{
	"tone-cyan",
	"tone-magenta",
	"tone-cyan-deep",
	"tone-violet-light",
	"tone-blue",
	"tone-blue-light",
	"tone-red-light",
	"tone-magenta-light",
}

// Report contains all local data rendered into one HTML file.
type Report struct {
	Commit string
	Status provenance.StatusResult
	Files  []provenance.BlameResult
}

type pageView struct {
	Commit               string
	CommitShort          string
	Files                []fileView
	Sources              []sourceView
	TotalLines           int
	AILines              int
	HumanLines           int
	HumanOverrideLines   int
	UntrackedLines       int
	AIPercent            float64
	AIGap                float64
	HumanPercent         float64
	HumanGap             float64
	HumanOverridePercent float64
	HumanOverrideGap     float64
	UntrackedPercent     float64
	UntrackedGap         float64
	HumanOverrideOffset  float64
	UntrackedOffset      float64
	AIOffset             float64
	FileCount            int
	ModelCount           int
	Status               provenance.StatusResult
	Warnings             []string
}

type fileView struct {
	ID                   string
	Active               bool
	Path                 string
	BlobShort            string
	Lines                []lineView
	Sources              []sourceView
	TotalLines           int
	AILines              int
	HumanLines           int
	HumanOverrideLines   int
	UntrackedLines       int
	AIPercent            float64
	HumanOverridePercent float64
	Warnings             []string
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
			line.Attribution = dashboardAttribution(line.Attribution)
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
		case string(model.AuthorHumanOverride):
			view.HumanOverrideLines += source.Lines
		case string(model.AuthorUntracked):
			view.UntrackedLines += source.Lines
		}
	}
	view.AIPercent = percent(view.AILines, totalLines)
	view.HumanPercent = percent(view.HumanLines, totalLines)
	view.HumanOverridePercent = percent(view.HumanOverrideLines, totalLines)
	view.UntrackedPercent = percent(view.UntrackedLines, totalLines)
	view.AIGap = 100 - view.AIPercent
	view.HumanGap = 100 - view.HumanPercent
	view.HumanOverrideGap = 100 - view.HumanOverridePercent
	view.UntrackedGap = 100 - view.UntrackedPercent
	view.HumanOverrideOffset = -view.HumanPercent
	view.UntrackedOffset = -(view.HumanPercent + view.HumanOverridePercent)
	view.AIOffset = -(view.HumanPercent + view.HumanOverridePercent + view.UntrackedPercent)
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
			line.Attribution = dashboardAttribution(line.Attribution)
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
			case string(model.AuthorHumanOverride):
				item.HumanOverrideLines += source.Lines
			case string(model.AuthorUntracked):
				item.UntrackedLines += source.Lines
			}
		}
		item.AIPercent = percent(item.AILines, item.TotalLines)
		item.HumanOverridePercent = percent(item.HumanOverrideLines, item.TotalLines)
		view.Files = append(view.Files, item)
	}
	return view, nil
}

func dashboardAttribution(value model.Attribution) model.Attribution {
	if value.Author == "" {
		return model.Attribution{Author: model.AuthorUntracked}
	}
	return value
}

func sourceOf(value model.Attribution) (key, label, kind string) {
	value = dashboardAttribution(value)
	kind = string(value.Author)
	switch value.Author {
	case model.AuthorAI, model.AuthorHumanOverride:
		modelName := value.Model
		if modelName == "" {
			modelName = "unknown"
		}
		key = kind + "\x00" + value.Agent + "\x00" + modelName + "\x00" + value.Identity
		label = kind + ":"
		if value.Author == model.AuthorHumanOverride && value.Identity != "" {
			label += value.Identity + "/"
		}
		label += value.Agent + "/" + modelName
	case model.AuthorHuman:
		key = kind + "\x00" + value.Identity
		label = kind
		if value.Identity != "" {
			label += ":" + value.Identity
		}
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
		case string(model.AuthorHumanOverride):
			result[value.key] = "tone-human-override"
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
			return "tone-cyan"
		case "claude":
			return "tone-magenta-light"
		case "codex":
			return "tone-cyan-deep"
		case "gemini":
			return "tone-violet-light"
		case "copilot":
			return "tone-blue"
		case "vscode", "windsurf":
			return "tone-blue-light"
		case "cursor":
			return "tone-red-light"
		case "grok":
			return "tone-magenta"
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
	case string(model.AuthorHumanOverride):
		return 1
	case string(model.AuthorAI):
		return 2
	case string(model.AuthorUntracked):
		return 3
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
    /*
      Palette and type tokens for the report. Values are fixed: a tone is
      a declared token, never an ad hoc color, so a stray accent cannot
      slip in. The scale carries only cyan, blue, violet, magenta, red,
      and grey, which is why success reads as cyan and failure as red.
      Every foreground below clears 4.5:1 against the surface it sits on.
      See docs/DESIGN.md.
    */
    :root {
      color-scheme: dark;
      --cl-cyan: #00FFFF;
      --cl-cyan-700: #00AAAA;
      --cl-blue-100: #BFBFFF;
      --cl-blue-200: #8080FF;
      --cl-violet-100: #D8BFEF;
      --cl-violet-200: #B280DF;
      --cl-magenta: #FF009B;
      --cl-magenta-200: #FF80CD;
      --cl-red-200: #FF8080;
      --cl-red-300: #FF4040;
      --cl-black: #000000;
      --cl-black-600: #333333;
      --cl-black-700: #1A1A1A;
      --cl-grey-400: #BFBFBF;
      --cl-grey-500: #A6A6A6;
      --cl-grey-800: #404040;
      --cl-gradient-primary: linear-gradient(90deg, #00FFFF 0%, #6400BE 50%, #FF0000 100%);
      --cl-font: "Cera Pro", Inter, Arial, sans-serif;
      /* Monospace is outside the brand families and stays a system stack. */
      --cl-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;

      --bg: var(--cl-black);
      --surface: var(--cl-black-700);
      --surface-strong: var(--cl-black-600);
      --border: var(--cl-grey-800);
      --text: #FFFFFF;
      --muted: var(--cl-grey-400);
      --faint: var(--cl-grey-500);
      --ok: var(--cl-cyan);
      --alert: var(--cl-red-300);
      --human: var(--cl-violet-200);
      --human-override: var(--cl-magenta);
      --untracked: var(--cl-grey-500);
      --ai: var(--cl-cyan);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-width: 320px;
      color: var(--text);
      background:
        radial-gradient(circle at 85% 0%, rgba(0, 255, 255, 0.10), transparent 32rem),
        radial-gradient(circle at 5% 95%, rgba(100, 0, 190, 0.14), transparent 36rem),
        var(--bg);
      font-family: var(--cl-font);
      font-size: 1rem;
      line-height: 1.4;
      letter-spacing: -0.01em;
    }
    header {
      position: sticky;
      isolation: isolate;
      top: 0;
      z-index: 10;
      display: flex;
      align-items: center;
      gap: 1rem;
      min-height: 5.5rem;
      padding: 1rem 2rem;
      background: rgba(0, 0, 0, 0.92);
      backdrop-filter: blur(18px);
    }
    header::after {
      content: "";
      position: absolute;
      inset: auto 0 0 0;
      height: 3px;
      background: var(--cl-gradient-primary);
    }
    .traffic { display: flex; gap: 0.55rem; }
    .traffic span { width: 0.78rem; height: 0.78rem; border-radius: 50%; }
    .traffic span:nth-child(1) { background: #00FFFF; }
    .traffic span:nth-child(2) { background: #6400BE; }
    .traffic span:nth-child(3) { background: #FF0000; }
    h1, h2, h3, p { margin: 0; }
    h1 { font-size: clamp(1.5rem, 2.4vw, 2.5rem); font-weight: 800; line-height: 1.2; letter-spacing: -0.02em; }
    h2 { font-size: 1.25rem; font-weight: 700; letter-spacing: -0.02em; }
    h3 { font-size: 1rem; font-weight: 700; }
    .subtitle { color: var(--muted); margin-top: 0.3rem; }
    .commit {
      margin-left: auto;
      padding: 0.65rem 1rem;
      border: 1px solid var(--border);
      border-radius: 999px;
      color: var(--text);
      background: var(--surface-strong);
      font-family: var(--cl-mono);
      font-size: 0.8rem;
    }
    .commit::before { content: ""; display: inline-block; width: 0.65rem; height: 0.65rem; margin-right: 0.65rem; border-radius: 50%; background: var(--ok); }
    main { width: min(1500px, calc(100% - 2rem)); margin: 0 auto; padding: 1.5rem 0 3rem; }
    .card {
      border: 1px solid var(--border);
      border-radius: 1.15rem;
      background: var(--surface);
      box-shadow: 0 1rem 3rem rgba(0, 0, 0, 0.45);
    }
    .kpis { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 1rem; }
    .kpi { padding: 1.25rem 1.35rem; }
    .kpi strong { display: block; font-size: 2.5rem; font-weight: 800; line-height: 1.1; letter-spacing: -0.02em; }
    .kpi span { display: block; margin-top: 0.55rem; color: var(--muted); }
    .accent { color: var(--ai); }
    .healthy { color: var(--ok); }
    .overview { display: grid; grid-template-columns: minmax(18rem, 0.75fr) minmax(30rem, 1.25fr); gap: 1rem; margin-top: 1rem; }
    .panel { padding: 1.5rem; }
    .donut-wrap { display: grid; grid-template-columns: 13rem 1fr; align-items: center; gap: 1rem; margin-top: 1.25rem; }
    .donut { width: 13rem; height: 13rem; transform: rotate(-90deg); }
    .donut circle { fill: none; stroke-width: 10; }
    .donut .track { stroke: var(--cl-black-600); }
    .donut .human { stroke: var(--human); }
    .donut .human-override { stroke: var(--human-override); }
    .donut .untracked { stroke: var(--untracked); }
    .donut .ai { stroke: var(--ai); stroke-linecap: round; }
    .donut-label { position: absolute; text-align: center; pointer-events: none; }
    .donut-label strong { display: block; font-size: 2.5rem; font-weight: 800; letter-spacing: -0.02em; }
    .donut-label span { color: var(--muted); }
    .donut-box { position: relative; display: grid; place-items: center; }
    .legend { display: grid; grid-template-columns: minmax(0, 1fr); gap: 0.8rem; }
    .legend-row, .source-head, .health-row { display: flex; align-items: center; gap: 0.65rem; }
    .legend-row strong, .source-head strong { margin-left: auto; flex: 0 0 auto; font-family: var(--cl-mono); font-size: 0.82rem; }
    .dot { width: 0.65rem; height: 0.65rem; border-radius: 50%; background: currentColor; flex: 0 0 auto; }
    .source-list { display: grid; grid-template-columns: minmax(0, 1fr); gap: 1rem; margin-top: 1.3rem; }
    .source-head { margin-bottom: 0.45rem; }
    /* A nowrap flex item keeps its full min-content width unless min-width
       is cleared, which pushes long agent labels out of the card. */
    .legend-row span, .source-head span, .health-row span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .bar { display: block; width: 100%; height: 0.65rem; }
    .bar .track { fill: var(--cl-black-600); }
    .bar .fill { fill: currentColor; }
    .health { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.8rem 1.5rem; margin-top: 1.2rem; }
    .health-row { min-height: 2.75rem; padding: 0 0.85rem; border: 1px solid var(--border); border-radius: 0.8rem; background: var(--surface-strong); }
    .health-row::before { content: "✓"; display: grid; place-items: center; width: 1.25rem; height: 1.25rem; border-radius: 50%; color: var(--cl-black); background: var(--ok); font-weight: 800; }
    .health-row code { margin-left: auto; color: var(--muted); }
    .warning { margin-top: 1rem; padding: 0.8rem 1rem; border: 1px solid var(--alert); border-radius: 0.8rem; color: var(--alert); background: rgba(255, 0, 0, 0.08); }
    .files-title { display: flex; align-items: center; gap: 1rem; margin: 1.5rem 0 1rem; }
    select {
      min-width: min(32rem, 60vw);
      /* 44 px minimum touch target: implementation guardrail. */
      min-height: 44px;
      padding: 0.7rem 2.5rem 0.7rem 0.85rem;
      color: var(--text);
      border: 1px solid var(--border);
      border-radius: 0.75rem;
      background: var(--surface-strong);
      font: inherit;
    }
    :focus-visible { outline: 2px solid var(--cl-cyan); outline-offset: 2px; }
    .file-view { display: none; grid-template-columns: 19rem minmax(0, 1fr); gap: 1rem; }
    .file-view.active { display: grid; }
    .file-summary { padding: 1.25rem; align-self: start; position: sticky; top: 7rem; }
    .file-summary code { color: var(--muted); word-break: break-all; }
    .file-summary .source-list { margin-top: 1.5rem; }
    .code-card { overflow: hidden; }
    .code-head { display: flex; align-items: center; gap: 1rem; min-height: 3.2rem; padding: 0 1rem; border-bottom: 1px solid var(--border); background: var(--surface-strong); }
    .code-head code { color: var(--muted); }
    .code-head span { margin-left: auto; color: var(--faint); font-size: 0.8rem; }
    .code-scroll { overflow-x: auto; }
    table { width: 100%; border-collapse: collapse; font-family: var(--cl-mono); font-size: 0.82rem; }
    tr:nth-child(even) { background: rgba(255, 255, 255, 0.012); }
    td { height: 1.55rem; vertical-align: top; }
    .line-source { width: 0.28rem; background: currentColor; }
    .line-number { width: 4rem; padding: 0.22rem 0.85rem; color: var(--faint); text-align: right; user-select: none; }
    .line-code { min-width: 28rem; padding: 0.22rem 0.5rem; color: var(--text); white-space: pre; }
    .line-label { width: 13rem; padding: 0.22rem 0.8rem; color: currentColor; white-space: nowrap; }
    footer { display: flex; justify-content: space-between; gap: 1rem; padding-top: 1.5rem; color: var(--faint); font-family: var(--cl-mono); font-size: 0.78rem; }
    .tone-human { color: var(--human); }
    .tone-human-override { color: var(--human-override); }
    .tone-untracked { color: var(--untracked); }
    .tone-ai { color: var(--ai); }
    .tone-cyan { color: var(--cl-cyan); }
    .tone-cyan-deep { color: var(--cl-cyan-700); }
    .tone-blue { color: var(--cl-blue-200); }
    .tone-blue-light { color: var(--cl-blue-100); }
    .tone-violet-light { color: var(--cl-violet-100); }
    .tone-magenta { color: var(--cl-magenta); }
    .tone-magenta-light { color: var(--cl-magenta-200); }
    .tone-red-light { color: var(--cl-red-200); }
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
    @media (prefers-reduced-motion: reduce) {
      * { animation-duration: 0.01ms !important; animation-iteration-count: 1 !important; transition-duration: 0.01ms !important; }
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
        <h2>Attribution classes</h2>
        <div class="donut-wrap">
          <div class="donut-box">
            <svg class="donut" viewBox="0 0 42 42" role="img" aria-label="{{printf "%.1f" .AIPercent}} percent AI observed">
              <circle class="track" cx="21" cy="21" r="15.9155"></circle>
              <circle class="human" cx="21" cy="21" r="15.9155" pathLength="100" stroke-dasharray="{{printf "%.1f" .HumanPercent}} {{printf "%.1f" .HumanGap}}"></circle>
              <circle class="human-override" cx="21" cy="21" r="15.9155" pathLength="100" stroke-dasharray="{{printf "%.1f" .HumanOverridePercent}} {{printf "%.1f" .HumanOverrideGap}}" stroke-dashoffset="{{printf "%.1f" .HumanOverrideOffset}}"></circle>
              <circle class="untracked" cx="21" cy="21" r="15.9155" pathLength="100" stroke-dasharray="{{printf "%.1f" .UntrackedPercent}} {{printf "%.1f" .UntrackedGap}}" stroke-dashoffset="{{printf "%.1f" .UntrackedOffset}}"></circle>
              <circle class="ai" cx="21" cy="21" r="15.9155" pathLength="100" stroke-dasharray="{{printf "%.1f" .AIPercent}} {{printf "%.1f" .AIGap}}" stroke-dashoffset="{{printf "%.1f" .AIOffset}}"></circle>
            </svg>
            <div class="donut-label"><strong>{{printf "%.1f" .AIPercent}}%</strong><span>AI observed</span></div>
          </div>
          <div class="legend">
            <div class="legend-row tone-ai"><span class="dot"></span><span>AI observed</span><strong>{{.AILines}}</strong></div>
            <div class="legend-row tone-human"><span class="dot"></span><span>human</span><strong>{{.HumanLines}}</strong></div>
            <div class="legend-row tone-human-override"><span class="dot"></span><span>human override</span><strong>{{.HumanOverrideLines}}</strong></div>
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
        <div class="code-head"><code>{{.Path}}</code><span>class : author / agent / model</span></div>
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
      const show = (id, link) => {
        const selected = views.some((view) => view.id === id) ? id : views[0]?.id;
        views.forEach((view) => view.classList.toggle("active", view.id === selected));
        if (selected) {
          picker.value = selected;
          // Writing the fragment on load makes the browser jump to the
          // file section, so only an explicit choice updates the URL.
          if (link) {
            history.replaceState(null, "", "#" + selected);
          }
        }
      };
      picker.addEventListener("change", () => show(picker.value, true));
      window.addEventListener("hashchange", () => show(location.hash.slice(1), false));
      show(location.hash.slice(1), false);
    })();
  </script>
</body>
</html>
`))
