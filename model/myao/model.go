package myao

import (
	_ "embed"
	"fmt"
	"strings"

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

	var sb strings.Builder
	for _, v := range opts.UsersMap {
		sb.WriteString(fmt.Sprintf("- %v\n", v))
	}
	systemText := fmt.Sprintf(config.SystemText, sb.String())
	config.SystemText = systemText

	m := &Myao{
		model: &model.Shared{
			Config: config,
			OpenAI: openAI,
		},
		Config: config,
	}
	for _, msg := range config.InitConversations {
		m.model.Remember(false, msg.Role, msg.Content)
	}

	return m, nil
}

func (m *Myao) Name() string {
	return m.model.Name
}

func (m *Myao) FormatText(user, content string) string {
	return fmt.Sprintf(m.Config.TextFormat, user, content)
}

func (m *Myao) Remember(role, content string) {
	m.model.Remember(false, role, content)
}

func (m *Myao) Summarize() {
	m.model.Summarize()
}

func (m *Myao) Reply(content string) (string, error) {
	return m.model.Reply(false, "user", content)
}
