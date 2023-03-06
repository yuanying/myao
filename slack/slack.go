package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"k8s.io/klog/v2"
)

var (
	slackBotToken      string
	slackSigningSecret string
)

func init() {
	slackBotToken = os.Getenv("SLACK_BOT_TOKEN")
	slackSigningSecret = os.Getenv("SLACK_SIGNING_SECRET")
}

func Handler(w http.ResponseWriter, r *http.Request) {
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
			if _, _, err := api.PostMessage(event.Channel, slack.MsgOptionText("pong", false)); err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
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
