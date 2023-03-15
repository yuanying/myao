package myao

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/ieee0824/gopenai-api/api"
	"github.com/ieee0824/gopenai-api/config"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model"
	"github.com/yuanying/myao/model/myao/configs"
	"github.com/yuanying/myao/utils"
)

var _ model.Model = (*Myao)(nil)

type Memory struct {
	api.Message
	summary bool
}

type Myao struct {
	name   string
	Config *configs.Config
	openAI api.OpenAIAPIIface

	// mu protects memories from concurrent access.
	mu       sync.RWMutex
	memories []Memory

	systemText string
}

func New(opts *model.Opts) (*Myao, error) {
	openAI := api.New(&config.Configuration{
		ApiKey:       utils.ToPtr(opts.OpenAIAccessToken),
		Organization: utils.ToPtr(opts.OpenAIOrganizationID),
	})

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

	m := &Myao{
		name:       config.Name,
		openAI:     openAI,
		Config:     config,
		systemText: systemText,
	}
	for _, msg := range config.InitConversations {
		m.remember(false, msg.Role, msg.Content)
	}

	return m, nil
}

func (m *Myao) Name() string {
	return m.name
}

func (m *Myao) FormatText(user, content string) string {
	return fmt.Sprintf(m.Config.TextFormat, user, content)
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
			m.reply(true, "system", m.Config.SummaryText)
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
		systemText = systemText + m.Config.InitText
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
	temperature := m.Config.Temperature
	if summary {
		temperature = 0
	}
	messages := toMessages(m.Memories())
	if content != "" {
		messages = append(messages, api.Message{Role: role, Content: content})
	}

	output, err := m.openAI.ChatCompletionsV1(&api.ChatCompletionsV1Input{
		Model:       utils.ToPtr("gpt-3.5-turbo"),
		Messages:    messages,
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
		return m.Config.ErrorText, err
	}

	reply := output.Choices[0].Message
	if summary != true && content != "" {
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
