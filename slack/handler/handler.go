package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/model"
	"github.com/yuanying/myao/slack/users"
)

type Opts struct {
	Myao                model.Model
	Slack               *slack.Client
	SlackUsers          *users.Users
	MaxDelayReplyPeriod time.Duration
}

type Handler struct {
	myao                model.Model
	myaoID              string
	slack               *slack.Client
	users               *users.Users
	maxDeplyReplyPeriod time.Duration

	// mu protects cancel from concurrent access.
	mu     sync.Mutex
	cancel context.CancelFunc

	events chan *slackevents.MessageEvent
}

func New(opts *Opts) (*Handler, error) {
	bot, err := opts.Slack.AuthTest()
	if err != nil {
		return nil, err
	}

	h := &Handler{
		users:               opts.SlackUsers,
		myao:                opts.Myao,
		myaoID:              bot.UserID,
		slack:               opts.Slack,
		maxDeplyReplyPeriod: opts.MaxDelayReplyPeriod,
		events:              make(chan *slackevents.MessageEvent, 100),
	}

	go h.processEvent()

	return h, nil
}

func (h *Handler) Handle(event interface{}) {
	switch event := event.(type) {
	case *slackevents.AppMentionEvent:
		klog.Infof("AppMentionEvent: user -> %v,  text -> %v", event.User, event.Text)
	case *slackevents.MessageEvent:
		klog.Infof("MessageEvent: bot-> %v, user-> %v, text -> %v", event.BotID, event.User, event.Text)
		h.events <- event
	}
}

func (h *Handler) processEvent() {
	for event := range h.events {
		h.Reply(event)
	}
}

func convertToDataURL(fileContent []byte, mimeType string) string {
	encoded := base64.StdEncoding.EncodeToString(fileContent)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)
}

func (h *Handler) Reply(event *slackevents.MessageEvent) {
	if event.BotID != "" {
		return
	}
	if event.Text == "" {
		return
	}
	fileDataUrls := make([]string, len(event.Files))
	if event.Files != nil && len(fileDataUrls) > 0 {
		for i, file := range event.Files {
			if file.Filetype == "png" || file.Filetype == "jpg" || file.Filetype == "jpeg" || file.Filetype == "gif" {
				var buf bytes.Buffer
				err := h.slack.GetFile(file.URLPrivate, &buf)
				if err != nil {
					fmt.Printf("Failed to download: %v, %v\n", file.URLPrivate, err)
					continue
				}

				dataURL := convertToDataURL(buf.Bytes(), file.Mimetype)
				fileDataUrls[i] = dataURL
			}
		}
	}
	// event.ThreadTimeStamp

	h.mu.Lock()
	if h.cancel != nil {
		h.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.mu.Unlock()

	// go h.reply(ctx, event.Channel, event.ThreadTimeStamp, h.users.Text(h.myaoID, h.myao, event), fileDataUrls)
	go h.reply(ctx, event.Channel, event.ThreadTimeStamp, event, fileDataUrls)
}

func (h *Handler) reply(ctx context.Context, channel, thread string, event *slackevents.MessageEvent, fileDataUrls []string) {
	sec := 5
	text := h.users.Text(h.myaoID, h.myao, event)

	if !strings.Contains(event.Text, h.myao.Name()) && !strings.Contains(event.Text, fmt.Sprintf("@%v", h.myaoID)) {
		seed := time.Now().UnixNano()
		rand.Seed(seed)
		sec = rand.Intn(int(h.maxDeplyReplyPeriod.Seconds()))
		klog.Infof("Waiting reply %v seconds", sec)
	} else {
		command := strings.Fields(event.Text)
		if len(command) > 1 {
			if command[1] == "/help" {
				reply := "Available commands:\n/help - Show this help\n/reset - Reset the old memories\n"
				msgOpts := []slack.MsgOption{slack.MsgOptionText(reply, false)}
				if thread != "" {
					msgOpts = append(msgOpts, slack.MsgOptionTS(thread))
				}
				if _, _, err := h.slack.PostMessage(channel, msgOpts...); err != nil {
					klog.Errorf("Slack post message error: %v", err)
					return
				}
				return
			} else if command[1] == "/reset" {
				reply, err := h.myao.Reset()
				msgOpts := []slack.MsgOption{slack.MsgOptionText(reply, false)}
				if thread != "" {
					msgOpts = append(msgOpts, slack.MsgOptionTS(thread))
				}

				if err != nil {
					klog.Errorf("Myao reset error: %v", err)
				}
				if reply != "" {
					if _, _, err := h.slack.PostMessage(channel, msgOpts...); err != nil {
						klog.Errorf("Slack post message error: %v", err)
						return
					}
				} else {
					klog.Infof("reply doesn't exist")
				}
				return
			}
		}
	}

	select {
	case <-ctx.Done():
		h.myao.Remember("user", text, fileDataUrls)
		klog.Infof("Skip message: %v", text)
	case <-time.After(time.Duration(sec) * time.Second):
		reply, err := h.myao.Reply(text, fileDataUrls)
		msgOpts := []slack.MsgOption{slack.MsgOptionText(reply, false)}
		if thread != "" {
			msgOpts = append(msgOpts, slack.MsgOptionTS(thread))
		}

		if err != nil {
			klog.Errorf("Myao reply error: %v", err)
		}
		klog.Infof("OpenAPI reply: %v", reply)
		if _, _, err := h.slack.PostMessage(channel, msgOpts...); err != nil {
			klog.Errorf("Slack post message error: %v", err)
			return
		}
	}
}
