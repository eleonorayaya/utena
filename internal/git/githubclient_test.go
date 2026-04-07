package git

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v72/github"
)

func setupMockSDKClient(t *testing.T, handler http.Handler) *githubSDKClient {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := github.NewClient(nil).WithAuthToken("test-token")
	client.BaseURL, _ = client.BaseURL.Parse(server.URL + "/")
	return newGitHubClientWithBaseClient(client)
}

func TestListRepoPRs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/hello/pulls", func(w http.ResponseWriter, r *http.Request) {
		prs := []*github.PullRequest{
			{Number: github.Ptr(1), Title: github.Ptr("Fix bug"), State: github.Ptr("open"), Draft: github.Ptr(false), HTMLURL: github.Ptr("https://github.com/octocat/hello/pull/1")},
			{Number: github.Ptr(2), Title: github.Ptr("Add feature"), State: github.Ptr("open"), Draft: github.Ptr(true), HTMLURL: github.Ptr("https://github.com/octocat/hello/pull/2")},
		}
		json.NewEncoder(w).Encode(prs)
	})

	client := setupMockSDKClient(t, mux)
	prs, err := client.ListRepoPRs(context.Background(), "octocat", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("expected 2 PRs, got %d", len(prs))
	}
	if prs[0].GetNumber() != 1 || prs[0].GetTitle() != "Fix bug" {
		t.Errorf("unexpected first PR: %+v", prs[0])
	}
	if !prs[1].GetDraft() {
		t.Error("expected second PR to be draft")
	}
}

func TestGetPR(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/hello/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		pr := &github.PullRequest{
			Number:  github.Ptr(42),
			Title:   github.Ptr("The answer"),
			State:   github.Ptr("open"),
			HTMLURL: github.Ptr("https://github.com/octocat/hello/pull/42"),
			User:    &github.User{Login: github.Ptr("octocat")},
			Head:    &github.PullRequestBranch{Ref: github.Ptr("feature-branch")},
			Base:    &github.PullRequestBranch{Ref: github.Ptr("main")},
		}
		json.NewEncoder(w).Encode(pr)
	})

	client := setupMockSDKClient(t, mux)
	pr, err := client.GetPR(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.GetNumber() != 42 {
		t.Errorf("expected number 42, got %d", pr.GetNumber())
	}
	if pr.GetTitle() != "The answer" {
		t.Errorf("expected title 'The answer', got %q", pr.GetTitle())
	}
	if pr.GetUser().GetLogin() != "octocat" {
		t.Errorf("expected user 'octocat', got %q", pr.GetUser().GetLogin())
	}
	if pr.GetHead().GetRef() != "feature-branch" {
		t.Errorf("expected head ref 'feature-branch', got %q", pr.GetHead().GetRef())
	}
	if pr.GetBase().GetRef() != "main" {
		t.Errorf("expected base ref 'main', got %q", pr.GetBase().GetRef())
	}
}

func TestGetPRDiff(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "diff") {
			w.Write([]byte("diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n"))
			return
		}
		json.NewEncoder(w).Encode(github.PullRequest{Number: github.Ptr(1)})
	})

	client := setupMockSDKClient(t, mux)
	diff, err := client.GetPRDiff(context.Background(), "octocat", "hello", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n"
	if diff != expected {
		t.Errorf("unexpected diff content: %q", diff)
	}
}

func TestGetCurrentUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(github.User{Login: github.Ptr("testuser")})
	})

	client := setupMockSDKClient(t, mux)
	login, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if login != "testuser" {
		t.Errorf("expected 'testuser', got %q", login)
	}
}

func TestResolveGitHubToken_EnvVar(t *testing.T) {
	original := getEnv
	t.Cleanup(func() { getEnv = original })

	getEnv = func(key string) string {
		if key == "GITHUB_TOKEN" {
			return "env-token-123"
		}
		return ""
	}

	token := resolveGitHubToken()
	if token != "env-token-123" {
		t.Errorf("expected 'env-token-123', got %q", token)
	}
}
