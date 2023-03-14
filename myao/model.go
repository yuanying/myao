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

	"github.com/yuanying/myao/myao/configs"
	"github.com/yuanying/myao/utils"
)

var (
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
	Name   string
	config *configs.Config
	openAI api.OpenAIAPIIface

	// mu protects memories from concurrent access.
	mu       sync.RWMutex
	memories []Memory

	muu    sync.RWMutex
	userID string

	systemText string
}

func New(character string, users map[string]string) (*Myao, error) {
	openAI := api.New(&config.Configuration{
		ApiKey:       utils.ToPtr(openAIAccessToken),
		Organization: utils.ToPtr(openAIOrganizationID),
	})

	config, err := configs.Load(character)
	if err != nil {
		klog.Errorf("Failed to load config: %v", err)
		return nil, err
	}

	var sb strings.Builder
	for _, v := range users {
		sb.WriteString(fmt.Sprintf("- %v\n", v))
	}
	systemText := fmt.Sprintf(config.SystemText, sb.String())

	return &Myao{
		Name:       config.Name,
		openAI:     openAI,
		config:     config,
		systemText: systemText,
	}, nil
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
	klog.Infof("Memories: %v", len(m.memories))
	m.memories = append(m.memories, Memory{
		summary: summary,
		Message: api.Message{
			Role:    role,
			Content: content,
		},
	})
}

func (m *Myao) Summarize() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.needsSummary() {
		memories := make([]Memory, len(m.memories))
		copy(memories, m.memories)
		klog.Infof("Needs summary")
		go func() {
			m.reply(true, "system", m.config.SummaryText)
		}()
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
		systemText = systemText + m.config.InitText
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
	temperature := m.config.Temperature
	if summary {
		temperature = 0
	}

	output, err := m.openAI.ChatCompletionsV1(&api.ChatCompletionsV1Input{
		Model:       utils.ToPtr("gpt-3.5-turbo"),
		Messages:    append(toMessages(m.Memories()), api.Message{Role: role, Content: content}),
		Temperature: utils.ToPtr(temperature),
	})
	if output.Usage != nil {
		klog.Infof("Usage: prompt %v tokens, completions %v tokens", output.Usage.PromptTokens, output.Usage.CompletionTokens)
		if output.Usage.PromptTokens > 3584 {
			m.forget(5)
		} else if output.Usage.PromptTokens > 3328 {
			m.forget(4)
		} else if output.Usage.PromptTokens > 3072 {
			m.forget(3)
		}
	}
	if err != nil {
		if output.Error != nil {
			klog.Errorf("OpenAI returns error: %v\n, message: %v", err, output.Error.Message)
			if output.Error.Code == "context_length_exceeded" {
				m.forget(8)
			}
		}
		return m.config.ErrorText, err
	}

	reply := output.Choices[0].Message
	if summary != true {
		m.remember(summary, role, content)
	}
	m.remember(summary, reply.Role, reply.Content)
	if summary {
		klog.Infof("Summary: %v", reply.Content)
	}

	return reply.Content, nil
}

func (m *Myao) forget(num int) {
	klog.Infof("Try forget the old memries")
	m.mu.Lock()
	defer m.mu.Unlock()
	if num > len(m.memories) {
		num = len(m.memories)
	}
	m.memories = m.memories[num:]
}
