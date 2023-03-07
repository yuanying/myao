package myao

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"sync"

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
	Name       string
	openAI     api.OpenAIAPIIface
	systemText string

	// mu protects memories from concurrent access.
	mu       sync.RWMutex
	memories []api.Message

	muu    sync.RWMutex
	userID string
}

func New(name string, users map[string]string) *Myao {
	openAI := api.New(&config.Configuration{
		ApiKey:       utils.ToPtr(openAIAccessToken),
		Organization: utils.ToPtr(openAIOrganizationID),
	})

	var sb strings.Builder
	for _, v := range users {
		sb.WriteString(fmt.Sprintf("- %v\n", v))
	}
	systemText := fmt.Sprintf(system, sb.String())
	klog.Infof("SystemText:\n%v", systemText)

	return &Myao{
		Name:       name,
		openAI:     openAI,
		systemText: systemText,
	}
}

func (m *Myao) SetUserID(id string) {
	m.muu.Lock()
	defer m.muu.Unlock()

	klog.Infof("UserID is set: %v", id)
	m.userID = id
}

func (m *Myao) UserID() string {
	m.muu.RLock()
	defer m.muu.RUnlock()

	return m.userID
}

func (m *Myao) Remember(role, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memories = append(m.memories, api.Message{
		Role:    role,
		Content: content,
	})
	if len(m.memories) > 20 {
		m.memories = m.memories[1:]
	}
}

func (m *Myao) Memories() []api.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]api.Message{{Role: "system", Content: m.systemText}}, m.memories...)
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
