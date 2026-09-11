package ci

import (
	"embed"
	"fmt"
)

//go:embed templates/github.yml templates/gitlab.yml
var workflowTemplates embed.FS

func embeddedTemplate(provider Provider) ([]byte, error) {
	var name string
	switch provider {
	case ProviderGitHub:
		name = "templates/github.yml"
	case ProviderGitLab:
		name = "templates/gitlab.yml"
	default:
		return nil, fmt.Errorf("unsupported CI provider %q", provider)
	}
	data, err := workflowTemplates.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read embedded CI template: %w", err)
	}
	return data, nil
}
