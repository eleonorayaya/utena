package git

import (
	"context"
	"slices"
	"strings"

	"github.com/google/go-github/v72/github"
)

// Monitor notification types for the PR domain. The payload structs below are
// the wire shapes Claude sees, so their json tags are a public contract.
const (
	NotificationPullRequest   = "pull_request"
	NotificationReview        = "pull_request_review"
	NotificationReviewComment = "pull_request_review_comment"
	NotificationChecks        = "ci_checks"
)

const bodyLimit = 600

type PRActivity struct {
	Type string
	Data any
}

type PRNotification struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	State         string `json:"state"`
	PreviousState string `json:"previous_state,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Checks        string `json:"checks,omitempty"`
	URL           string `json:"url"`
}

func NewPRNotification(pr *PullRequest, previous *PullRequest, branch string) PRNotification {
	n := PRNotification{
		Number: pr.Number,
		Title:  pr.Title,
		State:  string(pr.State),
		Branch: branch,
		Checks: string(pr.ChecksState),
		URL:    pr.HTMLURL,
	}
	if previous != nil && previous.State != pr.State {
		n.PreviousState = string(previous.State)
	}
	return n
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

	var activity []PRActivity
	updated := false

	reviews, err := s.githubClient.ListPRReviews(ctx, owner, name, pr.Number)
	if err != nil {
		return nil, err
	}
	if events, watermark := s.newReviews(pr, reviews); watermark > pr.LastReviewID {
		activity = append(activity, events...)
		pr.LastReviewID = watermark
		updated = true
	}

	comments, err := s.githubClient.ListPRReviewComments(ctx, owner, name, pr.Number)
	if err != nil {
		return nil, err
	}
	if events, watermark := s.newReviewComments(pr, comments); watermark > pr.LastReviewCommentID {
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
		out = append(out, PRActivity{Type: NotificationReview, Data: ReviewActivity{
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
		out = append(out, PRActivity{Type: NotificationReviewComment, Data: ReviewCommentActivity{
			Number: pr.Number,
			Title:  pr.Title,
			Author: author,
			Path:   comment.GetPath(),
			Line:   comment.GetLine(),
			Body:   truncate(comment.GetBody()),
			URL:    comment.GetHTMLURL(),
		}})
	}
	// ListPRReviewComments sorts newest first (ListReviews has no sort
	// option), so flip to reading order
	slices.Reverse(out)
	return out, watermark
}

// checksTransition reports the check state for the PR's head commit and the
// event worth sending: the first failure while a run is still in flight, and
// the rollup once every run has finished. A new head commit resets silently.
func checksTransition(pr *PullRequest, runs []*github.CheckRun) (*PRActivity, ChecksState) {
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

	state := ChecksStatePending
	switch {
	case len(failed) > 0 && complete:
		state = ChecksStateFailed
	case len(failed) > 0:
		state = ChecksStateFailing
	case complete && len(runs) > 0:
		state = ChecksStatePassed
	}

	previous := pr.ChecksState
	if pr.ChecksHeadSHA != pr.HeadSHA {
		previous = "" // a new head commit invalidates the old rollup
	}
	// Stay quiet while runs are in flight, on the first sight of a PR (the
	// connect snapshot carries current state, so adopting a batch of PRs does
	// not fire a rollup for each), on a repeat of what we already reported,
	// and when a run fails and finishes inside one poll — that needs only the
	// rollup.
	if state == ChecksStatePending || state == previous || pr.ChecksHeadSHA == "" ||
		(state == ChecksStateFailing && previous.terminal()) {
		return nil, state
	}

	return &PRActivity{Type: NotificationChecks, Data: ChecksActivity{
		Number: pr.Number,
		Title:  pr.Title,
		State:  string(state),
		Failed: failed,
		URL:    pr.HTMLURL,
	}}, state
}

func (s *GitService) ignoredAuthor(login, userType string) bool {
	return login == "" || login == s.currentUser ||
		userType == "Bot" || strings.HasSuffix(login, "[bot]")
}

func truncate(body string) string {
	body = strings.TrimSpace(body)
	runes := []rune(body)
	if len(runes) <= bodyLimit {
		return body
	}
	return string(runes[:bodyLimit]) + "…"
}
