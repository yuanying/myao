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
	system string
	//go:embed history.txt
	history string
	//go:embed summarize.txt
	summarize string
	//go:embed init.txt
	initText string
	//go:embed error.txt
	errorText            string
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
	summary  string

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

	klog.Infof("Total memories: %v", len(m.memories))
	if len(m.memories) > 20 {
		klog.Infof("Trying compress memories")
		m.summarize()
	}
}

func (m *Myao) summarize() {
	summaryText := summarize
	if m.summary != "" {
		historyText := fmt.Sprintf(history, m.summary)
		summaryText = summaryText + historyText
	}
	output, err := m.openAI.ChatCompletionsV1(&api.ChatCompletionsV1Input{
		Model:    utils.ToPtr("gpt-3.5-turbo"),
		Messages: append([]api.Message{{Role: "system", Content: summaryText}}, m.memories[0:10]...),
	})
	if err != nil {
		klog.Errorf("OpenAI returns error: %v\n, message: %v", err, output.Error.Message)
		return
	}
	m.summary = output.Choices[0].Message.Content
	klog.Infof("longtermMemory is set: %v", m.summary)
	m.memories = m.memories[10:]
}

func (m *Myao) Memories() []api.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	systemText := m.systemText
	if m.summary != "" {
		historyText := fmt.Sprintf(history, m.summary)
		systemText = systemText + historyText
	} else {
		systemText = systemText + initText
	}
	return append([]api.Message{{Role: "system", Content: systemText}}, m.memories...)
}

func (m *Myao) Reply(content string) (string, error) {
	klog.Infof("Requesting chat completions...: %v", content)
	output, err := m.openAI.ChatCompletionsV1(&api.ChatCompletionsV1Input{
		Model:    utils.ToPtr("gpt-3.5-turbo"),
		Messages: m.Memories(),
	})
	if output.Usage != nil {
		klog.Infof("Usage: prompt %v tokens, completions %v tokens", output.Usage.PromptTokens, output.Usage.CompletionTokens)
	}
	if err != nil {
		klog.Errorf("OpenAI returns error: %v\n, message: %v", err, output.Error.Message)
		return errorText, nil
	}

	reply := output.Choices[0].Message
	m.Remember(reply.Role, reply.Content)

	return reply.Content, nil
}
