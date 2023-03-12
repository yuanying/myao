package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/myao"
)

var (
	slackBotToken      string
	slackSigningSecret string
)

func init() {
	slackBotToken = os.Getenv("SLACK_BOT_TOKEN")
	slackSigningSecret = os.Getenv("SLACK_SIGNING_SECRET")
}

type Handler struct {
	myao                *myao.Myao
	slack               *slack.Client
	users               *Users
	maxDeplyReplyPeriod time.Duration

	// mu protects cancel from concurrent access.
	mu     sync.Mutex
	cancel context.CancelFunc
}

func New(maxDeplyReplyPeriod time.Duration) (*Handler, error) {
	slack := slack.New(slackBotToken)
	bot, err := slack.AuthTest()
	if err != nil {
		return nil, err
	}
	users, err := NewUsers(slack)
	if err != nil {
		return nil, err
	}
	myao, err := myao.New(users.users)
	if err != nil {
		return nil, err
	}
	myao.SetUserID(bot.UserID)
	return &Handler{
		users:               users,
		myao:                myao,
		slack:               slack,
		maxDeplyReplyPeriod: maxDeplyReplyPeriod,
	}, nil
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := Verify(r.Header, r.Body)
	if err != nil {
		klog.Errorf("Verify error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		klog.Errorf("Event parsing failes: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	switch event.Type {
	case slackevents.AppRateLimited:
		klog.Errorf("Slack events are rate limted: %v", event)
	case slackevents.URLVerification:
		var res *slackevents.ChallengeResponse
		if err := json.Unmarshal(body, &res); err != nil {
			klog.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte(res.Challenge)); err != nil {
			klog.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent
		switch event := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			klog.Infof("AppMentionEvent: user -> %v,  text -> %v", event.User, event.Text)
		case *slackevents.MessageEvent:
			klog.Infof("MessageEvent: bot-> %v, user-> %v, text -> %v", event.BotID, event.User, event.Text)
			h.Reply(event)
		}
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

	go h.reply(ctx, event.Channel, h.users.Text(h.myao, event))
}

func (h *Handler) reply(ctx context.Context, channel, text string) {
	sec := 5

	if !strings.Contains(text, h.myao.Name) && !strings.Contains(text, fmt.Sprintf("@%v", h.myao.UserID())) {
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

func Verify(header http.Header, body io.ReadCloser) ([]byte, error) {
	verifier, err := slack.NewSecretsVerifier(header, slackSigningSecret)
	if err != nil {
		return nil, fmt.Errorf("slack secret verifier error: %w", err)
	}

	bodyReader := io.TeeReader(body, &verifier)
	bodyByte, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, fmt.Errorf("read request body error: %w", err)
	}

	if err := verifier.Ensure(); err != nil {
		return nil, fmt.Errorf("ensure slack secret verifier error: %w", err)
	}
	return bodyByte, nil
}
