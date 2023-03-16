package model

import (
	"sync"

	"github.com/ieee0824/gopenai-api/api"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model/configs"
	"github.com/yuanying/myao/utils"
)

type Opts struct {
	OpenAIAccessToken    string
	OpenAIOrganizationID string
	CharacterType        string
	UsersMap             map[string]string
}

type Model interface {
	FormatText(user, content string) string
	Remember(role, content string)
	Reply(content string) (string, error)
	Summarize()
	Name() string
}

type memory struct {
	api.Message
	summary bool
}

type Shared struct {
	*configs.Config
	OpenAI api.OpenAIAPIIface

	// mu protects memories from concurrent access.
	mu       sync.RWMutex
	memories []memory
}

func (s *Shared) Remember(summary bool, role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	klog.Infof("memories: %v", len(s.memories))
	s.memories = append(s.memories, memory{
		summary: summary,
		Message: api.Message{
			Role:    role,
			Content: content,
		},
	})
}

func (s *Shared) Forget(num int) {
	klog.Infof("Try forget the old memries")
	s.mu.Lock()
	defer s.mu.Unlock()
	if num > len(s.memories) {
		num = len(s.memories)
	}
	s.memories = s.memories[num:]
}

func (s *Shared) Messages() []api.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	rtn := make([]api.Message, len(s.memories)+1)
	rtn[0] = api.Message{Role: "system", Content: s.SystemText}

	for i := range s.memories {
		rtn[i+1] = s.memories[i].Message
	}
	return rtn
}

func (s *Shared) Summarize() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.needsSummary() {
		memories := make([]memory, len(s.memories))
		copy(memories, s.memories)
		klog.Infof("Needs summary")
		go func() {
			s.Reply(true, "system", s.SummaryText)
		}()
	}
}

func (s *Shared) needsSummary() bool {
	if len(s.memories) > 15 {
		for _, mem := range s.memories[len(s.memories)-15:] {
			if mem.summary == true {
				return false
			}
		}
		return true
	}
	return false
}

func (s *Shared) Reply(summary bool, role, content string) (string, error) {
	klog.Infof("Requesting chat completions...: %v", content)
	temperature := s.Temperature
	if summary {
		temperature = 0
	}
	messages := s.Messages()
	if content != "" {
		messages = append(messages, api.Message{Role: role, Content: content})
	}

	output, err := s.OpenAI.ChatCompletionsV1(&api.ChatCompletionsV1Input{
		Model:       utils.ToPtr("gpt-3.5-turbo"),
		Messages:    messages,
		Temperature: utils.ToPtr(temperature),
	})
	if output.Usage != nil {
		klog.Infof("Usage: prompt %v tokens, completions %v tokens", output.Usage.PromptTokens, output.Usage.CompletionTokens)
		if output.Usage.PromptTokens > 3584 {
			s.Forget(5)
		} else if output.Usage.PromptTokens > 3328 {
			s.Forget(4)
		} else if output.Usage.PromptTokens > 3072 {
			s.Forget(3)
		}
	}
	if err != nil {
		if output.Error != nil {
			klog.Errorf("OpenAI returns error: %v\n, message: %v", err, output.Error.Message)
			if output.Error.Code == "context_length_exceeded" {
				s.Forget(8)
			}
		}
		return s.ErrorText, err
	}

	reply := output.Choices[0].Message
	if summary != true && content != "" {
		s.Remember(summary, role, content)
	}
	s.Remember(summary, reply.Role, reply.Content)
	if summary {
		klog.Infof("Summary: %v", reply.Content)
	}

	return reply.Content, nil
}
