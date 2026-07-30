package git

import (
	"context"

	"github.com/google/go-github/v72/github"
)

// MockGitHubClient is a GitHubClient for tests. It lives outside _test.go so
// other packages' tests can use it too, the same way tmux.NewMockRunner does.
type MockGitHubClient struct {
	RepoPRs        []*github.PullRequest
	PRByNumber     map[int]*github.PullRequest
	DiffContent    string
	CurrentUser    string
	Reviews        []*github.PullRequestReview
	ReviewComments []*github.PullRequestComment
	CheckRuns      []*github.CheckRun
	Err            error
}

func (m *MockGitHubClient) ListRepoPRs(context.Context, string, string) ([]*github.PullRequest, error) {
	return m.RepoPRs, m.Err
}

func (m *MockGitHubClient) GetPR(_ context.Context, _, _ string, number int) (*github.PullRequest, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if pr, ok := m.PRByNumber[number]; ok {
		return pr, nil
	}
	return nil, ErrPRNotFound
}

func (m *MockGitHubClient) GetPRDiff(context.Context, string, string, int) (string, error) {
	return m.DiffContent, m.Err
}

func (m *MockGitHubClient) GetCurrentUser(context.Context) (string, error) {
	return m.CurrentUser, m.Err
}

func (m *MockGitHubClient) ListPRReviews(context.Context, string, string, int) ([]*github.PullRequestReview, error) {
	return m.Reviews, m.Err
}

func (m *MockGitHubClient) ListPRReviewComments(context.Context, string, string, int) ([]*github.PullRequestComment, error) {
	return m.ReviewComments, m.Err
}

func (m *MockGitHubClient) ListCheckRuns(context.Context, string, string, string) ([]*github.CheckRun, error) {
	return m.CheckRuns, m.Err
}
