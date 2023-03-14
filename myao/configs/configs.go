package configs

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

var (
	//go:embed default.yaml
	defaultConfig []byte
	//go:embed english_teacher.yaml
	englishTeacherConfig []byte
)

type Message struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

type Config struct {
	Name        string  `yaml:"name"`
	SystemText  string  `yaml:"systemText"`
	InitText    string  `yaml:"initText"`
	ErrorText   string  `yaml:"errorText"`
	SummaryText string  `yaml:"summaryText"`
	Temperature float32 `yaml:"temperature"`
	TextFormat  string  `yaml:"textFormat"`

	InitConversations []Message `yaml:"initConversations"`
}

func Load(character string) (*Config, error) {
	config := Config{}
	configYaml := defaultConfig

	switch character {
	case "english-teacher":
		configYaml = englishTeacherConfig
	}

	err := yaml.Unmarshal(configYaml, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
