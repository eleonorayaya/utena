package git

import (
	"context"
	"slices"
	"strings"

	"github.com/google/go-github/v72/github"
)

const (
	ActivityReview        = "pull_request_review"
	ActivityReviewComment = "pull_request_review_comment"
	ActivityChecks        = "ci_checks"
)

const bodyLimit = 600

type PRActivity struct {
	Type string
	Data any
}

type ReviewActivity struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Author string `json:"author"`
	Body   string `json:"body,omitempty"`
	URL    string `json:"url"`
}

type ReviewCommentActivity struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Body   string `json:"body,omitempty"`
	URL    string `json:"url"`
}

type ChecksActivity struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	State  string   `json:"state"`
	Failed []string `json:"failed,omitempty"`
	URL    string   `json:"url"`
}

// SyncPRActivity fetches reviews, inline review comments and check runs for a
// single PR and returns only what is new since the last sync. Watermarks are
// persisted on the PR, so a daemon restart does not replay old activity.
func (s *GitService) SyncPRActivity(ctx context.Context, pr *PullRequest) ([]PRActivity, error) {
	if s.githubClient == nil {
		return nil, ErrNoGitHubClient
	}
	repo, err := s.repoStore.GetByID(pr.RepoID)
	if err != nil {
		return nil, err
	}
	owner, name, err := repo.OwnerAndName()
	if err != nil {
		return nil, err
	}

	activity := make([]PRActivity, 0)
	updated := false

	reviews, err := s.githubClient.ListPRReviews(ctx, owner, name, pr.Number)
	if err != nil {
		return nil, err
	}
	if events, watermark := s.newReviews(pr, reviews); watermark > pr.LastReviewID || !pr.ActivityBaselined {
		activity = append(activity, events...)
		pr.LastReviewID = watermark
		updated = true
	}

	comments, err := s.githubClient.ListPRReviewComments(ctx, owner, name, pr.Number)
	if err != nil {
		return nil, err
	}
	if events, watermark := s.newReviewComments(pr, comments); watermark > pr.LastReviewCommentID || !pr.ActivityBaselined {
		activity = append(activity, events...)
		pr.LastReviewCommentID = watermark
		updated = true
	}
	if !pr.ActivityBaselined {
		pr.ActivityBaselined = true
		updated = true
	}

	if pr.HeadSHA != "" {
		runs, err := s.githubClient.ListCheckRuns(ctx, owner, name, pr.HeadSHA)
		if err != nil {
			return nil, err
		}
		if event, state := checksTransition(pr, runs); state != pr.ChecksState || pr.ChecksHeadSHA != pr.HeadSHA {
			if event != nil {
				activity = append(activity, *event)
			}
			pr.ChecksState = state
			pr.ChecksHeadSHA = pr.HeadSHA
			updated = true
		}
	}

	if updated {
		if err := s.prStore.Update(pr); err != nil {
			return nil, err
		}
	}
	return activity, nil
}

func (s *GitService) newReviews(pr *PullRequest, reviews []*github.PullRequestReview) ([]PRActivity, int64) {
	watermark := pr.LastReviewID
	var out []PRActivity
	for _, review := range reviews {
		if review.GetID() > watermark {
			watermark = review.GetID()
		}
		if !pr.ActivityBaselined || review.GetID() <= pr.LastReviewID {
			continue
		}
		author := review.GetUser().GetLogin()
		if s.ignoredAuthor(author, review.GetUser().GetType()) {
			continue
		}
		out = append(out, PRActivity{Type: ActivityReview, Data: ReviewActivity{
			Number: pr.Number,
			Title:  pr.Title,
			State:  strings.ToLower(review.GetState()),
			Author: author,
			Body:   truncate(review.GetBody()),
			URL:    review.GetHTMLURL(),
		}})
	}
	return out, watermark
}

func (s *GitService) newReviewComments(pr *PullRequest, comments []*github.PullRequestComment) ([]PRActivity, int64) {
	watermark := pr.LastReviewCommentID
	var out []PRActivity
	for _, comment := range comments {
		if comment.GetID() > watermark {
			watermark = comment.GetID()
		}
		if !pr.ActivityBaselined || comment.GetID() <= pr.LastReviewCommentID {
			continue
		}
		author := comment.GetUser().GetLogin()
		if s.ignoredAuthor(author, comment.GetUser().GetType()) {
			continue
		}
		out = append(out, PRActivity{Type: ActivityReviewComment, Data: ReviewCommentActivity{
			Number: pr.Number,
			Title:  pr.Title,
			Author: author,
			Path:   comment.GetPath(),
			Line:   comment.GetLine(),
			Body:   truncate(comment.GetBody()),
			URL:    comment.GetHTMLURL(),
		}})
	}
	// the API gave us newest first, so flip to reading order
	slices.Reverse(out)
	return out, watermark
}

// checksTransition reports the check state for the PR's head commit and the
// event worth sending: the first failure while a run is still in flight, and
// the rollup once every run has finished. A new head commit resets silently.
func checksTransition(pr *PullRequest, runs []*github.CheckRun) (*PRActivity, ChecksState) {
	if len(runs) == 0 {
		return nil, ChecksStatePending
	}

	state := ChecksStatePassed
	var failed []string
	complete := true
	for _, run := range runs {
		if run.GetStatus() != "completed" {
			complete = false
			continue
		}
		switch run.GetConclusion() {
		case "success", "neutral", "skipped":
		default:
			failed = append(failed, run.GetName())
		}
	}
	switch {
	case len(failed) > 0 && complete:
		state = ChecksStateFailed
	case len(failed) > 0:
		state = ChecksStateFailing
	case !complete:
		state = ChecksStatePending
	}

	sameCommit := pr.ChecksHeadSHA == pr.HeadSHA
	if sameCommit && state == pr.ChecksState {
		return nil, state
	}
	if state == ChecksStatePending {
		return nil, state
	}
	// first sight of this PR only records a baseline, so adopting a batch of
	// PRs does not fire a rollup for each one at once. The connect snapshot
	// carries the current state instead.
	if pr.ChecksHeadSHA == "" {
		return nil, state
	}
	// a run that fails and finishes within one poll only needs the rollup
	if state == ChecksStateFailing && sameCommit && pr.ChecksState.terminal() {
		return nil, state
	}

	return &PRActivity{Type: ActivityChecks, Data: ChecksActivity{
		Number: pr.Number,
		Title:  pr.Title,
		State:  string(state),
		Failed: failed,
		URL:    pr.HTMLURL,
	}}, state
}

func (s *GitService) ignoredAuthor(login, userType string) bool {
	if login == "" {
		return true
	}
	if login == s.currentUser {
		return true
	}
	return userType == "Bot" || strings.HasSuffix(login, "[bot]")
}

func truncate(body string) string {
	body = strings.TrimSpace(body)
	runes := []rune(body)
	if len(runes) <= bodyLimit {
		return body
	}
	return string(runes[:bodyLimit]) + "…"
}
