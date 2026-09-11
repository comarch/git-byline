// Package disclosure renders machine-readable AI content disclosure input.
package disclosure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/comarch/git-byline/internal/report"
)

const (
	NativeVersion        = 1
	CycloneDXVersion     = "1.6"
	SPDXVersion          = "3.0.1"
	AIShareDefinition    = "AI share is AI plus human-override lines divided by total lines; zero when total lines are zero."
	SPDXContext          = "https://spdx.org/rdf/3.0.1/spdx-context.jsonld"
	spdxDocumentID       = "https://git-byline.local/spdx/document"
	spdxPackageID        = "https://git-byline.local/spdx/package"
	spdxToolID           = "https://git-byline.local/spdx/tool"
	spdxCreationDateTime = "2006-01-02T15:04:05Z"
)

var (
	errDisclosureNegativeTotals = errors.New("disclosure aggregate contains negative totals")
	errDisclosureTotalsOverflow = errors.New("disclosure totals overflow while summing class counts")
	errDisclosureTotalsMismatch = errors.New("disclosure totals do not match class counts")
)

// Render converts an aggregate into one of the supported disclosure formats.
func Render(format string, aggregate report.Aggregate, generatedAt time.Time, toolVersion string) ([]byte, error) {
	if err := validateAggregate(aggregate); err != nil {
		return nil, fmt.Errorf("validate disclosure aggregate: %w", err)
	}
	switch format {
	case "json":
		return renderNative(aggregate, generatedAt, toolVersion)
	case "cyclonedx":
		return renderCycloneDX(aggregate, generatedAt, toolVersion)
	case "spdx":
		return renderSPDX(aggregate, generatedAt, toolVersion)
	default:
		return nil, fmt.Errorf("unsupported disclosure format %q", format)
	}
}

type nativeDocument struct {
	Version           int                  `json:"version"`
	Format            string               `json:"format"`
	ReportVersion     int                  `json:"report_version"`
	GeneratedAt       string               `json:"generated_at"`
	ToolVersion       string               `json:"tool_version"`
	AIShareDefinition string               `json:"ai_share_definition"`
	Specifications    nativeSpecifications `json:"specifications"`
	Range             nativeRange          `json:"range"`
	CommitCounts      nativeCommitCounts   `json:"commit_counts"`
	Totals            nativeTotals         `json:"totals"`
	Agents            []nativeAgent        `json:"agents"`
	Files             []nativeFile         `json:"files"`
	Sessions          []nativeSession      `json:"sessions"`
	Commits           []nativeCommit       `json:"commits"`
	Warnings          []string             `json:"warnings,omitempty"`
}

type nativeSpecifications struct {
	CycloneDXJSON string `json:"cyclonedx_json"`
	SPDXJSONLD    string `json:"spdx_json_ld"`
}

type nativeRange struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

type nativeCommitCounts struct {
	Total     int `json:"total"`
	Annotated int `json:"annotated"`
}

type nativeTotals struct {
	Human         int     `json:"human"`
	AI            int     `json:"ai"`
	Untracked     int     `json:"untracked"`
	HumanOverride int     `json:"human_override"`
	Lines         int     `json:"lines"`
	AIShare       float64 `json:"ai_share_percent"`
}

type nativeAgent struct {
	Agent  string        `json:"agent"`
	Totals nativeTotals  `json:"totals"`
	Models []nativeModel `json:"models"`
}

type nativeModel struct {
	Model  string       `json:"model"`
	Totals nativeTotals `json:"totals"`
}

type nativeFile struct {
	Path   string       `json:"path"`
	Totals nativeTotals `json:"totals"`
}

type nativeSession struct {
	Session string       `json:"session"`
	Agent   string       `json:"agent"`
	Model   string       `json:"model"`
	Totals  nativeTotals `json:"totals"`
}

type nativeCommit struct {
	Commit    string       `json:"commit"`
	Timestamp string       `json:"timestamp"`
	Totals    nativeTotals `json:"totals"`
}

