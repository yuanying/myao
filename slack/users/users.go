package users

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/yuanying/myao/model"
	"k8s.io/klog/v2"
)

type Users struct {
	Users map[string]string
}

func New(client *slack.Client) (*Users, error) {
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
		Users: users,
	}, nil
}

func (u *Users) Text(myaoID string, myao model.Model, event *slackevents.MessageEvent) string {
	text := event.Text
	if user, exist := u.Users[event.User]; exist {
		text = myao.FormatText(user, text)
		// text = fmt.Sprintf(myao.Config.TextFormat, user, text)
	}
	for i, v := range u.Users {
		text = strings.Replace(text, fmt.Sprintf("<@%v>", i), v, -1)
	}
	text = strings.Replace(text, fmt.Sprintf("<@%v>", myaoID), myao.Name(), -1)
	return text
}
