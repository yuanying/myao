package model

import (
	"context"
	"errors"
	"sync"

	"github.com/sashabaranov/go-openai"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model/configs"
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
	Message openai.ChatCompletionMessage
	summary bool
}

type Shared struct {
	*configs.Config
	OpenAI *openai.Client

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
		Message: openai.ChatCompletionMessage{
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

func (s *Shared) Messages() []openai.ChatCompletionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	rtn := make([]openai.ChatCompletionMessage, len(s.memories)+1)
	rtn[0] = openai.ChatCompletionMessage{Role: "system", Content: s.SystemText}

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
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: content})
	}

	output, err := s.OpenAI.CreateChatCompletion(
		context.TODO(),
		openai.ChatCompletionRequest{
			Model:       openai.GPT3Dot5Turbo,
			Messages:    messages,
			Temperature: temperature,
		},
	)
	if err != nil {
		klog.Errorf("OpenAI returns error: %v", err)
		var openAIErr *openai.APIError
		if errors.As(err, &openAIErr) {
			klog.Infof("openAIErr Message: %v", err, openAIErr.Message)
			if openAIErr.Code != nil {
				klog.Infof("openAIErr Code: %v", openAIErr.Code)
				if *openAIErr.Code == "context_length_exceeded" {
					s.Forget(8)
				}
			}
		}
		return s.ErrorText, err
	}

	klog.Infof("Usage: prompt %v tokens, completions %v tokens", output.Usage.PromptTokens, output.Usage.CompletionTokens)
	if output.Usage.PromptTokens > 3584 {
		s.Forget(5)
	} else if output.Usage.PromptTokens > 3328 {
		s.Forget(4)
	} else if output.Usage.PromptTokens > 3072 {
		s.Forget(3)
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

func (s *Shared) ChatCompletions(messages []openai.ChatCompletionMessage) (*openai.ChatCompletionResponse, error) {
	temperature := s.Temperature
	response, err := s.OpenAI.CreateChatCompletion(
		context.TODO(),
		openai.ChatCompletionRequest{
			Model:       openai.GPT3Dot5Turbo,
			Messages:    messages,
			Temperature: temperature,
		},
	)
	if err != nil {
		return nil, err
	}
	return &response, nil
}
