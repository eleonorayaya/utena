package git

import (
	"errors"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var (
	ErrPRNotFound      = errors.New("pull request not found")
	ErrPRAlreadyExists = common.NewConflict("pull request already exists")
)

type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateDraft  PRState = "draft"
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
	IsAssignedToMe bool    `json:"is_assigned_to_me"`
	HTMLURL        string  `json:"html_url"`
	AuthorLogin    string  `json:"author_login"`

	HeadSHA             string      `json:"head_sha,omitempty"`
	ActivityBaselined   bool        `json:"-"`
	LastReviewID        int64       `json:"-"`
	LastReviewCommentID int64       `json:"-"`
	ChecksHeadSHA       string      `json:"-"`
	ChecksState         ChecksState `json:"checks_state,omitempty"`
}

type ChecksState string

const (
	ChecksStatePending ChecksState = "pending"
	ChecksStateFailing ChecksState = "failing"
	ChecksStatePassed  ChecksState = "passed"
	ChecksStateFailed  ChecksState = "failed"
)

func (c ChecksState) terminal() bool {
	return c == ChecksStatePassed || c == ChecksStateFailed
}
