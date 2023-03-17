package configs

import (
	_ "embed"

	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
)

var (
	//go:embed default.yaml
	defaultConfig []byte
	//go:embed english_teacher.yaml
	englishTeacherConfig []byte
	//go:embed nyao.yaml
	nyaoConfig []byte
	//go:embed english_teaching_system.yaml
	englishTeachingSystemConfig []byte
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
	configYaml := defaultConfig

	switch character {
	case "english-teacher":
		configYaml = englishTeacherConfig
	}

	return load(configYaml)
}

func LoadNyao() (*Config, *Config, error) {
	nyao, err := load(nyaoConfig)
	if err != nil {
		klog.Infof("Failed to load nyao")
		return nil, nil, err
	}

	system, err := load(englishTeachingSystemConfig)
	if err != nil {
		klog.Infof("Failed to load nyao system")
		return nil, nil, err
	}

	return nyao, system, nil
}

func load(configYaml []byte) (config *Config, err error) {
	config = &Config{}
	err = yaml.Unmarshal(configYaml, config)
	return
}
