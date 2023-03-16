package nyao

import (
	"github.com/yuanying/myao/model"
)

var _ model.Model = (*Nyao)(nil)

type Nyao struct {
	nyao   *model.Shared
	system *model.Shared
}

func (n *Nyao) Name() string {
	return n.nyao.Name
}

func (n *Nyao) FormatText(user, content string) string {
	// return fmt.Sprintf(n.Config.TextFormat, user, content)
	return ""
}
func (n *Nyao) Remember(role, content string) {
	n.nyao.Remember(false, role, content)
}

func (n *Nyao) Reply(content string) (string, error) {
	return "", nil
}

func (n *Nyao) Summarize() {}
