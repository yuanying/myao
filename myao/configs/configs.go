package configs

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

var (
	//go:embed default.yaml
	defaultConfig []byte
)

type Config struct {
	Name        string  `yaml:"name"`
	SystemText  string  `yaml:"systemText"`
	InitText    string  `yaml:"initText"`
	ErrorText   string  `yaml:"errorText"`
	SummaryText string  `yaml:"summaryText"`
	Temperature float32 `yaml:"temperature"`
}

func Load(character string) (*Config, error) {
	config := Config{}
	err := yaml.Unmarshal(defaultConfig, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
