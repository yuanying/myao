package socket

import (
	"context"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/slack/handler"
)

type Handler struct {
	opts         *handler.Opts
	innerHandler *handler.Handler
}

func New(opts *handler.Opts) (*Handler, error) {
	innerHandler, err := handler.New(opts)
	if err != nil {
		return nil, err
	}

	return &Handler{
		opts:         opts,
		innerHandler: innerHandler,
	}, nil
}

func (h *Handler) Run(ctx context.Context) {
	socket := socketmode.New(h.opts.Slack)

	go func() {
		for socketEvent := range socket.Events {
			switch socketEvent.Type {
			case socketmode.EventTypeEventsAPI:
				socket.Ack(*socketEvent.Request)

				event := socketEvent.Data.(slackevents.EventsAPIEvent)
				switch event.Type {
				case slackevents.CallbackEvent:
					klog.Infof("CallbackEVent: %v", event)
					h.innerHandler.Handle(event.InnerEvent.Data)
				default:
					klog.Warningf("Unsupported event: %v", event.Type)
				}
			case socketmode.EventTypeHello:
				klog.Infof("EventTypeHello")
			default:
				klog.Warningf("Unsupported socket event: %v", socketEvent.Type)
			}
		}
	}()

	socket.RunContext(ctx)
}
