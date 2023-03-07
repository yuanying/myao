package slack

import (
	"fmt"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"k8s.io/klog/v2"
)

type Users struct {
	users map[string]string
}

func NewUsers(client *slack.Client) (*Users, error) {
	res, err := client.GetUsers()
	if err != nil {
		klog.Errorf("Failed to get users: %v", err)
		return nil, err
	}

	users := map[string]string{}
	for _, v := range res {
		if !v.IsBot {
			name := v.Profile.DisplayName
			if name == "" {
				name = v.Profile.RealName
			}
			klog.Infof("User found: %v, %v", v.ID, name)
			users[v.ID] = name
		}
	}

	return &Users{
		users: users,
	}, nil
}

func (u *Users) Text(event *slackevents.MessageEvent) string {
	if user, exist := u.users[event.User]; exist {
		return fmt.Sprintf("%v: 「%v」", user, event.Text)
	}
	return event.Text
}
