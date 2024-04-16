package model

import (
	"context"
	"errors"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
	"github.com/sashabaranov/go-openai"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model/configs"
	"github.com/yuanying/myao/utils"
)

func init() {
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}

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
	Name() string
}

type Shared struct {
	*configs.Config
	OpenAI *openai.Client

	// mu protects memories from concurrent access.
	mu              sync.RWMutex
	messages        []openai.ChatCompletionMessage
	systemNumTokens int
}

func (s *Shared) Remember(role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	klog.Infof("memories: %v", len(s.messages))
	s.messages = append(s.messages,
		openai.ChatCompletionMessage{
			Role:    role,
			Content: content,
		})
}

func (s *Shared) forget(role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.systemNumTokens == 0 {
		s.systemNumTokens = utils.NumTokensFromMessages(
			[]openai.ChatCompletionMessage{{Role: "system", Content: s.SystemText}},
			openai.GPT4Turbo,
		)
		klog.Infof("systemNumTokens: %v", s.systemNumTokens)
	}
	messages := append(s.messages,
		openai.ChatCompletionMessage{
			Role:    role,
			Content: content,
		})
	numTokens := utils.NumTokensFromMessages(messages, openai.GPT4Turbo)
	for (s.systemNumTokens + numTokens) > 3072 {
		klog.Infof("Total tokens: %v, forgetting...", s.systemNumTokens+numTokens)
		s.messages = s.messages[1:]
		messages = append(s.messages,
			openai.ChatCompletionMessage{
				Role:    role,
				Content: content,
			})
		numTokens = utils.NumTokensFromMessages(messages, openai.GPT4Turbo)
	}
}

func (s *Shared) Forget(num int) {
	klog.Infof("Try forget the old memries")
	s.mu.Lock()
	defer s.mu.Unlock()
	if num > len(s.messages) {
		num = len(s.messages)
	}
	s.messages = s.messages[num:]
}

func (s *Shared) Messages() []openai.ChatCompletionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	rtn := make([]openai.ChatCompletionMessage, len(s.messages)+1)
	rtn[0] = openai.ChatCompletionMessage{Role: "system", Content: s.SystemText}

	for i := range s.messages {
		rtn[i+1] = s.messages[i]
	}
	return rtn
}

func (s *Shared) Reply(role, content string) (string, error) {
	klog.Infof("Requesting chat completions...: %v", content)
	s.forget(role, content)
	temperature := s.Temperature
	messages := s.Messages()
	if content != "" {
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: content})
	}

	output, err := s.OpenAI.CreateChatCompletion(
		context.TODO(),
		openai.ChatCompletionRequest{
			Model:       openai.GPT4Turbo,
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
			}
		}
		return s.ErrorText, err
	}

	klog.Infof("Usage: prompt %v tokens, completions %v tokens", output.Usage.PromptTokens, output.Usage.CompletionTokens)

	reply := output.Choices[0].Message
	s.Remember(role, content)
	s.Remember(reply.Role, reply.Content)

	return reply.Content, nil
}

func (s *Shared) ChatCompletions(messages []openai.ChatCompletionMessage) (*openai.ChatCompletionResponse, error) {
	temperature := s.Temperature
	response, err := s.OpenAI.CreateChatCompletion(
		context.TODO(),
		openai.ChatCompletionRequest{
			Model:       openai.GPT4Turbo,
			Messages:    messages,
			Temperature: temperature,
		},
	)
	if err != nil {
		return nil, err
	}
	return &response, nil
}
