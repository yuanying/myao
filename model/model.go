package model

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
	"github.com/sashabaranov/go-openai"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model/configs"
)

const (
	model       = "gpt-4o"
	summaryFile = "summary.txt"
)

func init() {
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}

type Opts struct {
	OpenAIAccessToken    string
	OpenAIOrganizationID string
	CharacterType        string
	UsersMap             map[string]string
	PersistentDir        string
}

type Model interface {
	FormatText(user, content string) string
	Remember(role, content string, fileDataUrls []string)
	Reply(content string, fileDataUrls []string) (string, error)
	Reset() (string, error)
	Name() string
	SaveSummary(summary string)
	LoadSummary()
}

type Shared struct {
	*configs.Config
	OpenAI *openai.Client
	Opts   *Opts

	// mu protects memories from concurrent access.
	mu        sync.RWMutex
	messages  []openai.ChatCompletionMessage
	musummary sync.RWMutex

	systemNumTokens int
}

func ChatCompletionMessage(role, content string, fileDataUrls []string) *openai.ChatCompletionMessage {
	var multiContent []openai.ChatMessagePart
	if content != "" {
		multiContent = append(multiContent, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeText,
			Text: content,
		})
	}
	for _, url := range fileDataUrls {
		imageUrl := openai.ChatMessageImageURL{
			URL:    url,
			Detail: openai.ImageURLDetailAuto,
		}
		multiContent = append(multiContent, openai.ChatMessagePart{
			Type:     openai.ChatMessagePartTypeImageURL,
			ImageURL: &imageUrl,
		})
	}

	return &openai.ChatCompletionMessage{
		Role:         role,
		MultiContent: multiContent,
	}
}

func (s *Shared) Remember(role, content string, fileDataUrls []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	klog.Infof("memories: %v", len(s.messages))
	s.messages = append(s.messages, *ChatCompletionMessage(role, content, fileDataUrls))
}

func (s *Shared) Reset() (string, error) {
	klog.Infof("Reset the old memories")
	messages := s.Messages()
	messages = append(messages, openai.ChatCompletionMessage{Role: "user", Content: s.Config.SummaryText})

	output, err := s.OpenAI.CreateChatCompletion(
		context.TODO(),
		openai.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			Temperature: s.Temperature,
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

	reply := output.Choices[0].Message
	s.SaveSummary(reply.Content)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = []openai.ChatCompletionMessage{}
	s.messages = append(s.messages, *ChatCompletionMessage(reply.Role, reply.Content, []string{}))

	return reply.Content, nil
}

func (s *Shared) SaveSummary(summary string) {
	s.musummary.Lock()
	defer s.musummary.Unlock()
	if err := os.WriteFile(filepath.Join(s.Opts.PersistentDir, summaryFile), []byte(summary), 0644); err != nil {
		klog.Errorf("Failed to write summary: %v", err)
	}
}

func (s *Shared) LoadSummary() {
	s.musummary.Lock()
	defer s.musummary.Unlock()
	summary, err := os.ReadFile(filepath.Join(s.Opts.PersistentDir, summaryFile))
	if err != nil {
		klog.Errorf("Failed to read summary: %v", err)
		return
	}
	s.Remember("assistant", string(summary), []string{})
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

func (s *Shared) Reply(role, content string, fileDataUrls []string) (string, error) {
	klog.Infof("Requesting chat completions...: %v", content)
	temperature := s.Temperature
	messages := s.Messages()
	messages = append(messages, *ChatCompletionMessage(role, content, fileDataUrls))

	output, err := s.OpenAI.CreateChatCompletion(
		context.TODO(),
		openai.ChatCompletionRequest{
			Model:       model,
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
	s.Remember(role, content, fileDataUrls)
	s.Remember(reply.Role, reply.Content, []string{})

	if output.Usage.TotalTokens > 2*8096 {
		go s.Reset()
	}

	return reply.Content, nil
}

func (s *Shared) ChatCompletions(messages []openai.ChatCompletionMessage) (*openai.ChatCompletionResponse, error) {
	temperature := s.Temperature
	response, err := s.OpenAI.CreateChatCompletion(
		context.TODO(),
		openai.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			Temperature: temperature,
		},
	)
	if err != nil {
		return nil, err
	}
	return &response, nil
}
