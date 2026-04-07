package prlist

import (
	"fmt"

	"github.com/eleonorayaya/utena/internal/git"
)

type prItem struct {
	pr git.PullRequest
}

func (i prItem) Title() string {
	title := fmt.Sprintf("#%d %s", i.pr.Number, i.pr.Title)
	if i.pr.IsAssignedToMe {
		title += " [assigned]"
	}
	return title
}

func (i prItem) Description() string {
	return fmt.Sprintf("@%s · %s", i.pr.AuthorLogin, i.pr.State)
}

func (i prItem) FilterValue() string {
	return i.pr.Title
}
