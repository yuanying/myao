package myao

import (
	_ "embed"
	"fmt"

	"github.com/sashabaranov/go-openai"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model"
	"github.com/yuanying/myao/model/configs"
)

var _ model.Model = (*Myao)(nil)

type Myao struct {
	model  *model.Shared
	Config *configs.Config
}

func New(opts *model.Opts) (*Myao, error) {
	openAIConfig := openai.DefaultConfig(opts.OpenAIAccessToken)
	if opts.OpenAIOrganizationID != "" {
		openAIConfig.OrgID = opts.OpenAIOrganizationID
	}
	openAI := openai.NewClientWithConfig(openAIConfig)

	config, err := configs.Load(opts.CharacterType)
	if err != nil {
		klog.Errorf("Failed to load config: %v", err)
		return nil, err
	}

	m := &Myao{
		model: &model.Shared{
			Config: config,
			OpenAI: openAI,
			Opts:   opts,
		},
		Config: config,
	}
	m.LoadSummary()
	for _, msg := range config.InitConversations {
		m.model.Remember(msg.Role, msg.Content, []string{})
	}

	return m, nil
}

func (m *Myao) Name() string {
	return m.model.Name
}

func (m *Myao) SaveSummary(summary string) {
	m.model.SaveSummary(summary)
}

func (m *Myao) LoadSummary() {
	m.model.LoadSummary()
}

func (m *Myao) Reset() (string, error) {
	return m.model.Reset()
}

func (m *Myao) FormatText(user, content string) string {
	return fmt.Sprintf(m.Config.TextFormat, user, content)
}

func (m *Myao) Remember(role, content string, fileDataUrls []string) {
	m.model.Remember(role, content, fileDataUrls)
}

func (m *Myao) Reply(content string, fileDataUrls []string) (string, error) {
	return m.model.Reply("user", content, fileDataUrls)
}
