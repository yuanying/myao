package handler

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/myao"
	"github.com/yuanying/myao/slack/users"
)

type Opts struct {
	Myao                *myao.Myao
	Slack               *slack.Client
	SlackUsers          *users.Users
	MaxDelayReplyPeriod time.Duration
}

type Handler struct {
	myao                *myao.Myao
	myaoID              string
	slack               *slack.Client
	users               *users.Users
	maxDeplyReplyPeriod time.Duration

	// mu protects cancel from concurrent access.
	mu     sync.Mutex
	cancel context.CancelFunc
}

func New(opts *Opts) (*Handler, error) {
	bot, err := opts.Slack.AuthTest()
	if err != nil {
		return nil, err
	}

	return &Handler{
		users:               opts.SlackUsers,
		myao:                opts.Myao,
		myaoID:              bot.UserID,
		slack:               opts.Slack,
		maxDeplyReplyPeriod: opts.MaxDelayReplyPeriod,
	}, nil
}

func (h *Handler) Handle(event interface{}) {
	switch event := event.(type) {
	case *slackevents.AppMentionEvent:
		klog.Infof("AppMentionEvent: user -> %v,  text -> %v", event.User, event.Text)
	case *slackevents.MessageEvent:
		klog.Infof("MessageEvent: bot-> %v, user-> %v, text -> %v", event.BotID, event.User, event.Text)
		h.Reply(event)
	}
}

func (h *Handler) Reply(event *slackevents.MessageEvent) {
	if event.BotID != "" {
		return
	}
	if event.Text == "" {
		return
	}

	h.mu.Lock()
	if h.cancel != nil {
		h.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.mu.Unlock()

	go h.reply(ctx, event.Channel, h.users.Text(h.myaoID, h.myao, event))
}

func (h *Handler) reply(ctx context.Context, channel, text string) {
	sec := 5

	if !strings.Contains(text, h.myao.Name) && !strings.Contains(text, fmt.Sprintf("@%v", h.myaoID)) {
		seed := time.Now().UnixNano()
		rand.Seed(seed)
		sec = rand.Intn(int(h.maxDeplyReplyPeriod.Seconds()))
		klog.Infof("Waiting reply %v seconds", sec)
	}

	select {
	case <-ctx.Done():
		h.myao.Remember("user", text)
		klog.Infof("Skip message: %v", text)
	case <-time.After(time.Duration(sec) * time.Second):
		reply, err := h.myao.Reply(text)
		if err != nil {
			klog.Errorf("Myao reply error: %v", err)
			if _, _, err := h.slack.PostMessage(channel, slack.MsgOptionText(reply, false)); err != nil {
				klog.Errorf("Slack post message error: %v", err)
				return
			}
			return
		}
		klog.Infof("OpenAPI reply: %v", reply)
		if _, _, err := h.slack.PostMessage(channel, slack.MsgOptionText(reply, false)); err != nil {
			klog.Errorf("Slack post message error: %v", err)
			return
		}
		h.myao.Summarize()
	}
}
