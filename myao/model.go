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
	//go:embed summary_order.txt
	summaryOrder string
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

type Memory struct {
	api.Message
	summary bool
}

type Myao struct {
	Name       string
	openAI     api.OpenAIAPIIface
	systemText string

	// mu protects memories from concurrent access.
	mu       sync.RWMutex
	memories []Memory

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
	m.remember(false, role, content)
}

func (m *Myao) remember(summary bool, role, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.memories = append(m.memories, Memory{
		summary: summary,
		Message: api.Message{
			Role:    role,
			Content: content,
		},
	})

	if role != "user" && summary != true && m.needsSummary() {
		memories := make([]Memory, len(m.memories))
		copy(memories, m.memories)
		klog.Infof("Needs summary")
		go func() {
			m.reply(true, "system", summaryOrder)
		}()
	}

	klog.Infof("Total memories: %v", len(m.memories))
	if len(m.memories) > 20 {
		// m.summarize()
		m.memories = m.memories[len(m.memories)-20:]
	}
}

func (m *Myao) needsSummary() bool {
	if len(m.memories) > 15 {
		for _, mem := range m.memories[len(m.memories)-15:] {
			if mem.summary == true {
				return false
			}
		}
		return true
	}
	return false
}

func (m *Myao) Memories() []Memory {
	m.mu.RLock()
	defer m.mu.RUnlock()
	systemText := m.systemText
	if len(m.memories) < 15 {
		systemText = systemText + initText
	}
	system := Memory{Message: api.Message{Role: "system", Content: systemText}}
	return append([]Memory{system}, m.memories...)
}

func toMessages(ms []Memory) []api.Message {
	rtn := make([]api.Message, len(ms))

	for i := range ms {
		rtn[i] = ms[i].Message
	}
	return rtn
}

func (m *Myao) Reply(content string) (string, error) {
	return m.reply(false, "user", content)
}

func (m *Myao) reply(summary bool, role, content string) (string, error) {
	klog.Infof("Requesting chat completions...: %v", content)
	output, err := m.openAI.ChatCompletionsV1(&api.ChatCompletionsV1Input{
		Model:    utils.ToPtr("gpt-3.5-turbo"),
		Messages: toMessages(m.Memories()),
	})
	if output.Usage != nil {
		klog.Infof("Usage: prompt %v tokens, completions %v tokens", output.Usage.PromptTokens, output.Usage.CompletionTokens)
	}
	if err != nil {
		klog.Errorf("OpenAI returns error: %v\n, message: %v", err, output.Error.Message)
		return errorText, err
	}

	reply := output.Choices[0].Message
	if !summary {
		m.remember(summary, role, content)
	}
	m.remember(summary, reply.Role, reply.Content)
	if summary {
		klog.Infof("Summary: %v", reply.Content)
	}

	return reply.Content, nil
}