func renderNative(aggregate report.Aggregate, generatedAt time.Time, toolVersion string) ([]byte, error) {
	document := nativeDocument{
		Version:           NativeVersion,
		Format:            "git-byline-disclosure",
		ReportVersion:     aggregate.Version,
		GeneratedAt:       generatedAt.UTC().Format(time.RFC3339Nano),
		ToolVersion:       toolVersion,
		AIShareDefinition: AIShareDefinition,
		Specifications: nativeSpecifications{
			CycloneDXJSON: CycloneDXVersion,
			SPDXJSONLD:    SPDXVersion,
		},
		Range: nativeRange{
			From:  aggregate.From,
			To:    aggregate.To,
			Label: rangeLabel(aggregate),
		},
		CommitCounts: nativeCommitCounts{
			Total:     aggregate.Commits.Total,
			Annotated: aggregate.Commits.Annotated,
		},
		Totals:   nativeTotalsFor(aggregate.Totals),
		Agents:   make([]nativeAgent, 0, len(aggregate.Agents)),
		Files:    make([]nativeFile, 0, len(aggregate.Files)),
		Sessions: make([]nativeSession, 0, len(aggregate.Sessions)),
		Commits:  make([]nativeCommit, 0, len(aggregate.Commit)),
		Warnings: append([]string(nil), aggregate.Warnings...),
	}
	for _, value := range aggregate.Agents {
		agent := nativeAgent{
			Agent:  value.Agent,
			Totals: nativeTotalsFor(value.Totals),
			Models: make([]nativeModel, 0, len(value.Models)),
		}
		for _, model := range value.Models {
			agent.Models = append(agent.Models, nativeModel{
				Model:  model.Model,
				Totals: nativeTotalsFor(model.Totals),
			})
		}
		document.Agents = append(document.Agents, agent)
	}
	for _, value := range aggregate.Files {
		document.Files = append(document.Files, nativeFile{
			Path:   value.Path,
			Totals: nativeTotalsFor(value.Totals),
		})
	}
	for _, value := range aggregate.Sessions {
		document.Sessions = append(document.Sessions, nativeSession{
			Session: value.Session,
			Agent:   value.Agent,
			Model:   value.Model,
			Totals:  nativeTotalsFor(value.Totals),
		})
	}
	for _, value := range aggregate.Commit {
		document.Commits = append(document.Commits, nativeCommit{
			Commit:    value.Commit,
			Timestamp: value.Timestamp,
			Totals:    nativeTotalsFor(value.Totals),
		})
	}
	return marshalDocument(document)
}

func nativeTotalsFor(value report.Totals) nativeTotals {
	return nativeTotals{
		Human:         value.Human,
		AI:            value.AI,
		Untracked:     value.Untracked,
		HumanOverride: value.HumanOverride,
		Lines:         value.Lines,
		AIShare:       aiSharePercent(value),
	}
}

func aiSharePercent(value report.Totals) float64 {
	if value.Lines == 0 {
		return 0
	}
	share := (float64(value.AI) + float64(value.HumanOverride)) * 100 / float64(value.Lines)
	return math.Round(share*100) / 100
}

func rangeLabel(aggregate report.Aggregate) string {
	switch {
	case aggregate.From != "" && aggregate.To != "":
		return aggregate.From + ".." + aggregate.To
	case aggregate.From != "":
		return aggregate.From + "..HEAD"
	case aggregate.To != "":
		return aggregate.To
	default:
		return "HEAD"
	}
}

func marshalDocument(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode disclosure JSON: %w", err)
	}
	return append(data, '\n'), nil
}

type cyclonedxDocument struct {
	BOMFormat   string               `json:"bomFormat"`
	SpecVersion string               `json:"specVersion"`
	Version     int                  `json:"version"`
	Metadata    cyclonedxMetadata    `json:"metadata"`
	Components  []cyclonedxComponent `json:"components"`
}

type cyclonedxMetadata struct {
	Timestamp  string              `json:"timestamp"`
	Tools      cyclonedxTools      `json:"tools"`
	Properties []cyclonedxProperty `json:"properties"`
}

type cyclonedxTools struct {
	Components []cyclonedxTool `json:"components"`
}

type cyclonedxTool struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version"`
	BOMRef  string `json:"bom-ref"`
}

type cyclonedxComponent struct {
	Type       string              `json:"type"`
	Name       string              `json:"name"`
	BOMRef     string              `json:"bom-ref"`
	Properties []cyclonedxProperty `json:"properties"`
}

type cyclonedxProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func renderCycloneDX(aggregate report.Aggregate, generatedAt time.Time, toolVersion string) ([]byte, error) {
	document := cyclonedxDocument{
		BOMFormat:   "CycloneDX",
		SpecVersion: CycloneDXVersion,
		Version:     1,
		Metadata: cyclonedxMetadata{
			Timestamp: generatedAt.UTC().Format(time.RFC3339Nano),
			Tools: cyclonedxTools{
				Components: []cyclonedxTool{{
					Type:    "application",
					Name:    "git-byline",
					Version: toolVersion,
					BOMRef:  "git-byline",
				}},
			},
			Properties: cyclonedxAggregateProperties(aggregate, toolVersion),
		},
		Components: make([]cyclonedxComponent, 0, len(aggregate.Files)),
	}
	for _, value := range aggregate.Files {
		document.Components = append(document.Components, cyclonedxComponent{
			Type:       "file",
			Name:       value.Path,
			BOMRef:     "git-byline:file:" + value.Path,
			Properties: cyclonedxTotalsProperties(value.Totals),
		})
	}
	return marshalDocument(document)
}

func cyclonedxAggregateProperties(aggregate report.Aggregate, toolVersion string) []cyclonedxProperty {
	properties := []cyclonedxProperty{
		{Name: "git-byline:range", Value: rangeLabel(aggregate)},
		{Name: "git-byline:from", Value: aggregate.From},
		{Name: "git-byline:to", Value: aggregate.To},
		{Name: "git-byline:report-version", Value: strconv.Itoa(aggregate.Version)},
		{Name: "git-byline:commit-total", Value: strconv.Itoa(aggregate.Commits.Total)},
		{Name: "git-byline:commit-annotated", Value: strconv.Itoa(aggregate.Commits.Annotated)},
		{Name: "git-byline:tool-version", Value: toolVersion},
		{Name: "git-byline:cyclonedx-version", Value: CycloneDXVersion},
		{Name: "git-byline:spdx-version", Value: SPDXVersion},
		{Name: "git-byline:ai-share-definition", Value: AIShareDefinition},
	}
	properties = append(properties, cyclonedxTotalsProperties(aggregate.Totals)...)
	for index, agent := range aggregate.Agents {
		prefix := "git-byline:agents[" + strconv.Itoa(index) + "]."
		properties = append(properties,
			cyclonedxProperty{Name: prefix + "agent", Value: agent.Agent},
			cyclonedxProperty{Name: prefix + "lines", Value: strconv.Itoa(agent.Lines)},
			cyclonedxProperty{Name: prefix + "human", Value: strconv.Itoa(agent.Human)},
			cyclonedxProperty{Name: prefix + "ai", Value: strconv.Itoa(agent.AI)},
			cyclonedxProperty{Name: prefix + "human-override", Value: strconv.Itoa(agent.HumanOverride)},
			cyclonedxProperty{Name: prefix + "untracked", Value: strconv.Itoa(agent.Untracked)},
		)
		for modelIndex, model := range agent.Models {
			modelPrefix := prefix + "models[" + strconv.Itoa(modelIndex) + "]."
			properties = append(properties,
				cyclonedxProperty{Name: modelPrefix + "model", Value: model.Model},
				cyclonedxProperty{Name: modelPrefix + "lines", Value: strconv.Itoa(model.Lines)},
				cyclonedxProperty{Name: modelPrefix + "ai", Value: strconv.Itoa(model.AI)},
				cyclonedxProperty{Name: modelPrefix + "human-override", Value: strconv.Itoa(model.HumanOverride)},
			)
		}
	}
	for index, session := range aggregate.Sessions {
		prefix := "git-byline:sessions[" + strconv.Itoa(index) + "]."
		properties = append(properties,
			cyclonedxProperty{Name: prefix + "session", Value: session.Session},
			cyclonedxProperty{Name: prefix + "agent", Value: session.Agent},
			cyclonedxProperty{Name: prefix + "model", Value: session.Model},
			cyclonedxProperty{Name: prefix + "lines", Value: strconv.Itoa(session.Lines)},
		)
	}
	for index, commit := range aggregate.Commit {
		prefix := "git-byline:commits[" + strconv.Itoa(index) + "]."
		properties = append(properties,
			cyclonedxProperty{Name: prefix + "commit", Value: commit.Commit},
			cyclonedxProperty{Name: prefix + "timestamp", Value: commit.Timestamp},
			cyclonedxProperty{Name: prefix + "lines", Value: strconv.Itoa(commit.Lines)},
		)
	}
	for index, warning := range aggregate.Warnings {
		properties = append(properties, cyclonedxProperty{
			Name:  "git-byline:warnings[" + strconv.Itoa(index) + "]",
			Value: warning,
		})
	}
	return properties
}

