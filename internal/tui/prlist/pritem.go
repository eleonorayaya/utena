package prlist

import (
	"fmt"
	"strings"

	"github.com/eleonorayaya/utena/internal/git"
)

type prItem struct {
	pr git.PullRequest
}

func (i prItem) Title() string {
	title := fmt.Sprintf("#%d %s", i.pr.Number, i.pr.Title)
	var badges []string
	if i.pr.IsDraft {
		badges = append(badges, "[draft]")
	}
	if i.pr.IsAssignedToMe {
		badges = append(badges, "[assigned]")
	}
	if len(badges) > 0 {
		title += " " + strings.Join(badges, " ")
	}
	return title
}

func (i prItem) Description() string {
	return fmt.Sprintf("@%s · %s", i.pr.AuthorLogin, i.pr.State)
}

func (i prItem) FilterValue() string {
	return i.pr.Title
}
