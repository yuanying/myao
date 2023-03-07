package myao

import (
	_ "embed"
	"os"

	"github.com/ieee0824/gopenai-api/api"
	"github.com/ieee0824/gopenai-api/config"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/utils"
)

var (
	//go:embed system.txt
	system               string
	openAIAccessToken    string
	openAIOrganizationID string
)

func init() {
	openAIAccessToken = os.Getenv("OPENAI_ACCESS_TOKEN")
	openAIOrganizationID = os.Getenv("OPENAI_ORG_ID")
}

type Myao struct {
	openAI   api.OpenAIAPIIface
	memories []api.Message
}

func New() *Myao {
	openAI := api.New(&config.Configuration{
		ApiKey:       utils.ToPtr(openAIAccessToken),
		Organization: utils.ToPtr(openAIOrganizationID),
	})
	return &Myao{
		openAI: openAI,
	}
}

func (m *Myao) Remember(role, content string) {
	m.memories = append(m.memories, api.Message{
		Role:    role,
		Content: content,
	})
	if len(m.memories) > 20 {
		m.memories = m.memories[1:]
	}
}

func (m *Myao) Memories() []api.Message {
	return append([]api.Message{{Role: "system", Content: system}}, m.memories...)
}

func (m *Myao) Reply(content string) (string, error) {
	output, err := m.openAI.ChatCompletionsV1(&api.ChatCompletionsV1Input{
		Model:    utils.ToPtr("gpt-3.5-turbo"),
		Messages: m.Memories(),
	})
	if err != nil {
		klog.Errorf("OpenAI returns error: %v\n, message: %v", err, output.Error.Message)
		return "", err
	}

	reply := output.Choices[0].Message
	m.Remember(reply.Role, reply.Content)

	return reply.Content, nil
}
