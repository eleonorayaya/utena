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
	ListRepoPRs(ctx context.Context, owner, repo string) ([]RawPR, error)
	GetPR(ctx context.Context, owner, repo string, number int) (*RawPR, error)
	GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error)
	GetCurrentUser(ctx context.Context) (string, error)
}

type RawPR struct {
	Number    int
	Title     string
	State     string
	Draft     bool
	HTMLURL   string
	MergedAt  *string
	Assignees []struct{ Login string }
	User      struct{ Login string }
	Head      struct {
		Ref  string
		Repo *struct{ FullName string }
	}
	Base struct{ Ref string }
}

func (r *RawPR) ToPRState() PRState {
	if r.MergedAt != nil {
		return PRStateMerged
	}
	switch r.State {
	case "closed":
		return PRStateClosed
	default:
		return PRStateOpen
	}
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

func (c *githubSDKClient) ListRepoPRs(ctx context.Context, owner, repo string) ([]RawPR, error) {
	prs, _, err := c.client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State:       "all",
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, err
	}
	result := make([]RawPR, len(prs))
	for i, pr := range prs {
		result[i] = ghPRToRawPR(pr)
	}
	return result, nil
}

func (c *githubSDKClient) GetPR(ctx context.Context, owner, repo string, number int) (*RawPR, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	raw := ghPRToRawPR(pr)
	return &raw, nil
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

func ghPRToRawPR(pr *github.PullRequest) RawPR {
	raw := RawPR{
		Number:  pr.GetNumber(),
		Title:   pr.GetTitle(),
		State:   pr.GetState(),
		Draft:   pr.GetDraft(),
		HTMLURL: pr.GetHTMLURL(),
	}

	if pr.MergedAt != nil {
		s := pr.MergedAt.String()
		raw.MergedAt = &s
	}

	for _, a := range pr.Assignees {
		raw.Assignees = append(raw.Assignees, struct{ Login string }{Login: a.GetLogin()})
	}

	raw.User.Login = pr.GetUser().GetLogin()
	raw.Head.Ref = pr.GetHead().GetRef()
	if pr.GetHead().GetRepo() != nil {
		raw.Head.Repo = &struct{ FullName string }{FullName: pr.GetHead().GetRepo().GetFullName()}
	}
	raw.Base.Ref = pr.GetBase().GetRef()

	return raw
}
