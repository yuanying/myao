package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/slack-go/slack"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/myao"
	"github.com/yuanying/myao/slack/handler"
	"github.com/yuanying/myao/slack/handler/event"
	"github.com/yuanying/myao/slack/handler/socket"
	"github.com/yuanying/myao/slack/users"
)

const (
	rootHTMLDoc = `<html>
<head><title>Myao</title></head>
<body>
<h1>Myao</h1>
<h2>Build</h2>
<pre>%s</pre>
</body>
</html>`
)

var (
	// General options
	handlerType         string
	character           string
	maxDelayReplyPeriod time.Duration

	// Options for Event type handler
	shutdownDelayPeriod time.Duration
	shutdownGracePeriod time.Duration
	bindAddress         string

	// Options for Slack Client
	slackBotToken string
	// For socket mode Slack Client
	slackAppToken string
	// For event mode Slack Client
	slackSigningSecret string

	// Options for OpenAI Client
	openAIAccessToken    string
	openAIOrganizationID string
)

func init() {
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		pflag.CommandLine.AddGoFlag(f)
	})
	pflag.StringVar(&character, "character", "default", "The character of this Chatbot.")
	pflag.StringVar(&handlerType, "handler", "event", "Type of event handler.")
	pflag.DurationVar(&maxDelayReplyPeriod, "max-delay-reply-period", 600*time.Second, "set the time (in seconds) that the myao will wait before replying")

	pflag.StringVar(&bindAddress, "bind-address", ":8080", "Address on which to expose web interface.")
	pflag.DurationVar(&shutdownDelayPeriod, "shutdown-wait-period", 1*time.Second, "set the time (in seconds) that the server will wait before initiating shutdown")
	pflag.DurationVar(&shutdownGracePeriod, "shutdown-grace-period", 5*time.Second, "set the time (in seconds) that the server will wait shutdown")
	pflag.Parse()

	slackBotToken = os.Getenv("SLACK_BOT_TOKEN")
	slackAppToken = os.Getenv("SLACK_APP_TOKEN")
	slackSigningSecret = os.Getenv("SLACK_SIGNING_SECRET")

	openAIAccessToken = os.Getenv("OPENAI_ACCESS_TOKEN")
	openAIOrganizationID = os.Getenv("OPENAI_ORG_ID")
}

func main() {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	slackOpts := []slack.Option{}
	if slackAppToken != "" {
		slackOpts = append(slackOpts, slack.OptionAppLevelToken(slackAppToken))
	}
	slackCli := slack.New(slackBotToken, slackOpts...)
	slackUsers, err := users.New(slackCli)
	if err != nil {
		klog.Errorf("Failed to create slack users obj: %v", err)
		os.Exit(1)
	}

	myaoOpts := &myao.Opts{
		OpenAIAccessToken:    openAIAccessToken,
		OpenAIOrganizationID: openAIOrganizationID,
		UsersMap:             slackUsers.Users,
		CharacterType:        character,
	}
	myao, err := myao.New(myaoOpts)
	if err != nil {
		klog.Errorf("Failed to create myao obj: %v", err)
		os.Exit(1)
	}

	switch handlerType {
	case "socket":
		s, err := socket.New(&handler.Opts{
			Myao:                myao,
			Slack:               slackCli,
			SlackUsers:          slackUsers,
			MaxDelayReplyPeriod: maxDelayReplyPeriod,
		})
		if err != nil {
			klog.Error("Failed to load socket client: %v", err)
			os.Exit(1)
		}
		s.Run(ctx)

	default:
		runEventHandler(ctx, slackCli, slackUsers, myao)
	}
}

func runEventHandler(ctx context.Context, slackCli *slack.Client, slackUsers *users.Users, myao *myao.Myao) {
	mux := http.NewServeMux()

	slackOpts := &event.Opts{
		Opts: &handler.Opts{
			Myao:                myao,
			Slack:               slackCli,
			SlackUsers:          slackUsers,
			MaxDelayReplyPeriod: maxDelayReplyPeriod,
		},
		SlackSigningSecret: slackSigningSecret,
	}
	slackHandler, err := event.New(slackOpts)
	if err != nil {
		klog.Errorf("slackHandler initialization fails: %v", err)
		return
	}
	mux.HandleFunc("/slack/events", slackHandler.Handle)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, (fmt.Sprintf(rootHTMLDoc, "v0.0.1")))
	})
	server := &http.Server{
		Addr:    bindAddress,
		Handler: mux,
	}

	go func() {
		klog.Info("start server")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			klog.Fatal(err.Error())
		}
	}()

	<-ctx.Done()

	klog.Info("signal received...")
	time.Sleep(shutdownDelayPeriod)

	ctx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		klog.Fatal(err.Error())
	}
}