func cyclonedxTotalsProperties(value report.Totals) []cyclonedxProperty {
	return []cyclonedxProperty{
		{Name: "git-byline:ai-share-definition", Value: AIShareDefinition},
		{Name: "git-byline:ai-share-percent", Value: formatShare(value)},
		{Name: "git-byline:lines", Value: strconv.Itoa(value.Lines)},
		{Name: "git-byline:human", Value: strconv.Itoa(value.Human)},
		{Name: "git-byline:ai", Value: strconv.Itoa(value.AI)},
		{Name: "git-byline:human-override", Value: strconv.Itoa(value.HumanOverride)},
		{Name: "git-byline:untracked", Value: strconv.Itoa(value.Untracked)},
	}
}

func formatShare(value report.Totals) string {
	return strconv.FormatFloat(aiSharePercent(value), 'f', 2, 64)
}

type spdxDocument struct {
	Context string        `json:"@context"`
	Graph   []spdxElement `json:"@graph"`
}

type spdxElement struct {
	SpdxID              string                `json:"spdxId,omitempty"`
	Type                string                `json:"type"`
	Name                string                `json:"name,omitempty"`
	Description         string                `json:"description,omitempty"`
	Summary             string                `json:"summary,omitempty"`
	Comment             string                `json:"comment,omitempty"`
	CreationInfo        *spdxCreationInfo     `json:"creationInfo,omitempty"`
	ProfileConformance  []string              `json:"profileConformance,omitempty"`
	Element             []string              `json:"element,omitempty"`
	RootElement         []string              `json:"rootElement,omitempty"`
	ReleaseTime         string                `json:"releaseTime,omitempty"`
	SuppliedBy          string                `json:"suppliedBy,omitempty"`
	DownloadLocation    string                `json:"software_downloadLocation,omitempty"`
	PackageVersion      string                `json:"software_packageVersion,omitempty"`
	PrimaryPurpose      string                `json:"software_primaryPurpose,omitempty"`
	InformationAboutApp string                `json:"ai_informationAboutApplication,omitempty"`
	TypeOfModel         []string              `json:"ai_typeOfModel,omitempty"`
	Metrics             []spdxDictionaryEntry `json:"ai_metric,omitempty"`
	From                string                `json:"from,omitempty"`
	RelationshipType    string                `json:"relationshipType,omitempty"`
	To                  []string              `json:"to,omitempty"`
}

type spdxCreationInfo struct {
	ID           string   `json:"@id,omitempty"`
	Type         string   `json:"type"`
	Created      string   `json:"created"`
	CreatedBy    []string `json:"createdBy"`
	CreatedUsing []string `json:"createdUsing,omitempty"`
	SpecVersion  string   `json:"specVersion"`
}

type spdxDictionaryEntry struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

