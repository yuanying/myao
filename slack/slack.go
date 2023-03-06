package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

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
	myao *myao.Myao
}

func New() *Handler {
	return &Handler{
		myao: myao.New(),
	}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	api := slack.New(slackBotToken)

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
			go func() {
				reply, err := h.myao.Reply(event.Text)
				if err != nil {
					klog.Errorf("Myao reply error: %v", err)
					return
				}
				if _, _, err := api.PostMessage(event.Channel, slack.MsgOptionText(reply, false)); err != nil {
					klog.Errorf("Slack post message error: %v", err)
					return
				}
			}()
		case *slackevents.MessageEvent:
			klog.Infof("MessageEvent: \n bot->%v\n user-> %v\n text -> %v", event.BotID, event.User, event.Text)
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
