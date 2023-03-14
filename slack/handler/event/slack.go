package event

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/slack/handler"
)

type Opts struct {
	*handler.Opts
	SlackSigningSecret string
}

type Handler struct {
	slackSigningSecret string

	innerHandler *handler.Handler
}

func New(opts *Opts) (*Handler, error) {
	innerHandler, err := handler.New(opts.Opts)
	if err != nil {
		return nil, err
	}

	return &Handler{
		slackSigningSecret: opts.SlackSigningSecret,
		innerHandler:       innerHandler,
	}, nil
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := h.Verify(r.Header, r.Body)
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
		klog.Infof("CallbackEVent: %v", event)
		h.innerHandler.Handle(event.InnerEvent.Data)
	}
}

func (h *Handler) Verify(header http.Header, body io.ReadCloser) ([]byte, error) {
	verifier, err := slack.NewSecretsVerifier(header, h.slackSigningSecret)
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