func renderSPDX(aggregate report.Aggregate, generatedAt time.Time, toolVersion string) ([]byte, error) {
	created := generatedAt.UTC().Format(spdxCreationDateTime)
	creationInfo := func(id string) *spdxCreationInfo {
		return &spdxCreationInfo{
			ID:           id,
			Type:         "CreationInfo",
			Created:      created,
			CreatedBy:    []string{"SpdxOrganization"},
			CreatedUsing: []string{spdxToolID},
			SpecVersion:  SPDXVersion,
		}
	}
	tool := spdxElement{
		SpdxID:       spdxToolID,
		Type:         "Tool",
		Name:         "git-byline",
		Comment:      "git-byline version " + toolVersion,
		CreationInfo: creationInfo("https://git-byline.local/spdx/creation/tool"),
	}
	packageElement := spdxElement{
		SpdxID:             spdxPackageID,
		Type:               "ai_AIPackage",
		Name:               "git-byline AI disclosure",
		Description:        "Machine-readable AI content disclosure input generated from observed line provenance.",
		CreationInfo:       creationInfo("https://git-byline.local/spdx/creation/package"),
		ProfileConformance: nil,
		ReleaseTime:        created,
		SuppliedBy:         "SpdxOrganization",
		DownloadLocation:   "NOASSERTION",
		PackageVersion:     strconv.Itoa(aggregate.Version),
		PrimaryPurpose:     "source",
		InformationAboutApp: "Observed line provenance for " + rangeLabel(aggregate) +
			". " + AIShareDefinition,
		Metrics: spdxMetrics(aggregate, toolVersion),
	}
	elements := []spdxElement{
		{
			SpdxID:             spdxDocumentID,
			Type:               "SpdxDocument",
			Name:               "git-byline disclosure",
			Description:        "Machine-readable input for an AI content disclosure process.",
			CreationInfo:       creationInfo("https://git-byline.local/spdx/creation/document"),
			ProfileConformance: []string{"ai", "core", "software"},
			Element:            []string{spdxPackageID, spdxToolID},
			RootElement:        []string{spdxPackageID},
		},
		tool,
		packageElement,
	}
	for index, file := range aggregate.Files {
		fileID := spdxFileID(file.Path)
		elements = append(elements, spdxElement{
			SpdxID:         fileID,
			Type:           "software_File",
			Name:           file.Path,
			CreationInfo:   creationInfo("https://git-byline.local/spdx/creation/file/" + strconv.Itoa(index)),
			PrimaryPurpose: "source",
		})
		elements[0].Element = append(elements[0].Element, fileID)
		packageElement.Metrics = append(packageElement.Metrics,
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: "git-byline:files[" + strconv.Itoa(index) + "].path", Value: file.Path},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: "git-byline:files[" + strconv.Itoa(index) + "].ai-share-percent", Value: formatShare(file.Totals)},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: "git-byline:files[" + strconv.Itoa(index) + "].lines", Value: strconv.Itoa(file.Lines)},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: "git-byline:files[" + strconv.Itoa(index) + "].ai", Value: strconv.Itoa(file.AI)},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: "git-byline:files[" + strconv.Itoa(index) + "].human-override", Value: strconv.Itoa(file.HumanOverride)},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: "git-byline:files[" + strconv.Itoa(index) + "].human", Value: strconv.Itoa(file.Human)},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: "git-byline:files[" + strconv.Itoa(index) + "].untracked", Value: strconv.Itoa(file.Untracked)},
		)
	}
	elements[2] = packageElement
	for _, relationshipType := range []string{"hasDeclaredLicense", "hasConcludedLicense"} {
		relationshipID := "https://git-byline.local/spdx/" + relationshipType
		elements = append(elements, spdxElement{
			SpdxID:           relationshipID,
			Type:             "Relationship",
			CreationInfo:     creationInfo("https://git-byline.local/spdx/creation/" + relationshipType),
			From:             spdxPackageID,
			RelationshipType: relationshipType,
			To:               []string{"expandedlicensing_NoAssertionLicense"},
		})
		elements[0].Element = append(elements[0].Element, relationshipID)
	}
	return marshalDocument(spdxDocument{Context: SPDXContext, Graph: elements})
}

