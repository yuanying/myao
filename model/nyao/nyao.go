package nyao

import (
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model"
	"github.com/yuanying/myao/model/configs"
)

var _ model.Model = (*Nyao)(nil)

type Nyao struct {
	nyao         *model.Shared
	nyaoConfig   *configs.Config
	system       *model.Shared
	systemConfig *configs.Config
}

func New(opts *model.Opts) (*Nyao, error) {
	openAIConfig := openai.DefaultConfig(opts.OpenAIAccessToken)
	if opts.OpenAIOrganizationID != "" {
		openAIConfig.OrgID = opts.OpenAIOrganizationID
	}
	openAI := openai.NewClientWithConfig(openAIConfig)

	nyao, system, err := configs.LoadNyao()
	if err != nil {
		return nil, err
	}

	return &Nyao{
		nyao: &model.Shared{
			Config: nyao,
			OpenAI: openAI,
		},
		system: &model.Shared{
			Config: system,
			OpenAI: openAI,
		},
		nyaoConfig:   nyao,
		systemConfig: system,
	}, nil
}

func (n *Nyao) Name() string {
	return n.nyao.Name
}

func (n *Nyao) FormatText(user, content string) string {
	return fmt.Sprintf(n.nyaoConfig.TextFormat, user, content)
}
func (n *Nyao) Remember(role, content string) {
	n.nyao.Remember(role, content)
}

func (n *Nyao) Reply(content string) (string, error) {
	nyao := n.nyaoReply(content)
	sys := n.sysReply(content)
	nyaoRes := <-nyao
	sysRes := <-sys

	var (
		nyaoRep, sysRep string
	)
	if nyaoRes.err != nil {
		klog.Warningf("Nyao API call returns error: %v", nyaoRes.err)
	}
	nyaoRep = nyaoRes.reply
	if sysRes.err != nil {
		klog.Warningf("Sys API call returns error: %v", sysRes.err)
	}
	sysRep = "> *Correction*\n" + sysRes.reply
	sysRep = strings.Join(strings.Split(sysRep, "\n"), "\n> ")
	rep := nyaoRep + "\n\n" + sysRep

	return rep, nil
}

type result struct {
	err   error
	reply string
}

func (n *Nyao) nyaoReply(content string) <-chan result {
	res := make(chan result)

	go func() {
		defer close(res)

		reply, err := n.nyao.Reply("user", content)
		res <- result{err: err, reply: reply}
	}()
	return res
}

func (n *Nyao) sysReply(content string) <-chan result {
	res := make(chan result)

	messages := []openai.ChatCompletionMessage{
		{
			Role:    "system",
			Content: n.systemConfig.SystemText,
		},
		{
			Role:    "user",
			Content: content,
		},
	}

	for _, m := range n.systemConfig.InitConversations {
		messages = append(messages, openai.ChatCompletionMessage{Role: m.Role, Content: m.Content})
	}
	klog.Infof("System Call:")
	for i, m := range messages {
		klog.Infof("sys %v: %v, %v", i, m.Role, m.Content)
	}

	go func(messages []openai.ChatCompletionMessage) {
		defer close(res)

		output, err := n.system.ChatCompletions(messages)
		if err != nil {
			klog.Warningf("System error message: %v", err)
			res <- result{err: err, reply: n.systemConfig.ErrorText}
			return
		}
		res <- result{reply: output.Choices[0].Message.Content}

	}(messages)
	return res
}
