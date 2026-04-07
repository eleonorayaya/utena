package git

import (
	"errors"

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
	RepoID         uint    `json:"repo_id" gorm:"uniqueIndex:idx_pr_repo_number;index"`
	Number         int     `json:"number" gorm:"uniqueIndex:idx_pr_repo_number"`
	HeadBranchID   *uint   `json:"head_branch_id,omitempty" gorm:"index"`
	HeadBranch     *Branch `json:"head_branch,omitempty" gorm:"foreignKey:HeadBranchID"`
	Title          string  `json:"title"`
	State          PRState `json:"state"`
	IsDraft        bool    `json:"is_draft"`
	IsAssignedToMe bool    `json:"is_assigned_to_me"`
	HTMLURL        string  `json:"html_url"`
	AuthorLogin    string  `json:"author_login"`
}