func spdxMetrics(aggregate report.Aggregate, toolVersion string) []spdxDictionaryEntry {
	metrics := []spdxDictionaryEntry{
		{Type: "DictionaryEntry", Key: "git-byline:range", Value: rangeLabel(aggregate)},
		{Type: "DictionaryEntry", Key: "git-byline:from", Value: aggregate.From},
		{Type: "DictionaryEntry", Key: "git-byline:to", Value: aggregate.To},
		{Type: "DictionaryEntry", Key: "git-byline:report-version", Value: strconv.Itoa(aggregate.Version)},
		{Type: "DictionaryEntry", Key: "git-byline:tool-version", Value: toolVersion},
		{Type: "DictionaryEntry", Key: "git-byline:commit-total", Value: strconv.Itoa(aggregate.Commits.Total)},
		{Type: "DictionaryEntry", Key: "git-byline:commit-annotated", Value: strconv.Itoa(aggregate.Commits.Annotated)},
		{Type: "DictionaryEntry", Key: "git-byline:ai-share-definition", Value: AIShareDefinition},
		{Type: "DictionaryEntry", Key: "git-byline:ai-share-percent", Value: formatShare(aggregate.Totals)},
		{Type: "DictionaryEntry", Key: "git-byline:lines", Value: strconv.Itoa(aggregate.Totals.Lines)},
		{Type: "DictionaryEntry", Key: "git-byline:human", Value: strconv.Itoa(aggregate.Totals.Human)},
		{Type: "DictionaryEntry", Key: "git-byline:ai", Value: strconv.Itoa(aggregate.Totals.AI)},
		{Type: "DictionaryEntry", Key: "git-byline:human-override", Value: strconv.Itoa(aggregate.Totals.HumanOverride)},
		{Type: "DictionaryEntry", Key: "git-byline:untracked", Value: strconv.Itoa(aggregate.Totals.Untracked)},
	}
	for index, agent := range aggregate.Agents {
		prefix := "git-byline:agents[" + strconv.Itoa(index) + "]."
		metrics = append(metrics,
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "agent", Value: agent.Agent},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "lines", Value: strconv.Itoa(agent.Lines)},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "ai", Value: strconv.Itoa(agent.AI)},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "human-override", Value: strconv.Itoa(agent.HumanOverride)},
		)
		for modelIndex, model := range agent.Models {
			modelPrefix := prefix + "models[" + strconv.Itoa(modelIndex) + "]."
			metrics = append(metrics,
				spdxDictionaryEntry{Type: "DictionaryEntry", Key: modelPrefix + "model", Value: model.Model},
				spdxDictionaryEntry{Type: "DictionaryEntry", Key: modelPrefix + "lines", Value: strconv.Itoa(model.Lines)},
				spdxDictionaryEntry{Type: "DictionaryEntry", Key: modelPrefix + "ai", Value: strconv.Itoa(model.AI)},
				spdxDictionaryEntry{Type: "DictionaryEntry", Key: modelPrefix + "human-override", Value: strconv.Itoa(model.HumanOverride)},
			)
		}
	}
	for index, session := range aggregate.Sessions {
		prefix := "git-byline:sessions[" + strconv.Itoa(index) + "]."
		metrics = append(metrics,
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "session", Value: session.Session},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "agent", Value: session.Agent},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "model", Value: session.Model},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "lines", Value: strconv.Itoa(session.Lines)},
		)
	}
	for index, commit := range aggregate.Commit {
		prefix := "git-byline:commits[" + strconv.Itoa(index) + "]."
		metrics = append(metrics,
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "commit", Value: commit.Commit},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "timestamp", Value: commit.Timestamp},
			spdxDictionaryEntry{Type: "DictionaryEntry", Key: prefix + "lines", Value: strconv.Itoa(commit.Lines)},
		)
	}
	for index, warning := range aggregate.Warnings {
		metrics = append(metrics, spdxDictionaryEntry{
			Type:  "DictionaryEntry",
			Key:   "git-byline:warnings[" + strconv.Itoa(index) + "]",
			Value: warning,
		})
	}
	return metrics
}

func spdxFileID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return "https://git-byline.local/spdx/file/" + hex.EncodeToString(hash[:])
}

func validateAggregate(aggregate report.Aggregate) error {
	if err := validateTotals("aggregate", aggregate.Totals); err != nil {
		return err
	}
	for _, agent := range aggregate.Agents {
		name := fmt.Sprintf("agent %q", agent.Agent)
		if err := validateTotals(name, agent.Totals); err != nil {
			return err
		}
		for _, model := range agent.Models {
			if err := validateTotals(
				fmt.Sprintf("%s model %q", name, model.Model),
				model.Totals,
			); err != nil {
				return err
			}
		}
	}
	for _, file := range aggregate.Files {
		if err := validateTotals(fmt.Sprintf("file %q", file.Path), file.Totals); err != nil {
			return err
		}
	}
	for _, session := range aggregate.Sessions {
		if err := validateTotals(
			fmt.Sprintf("session %q (agent %q, model %q)", session.Session, session.Agent, session.Model),
			session.Totals,
		); err != nil {
			return err
		}
	}
	for _, commit := range aggregate.Commit {
		if err := validateTotals(fmt.Sprintf("commit %q", commit.Commit), commit.Totals); err != nil {
			return err
		}
	}
	return nil
}

func validateTotals(name string, totals report.Totals) error {
	fields := []struct {
		name  string
		value int
	}{
		{name: "human", value: totals.Human},
		{name: "ai", value: totals.AI},
		{name: "untracked", value: totals.Untracked},
		{name: "human override", value: totals.HumanOverride},
		{name: "lines", value: totals.Lines},
	}
	for _, field := range fields {
		if field.value < 0 {
			return fmt.Errorf("%s: %w (%s count)", name, errDisclosureNegativeTotals, field.name)
		}
	}
	sum := 0
	for _, value := range []int{totals.Human, totals.AI, totals.Untracked, totals.HumanOverride} {
		maxInt := int(^uint(0) >> 1)
		if sum > maxInt-value {
			return fmt.Errorf("%s: %w", name, errDisclosureTotalsOverflow)
		}
		sum += value
	}
	if totals.Lines != sum {
		return fmt.Errorf("%s: %w (lines %d, class total %d)",
			name, errDisclosureTotalsMismatch, totals.Lines, sum)
	}
	return nil
}
