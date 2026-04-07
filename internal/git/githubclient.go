package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

type GitHubClient interface {
	ListRepoPRs(ctx context.Context, owner, repo string) ([]RawPR, error)
	GetPR(ctx context.Context, owner, repo string, number int) (*RawPR, error)
	GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error)
	GetCurrentUser(ctx context.Context) (string, error)
}

type RawPR struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	State     string  `json:"state"`
	Draft     bool    `json:"draft"`
	HTMLURL   string  `json:"html_url"`
	MergedAt  *string `json:"merged_at"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref  string `json:"ref"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
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

type githubRESTClient struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

func NewGitHubClient(ctx context.Context) (GitHubClient, error) {
	token := resolveGitHubToken()
	if token == "" {
		return nil, fmt.Errorf("no GitHub token found: set GITHUB_TOKEN or install gh CLI")
	}
	return &githubRESTClient{
		token:      token,
		httpClient: &http.Client{},
		baseURL:    "https://api.github.com",
	}, nil
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

func (c *githubRESTClient) doRequest(ctx context.Context, method, url string, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", accept)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *githubRESTClient) ListRepoPRs(ctx context.Context, owner, repo string) ([]RawPR, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=all&per_page=100", c.baseURL, owner, repo)
	body, err := c.doRequest(ctx, "GET", url, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}

	var prs []RawPR
	if err := json.Unmarshal(body, &prs); err != nil {
		return nil, fmt.Errorf("failed to parse PR list: %w", err)
	}
	return prs, nil
}

func (c *githubRESTClient) GetPR(ctx context.Context, owner, repo string, number int) (*RawPR, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.baseURL, owner, repo, number)
	body, err := c.doRequest(ctx, "GET", url, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}

	var pr RawPR
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse PR: %w", err)
	}
	return &pr, nil
}

func (c *githubRESTClient) GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.baseURL, owner, repo, number)
	body, err := c.doRequest(ctx, "GET", url, "application/vnd.github.diff")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *githubRESTClient) GetCurrentUser(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/user", c.baseURL)
	body, err := c.doRequest(ctx, "GET", url, "application/vnd.github+json")
	if err != nil {
		return "", err
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return "", fmt.Errorf("failed to parse user: %w", err)
	}
	return user.Login, nil
}
