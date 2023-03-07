package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
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
	myao   *myao.Myao
	slack  *slack.Client
	cancel context.CancelFunc
}

func New() *Handler {
	return &Handler{
		myao:  myao.New(),
		slack: slack.New(slackBotToken),
	}
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
			klog.Infof("AppMentionEvent: %v", event.Text)
		case *slackevents.MessageEvent:
			klog.Infof("MessageEvent: \n bot->%v\n user-> %v\n text -> %v", event.BotID, event.User, event.Text)
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

	if h.cancel != nil {
		h.cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel

	go h.reply(ctx, event.Channel, event.Text)
}

func (h *Handler) reply(ctx context.Context, channel, text string) {
	h.myao.Remember("user", text)
	seed := time.Now().UnixNano()
	rand.Seed(seed)
	sec := rand.Int63n(180)
	klog.Infof("Waiting reply %v seconds", sec)

	select {
	case <-ctx.Done():
		klog.Infof("Skip message: %v", text)
	case <-time.After(time.Duration(sec) * time.Second):
		reply, err := h.myao.Reply(text)
		if err != nil {
			klog.Errorf("Myao reply error: %v", err)
			return
		}
		if _, _, err := h.slack.PostMessage(channel, slack.MsgOptionText(reply, false)); err != nil {
			klog.Errorf("Slack post message error: %v", err)
			return
		}
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
