package github

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventIssueComment    EventType = "issue_comment"
	EventPRReview        EventType = "pull_request_review"
	EventPRReviewComment EventType = "pull_request_review_comment"
	EventPullRequest     EventType = "pull_request"
)

type User struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

type Repository struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Issue struct {
	Number  int     `json:"number"`
	Title   string  `json:"title"`
	Body    string  `json:"body"`
	State   string  `json:"state"`
	HTMLURL string  `json:"html_url"`
	User    User    `json:"user"`
	Labels  []Label `json:"labels"`
}

type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	User    User   `json:"user"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
	Draft     bool  `json:"draft"`
	Mergeable *bool `json:"mergeable"`
}

type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DiffComment struct {
	Comment
	Path         string `json:"path"`
	Line         int    `json:"line"`
	DiffHunk     string `json:"diff_hunk"`
	CommitID     string `json:"commit_id"`
	OriginalLine int    `json:"original_line"`
}

type IssueCommentEvent struct {
	Action     string     `json:"action"`
	Comment    Comment    `json:"comment"`
	Issue      Issue      `json:"issue"`
	Repository Repository `json:"repository"`
	Sender     User       `json:"sender"`
	PullRequest *PullRequest `json:"pull_request,omitempty"`
}

type PRReviewCommentEvent struct {
	Action      string      `json:"action"`
	Comment     DiffComment `json:"comment"`
	PullRequest PullRequest `json:"pull_request"`
	Repository  Repository  `json:"repository"`
	Sender      User        `json:"sender"`
}

type PullRequestEvent struct {
	Action      string      `json:"action"`
	Number      int         `json:"number"`
	PullRequest PullRequest `json:"pull_request"`
	Repository  Repository  `json:"repository"`
	Sender      User        `json:"sender"`
}

type Trigger struct {
	Request     string
	Context     TriggerContext
	ReplyTarget ReplyTarget
}

type TriggerContext struct {
	Repo        Repository
	IssueNumber int
	PRNumber    int
	PRHead      string
	PRBase      string
	IssuTitle   string
	IssueBody   string
	CommentBody string
	DiffContext string
	FilePath    string
	DiffLine    int
}

type ReplyTarget struct {
	Kind      ReplyKind
	Owner     string
	Repo      string
	Number    int
	CommentID int64
	CommitSHA string
	FilePath  string
	Line      int
}

type ReplyKind int

const (
	ReplyIssueComment ReplyKind = iota
	ReplyReviewComment
)

type ParsedEvent struct {
	Type    EventType
	Trigger *Trigger
	Raw     json.RawMessage
}

