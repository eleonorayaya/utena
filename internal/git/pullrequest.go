package git

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var (
	ErrPRNotFound      = errors.New("pull request not found")
	ErrPRAlreadyExists = errors.New("pull request already exists")
)

type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

type PullRequest struct {
	gorm.Model
	RepoID       uint    `json:"repo_id" gorm:"uniqueIndex:idx_pr_repo_number;index"`
	Number       int     `json:"number" gorm:"uniqueIndex:idx_pr_repo_number"`
	HeadBranchID *uint   `json:"head_branch_id,omitempty" gorm:"index"`
	HeadBranch   *Branch `json:"head_branch,omitempty" gorm:"foreignKey:HeadBranchID"`
	Title        string  `json:"title"`
	State        PRState `json:"state"`
	IsDraft      bool    `json:"is_draft"`
	HTMLURL      string  `json:"html_url"`
	AuthorLogin  string  `json:"author_login"`
}

func (pr *PullRequest) Signals() []common.Signal {
	switch pr.State {
	case PRStateMerged:
		return []common.Signal{{
			Source:   "github",
			Key:      fmt.Sprintf("pr:%d:merged", pr.ID),
			Severity: common.SeverityInfo,
			Label:    "merged",
		}}
	case PRStateOpen:
		label := "open"
		if pr.IsDraft {
			label = "draft"
		}
		return []common.Signal{{
			Source:   "github",
			Key:      fmt.Sprintf("pr:%d:open", pr.ID),
			Severity: common.SeverityInfo,
			Label:    label,
		}}
	default:
		return nil
	}
}
