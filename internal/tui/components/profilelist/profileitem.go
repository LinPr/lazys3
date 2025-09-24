package profilelist

import (
	"context"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/goccy/go-yaml"
)

type Profile struct {
	name        string
	EndpointUrl string
	config      *config.SharedConfig
}

func (p Profile) Title() string       { return p.name }
func (p Profile) Description() string { return p.EndpointUrl }
func (p Profile) FilterValue() string { return p.name }

func (b Profile) GetPreviewContent() string {

	y, _ := yaml.Marshal(b.config)
	return string(y)
	// return "fjdakfda"
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
				EndpointUrl: config.BaseEndpoint,
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
