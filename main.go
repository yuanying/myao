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

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"github.com/yuanying/myao/slack"
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
	shutdownDelayPeriod time.Duration
	shutdownGracePeriod time.Duration
	maxDelayReplyPeriod time.Duration
	bindAddress         string
	character           string
)

func init() {
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		pflag.CommandLine.AddGoFlag(f)
	})
	pflag.StringVar(&bindAddress, "bind-address", ":8080", "Address on which to expose web interface.")
	pflag.StringVar(&character, "character", "default", "The character of this Chatbot.")
	pflag.DurationVar(&maxDelayReplyPeriod, "max-delay-reply-period", 600*time.Second, "set the time (in seconds) that the myao will wait before replying")
	pflag.DurationVar(&shutdownDelayPeriod, "shutdown-wait-period", 1*time.Second, "set the time (in seconds) that the server will wait before initiating shutdown")
	pflag.DurationVar(&shutdownGracePeriod, "shutdown-grace-period", 5*time.Second, "set the time (in seconds) that the server will wait shutdown")
	pflag.Parse()
}

func main() {

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	mux := http.NewServeMux()

	slackHandler, err := slack.New(character, maxDelayReplyPeriod)
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

	ctx, cancel = context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		klog.Fatal(err.Error())
	}
}
