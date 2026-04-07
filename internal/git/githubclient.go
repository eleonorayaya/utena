package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v72/github"
)

type GitHubClient interface {
	ListRepoPRs(ctx context.Context, owner, repo string) ([]*github.PullRequest, error)
	GetPR(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error)
	GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error)
	GetCurrentUser(ctx context.Context) (string, error)
}

var getEnv = os.Getenv

type githubSDKClient struct {
	client *github.Client
}

func NewGitHubClient(ctx context.Context) (GitHubClient, error) {
	token := resolveGitHubToken()
	if token == "" {
		return nil, fmt.Errorf("no GitHub token found: set GITHUB_TOKEN or install gh CLI")
	}
	return &githubSDKClient{
		client: github.NewClient(nil).WithAuthToken(token),
	}, nil
}

func newGitHubClientWithBaseClient(httpClient *github.Client) *githubSDKClient {
	return &githubSDKClient{client: httpClient}
}

func resolveGitHubToken() string {
	if token := getEnv("GITHUB_TOKEN"); token != "" {
		return token
	}
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *githubSDKClient) ListRepoPRs(ctx context.Context, owner, repo string) ([]*github.PullRequest, error) {
	prs, _, err := c.client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State:       "all",
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, err
	}
	return prs, nil
}

func (c *githubSDKClient) GetPR(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (c *githubSDKClient) GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	diff, _, err := c.client.PullRequests.GetRaw(ctx, owner, repo, number, github.RawOptions{Type: github.Diff})
	if err != nil {
		return "", err
	}
	return diff, nil
}

func (c *githubSDKClient) GetCurrentUser(ctx context.Context) (string, error) {
	user, _, err := c.client.Users.Get(ctx, "")
	if err != nil {
		return "", err
	}
	return user.GetLogin(), nil
}
