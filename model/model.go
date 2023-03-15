package model

type Opts struct {
	OpenAIAccessToken    string
	OpenAIOrganizationID string
	CharacterType        string
	UsersMap             map[string]string
}

type Model interface {
	FormatText(user, content string) string
	Remember(role, content string)
	Reply(content string) (string, error)
	Summarize()
	Name() string
}
