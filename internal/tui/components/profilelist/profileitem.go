// Package profilelist renders the AWS shared-config profile picker and
// loads profiles from ~/.aws/credentials and ~/.aws/config.
package profilelist

import (
	"context"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/goccy/go-yaml"

	"github.com/LinPr/lazys3/internal/tui/components/preview"
)

type Profile struct {
	name        string
	EndpointURL string
	config      *config.SharedConfig
}

func (p Profile) Title() string       { return p.name }
func (p Profile) Description() string { return p.EndpointURL }
func (p Profile) FilterValue() string { return p.name }

func (p Profile) GetPreviewContent() string {

	y, _ := yaml.Marshal(p.config)
	return string(y)
}

// GetPreviewRequest returns nil because profiles do not need a live S3
// fetch — their preview content is the YAML dump from the shared config,
// produced synchronously by GetPreviewContent. The preview component
// checks for nil and skips the async fetch path.
func (p Profile) GetPreviewRequest() *preview.PreviewRequest {
	return nil
}

type ReadAwsConfigResult struct {
	Profiles []Profile
	Err      error
}

func ReadAwsConfigProfileListCmd() tea.Cmd {
	return func() tea.Msg {
		configs, err := LoadAwsConfig()
		if err != nil {
			return ReadAwsConfigResult{Err: err}
		}
		var profiles []Profile
		for _, config := range configs {
			profiles = append(profiles, Profile{
				name:        config.Profile,
				EndpointURL: config.BaseEndpoint,
				config:      &config,
			})
		}

		return ReadAwsConfigResult{Profiles: profiles}
	}
}

func LoadAwsConfig() ([]config.SharedConfig, error) {
	conf, err := loadIniFiles(DefaultSharedConfigFiles)
	if err != nil {
		return nil, err
	}

	sections := conf.List()
	configs := make([]config.SharedConfig, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimPrefix(section, "profile ")
		log.Println("section:", section)
		sharedConf, err := config.LoadSharedConfigProfile(context.Background(), section)
		if err != nil {
			log.Println("load shared config profile failed:", err)
		}
		configs = append(configs, sharedConf)
	}

	return configs, nil
}
