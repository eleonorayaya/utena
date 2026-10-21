package ui

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/assert"
)

func pr(n int, state git.PRState, url string) git.PullRequest {
	return git.PullRequest{Number: n, Title: "title", State: state, HTMLURL: url}
}

func TestOpenablePRs_FiltersAndOrders(t *testing.T) {
	got := OpenablePRs([]git.PullRequest{
		pr(1, git.PRStateDraft, "u1"),
		pr(2, git.PRStateMerged, "u2"),
		pr(3, git.PRStateOpen, "u3"),
		pr(4, git.PRStateOpen, ""),
		pr(5, git.PRStateOpen, "u5"),
	})
	var nums []int
	for _, p := range got {
		nums = append(nums, p.Number)
	}
	assert.Equal(t, []int{5, 3, 1}, nums)
}
