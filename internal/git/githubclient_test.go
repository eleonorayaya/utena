package git

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupMockGitHub(t *testing.T, handler http.Handler) *githubRESTClient {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &githubRESTClient{
		token:      "test-token",
		httpClient: server.Client(),
		baseURL:    server.URL,
	}
}

func TestListRepoPRs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/hello/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := []RawPR{
			{Number: 1, Title: "Fix bug", State: "open", HTMLURL: "https://github.com/octocat/hello/pull/1"},
			{Number: 2, Title: "Add feature", State: "open", Draft: true, HTMLURL: "https://github.com/octocat/hello/pull/2"},
		}
		json.NewEncoder(w).Encode(resp)
	})

	client := setupMockGitHub(t, mux)
	prs, err := client.ListRepoPRs(context.Background(), "octocat", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("expected 2 PRs, got %d", len(prs))
	}
	if prs[0].Number != 1 || prs[0].Title != "Fix bug" {
		t.Errorf("unexpected first PR: %+v", prs[0])
	}
	if !prs[1].Draft {
		t.Error("expected second PR to be draft")
	}
}

func TestGetPR(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/hello/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		pr := RawPR{
			Number:  42,
			Title:   "The answer",
			State:   "open",
			HTMLURL: "https://github.com/octocat/hello/pull/42",
		}
		pr.User.Login = "octocat"
		pr.Head.Ref = "feature-branch"
		pr.Base.Ref = "main"
		json.NewEncoder(w).Encode(pr)
	})

	client := setupMockGitHub(t, mux)
	pr, err := client.GetPR(context.Background(), "octocat", "hello", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 42 {
		t.Errorf("expected number 42, got %d", pr.Number)
	}
	if pr.Title != "The answer" {
		t.Errorf("expected title 'The answer', got %q", pr.Title)
	}
	if pr.User.Login != "octocat" {
		t.Errorf("expected user 'octocat', got %q", pr.User.Login)
	}
	if pr.Head.Ref != "feature-branch" {
		t.Errorf("expected head ref 'feature-branch', got %q", pr.Head.Ref)
	}
	if pr.Base.Ref != "main" {
		t.Errorf("expected base ref 'main', got %q", pr.Base.Ref)
	}
}

func TestGetPRDiff(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/hello/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "application/vnd.github.diff" {
			w.Write([]byte("diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n"))
			return
		}
		json.NewEncoder(w).Encode(RawPR{Number: 1})
	})

	client := setupMockGitHub(t, mux)
	diff, err := client.GetPRDiff(context.Background(), "octocat", "hello", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff")
	}
	if diff != "diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1 +1 @@\n-old\n+new\n" {
		t.Errorf("unexpected diff content: %q", diff)
	}
}

func TestGetCurrentUser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"login": "testuser"})
	})

	client := setupMockGitHub(t, mux)
	login, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if login != "testuser" {
		t.Errorf("expected 'testuser', got %q", login)
	}
}

func TestToPRState(t *testing.T) {
	tests := []struct {
		name     string
		pr       RawPR
		expected PRState
	}{
		{
			name:     "open PR",
			pr:       RawPR{State: "open"},
			expected: PRStateOpen,
		},
		{
			name:     "closed PR",
			pr:       RawPR{State: "closed"},
			expected: PRStateClosed,
		},
		{
			name: "merged PR",
			pr: RawPR{
				State:    "closed",
				MergedAt: strPtr("2026-01-01T00:00:00Z"),
			},
			expected: PRStateMerged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pr.ToPRState()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
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

func TestAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octocat/missing/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	})

	client := setupMockGitHub(t, mux)
	_, err := client.ListRepoPRs(context.Background(), "octocat", "missing")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestListAssignedPRs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"login": "testuser"})
	})
	mux.HandleFunc("/search/issues", func(w http.ResponseWriter, r *http.Request) {
		item1, _ := json.Marshal(map[string]any{
			"number": 10,
			"repository": map[string]string{
				"full_name": "octocat/hello",
			},
		})
		resp := map[string]any{
			"total_count": 1,
			"items":       []json.RawMessage{item1},
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/repos/octocat/hello/pulls/10", func(w http.ResponseWriter, r *http.Request) {
		pr := RawPR{
			Number:  10,
			Title:   "Assigned PR",
			State:   "open",
			HTMLURL: "https://github.com/octocat/hello/pull/10",
		}
		pr.User.Login = "someone"
		pr.Head.Ref = "fix-stuff"
		pr.Base.Ref = "main"
		json.NewEncoder(w).Encode(pr)
	})

	client := setupMockGitHub(t, mux)
	prs, err := client.ListAssignedPRs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
	if prs[0].Number != 10 {
		t.Errorf("expected PR number 10, got %d", prs[0].Number)
	}
	if prs[0].Head.Ref != "fix-stuff" {
		t.Errorf("expected head ref 'fix-stuff', got %q", prs[0].Head.Ref)
	}
}

func strPtr(s string) *string {
	return &s
}
