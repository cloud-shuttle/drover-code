package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestBuildGitHubSystemPrompt_issue(t *testing.T) {
	tr := &Trigger{
		Context: TriggerContext{
			Repo:        Repository{FullName: "acme/shop"},
			IssueNumber: 7,
			IssuTitle:   "Bug",
			IssueBody:   "steps to repro",
		},
	}
	s := buildGitHubSystemPrompt(tr, "/tmp/job")
	if !strings.Contains(s, "acme/shop") || !strings.Contains(s, "Issue #7") || !strings.Contains(s, "steps") {
		t.Fatalf("%s", s)
	}
}

func TestBuildGitHubSystemPrompt_pullRequest(t *testing.T) {
	tr := &Trigger{
		Context: TriggerContext{
			Repo:      Repository{FullName: "acme/shop"},
			PRNumber:  12,
			IssuTitle: "Feature",
			PRHead:    "feat",
			PRBase:    "main",
		},
	}
	s := buildGitHubSystemPrompt(tr, "/w")
	if !strings.Contains(s, "Pull Request #12") || !strings.Contains(s, "feat") {
		t.Fatalf("%s", s)
	}
}

func TestBuildGitHubSystemPrompt_reviewDiff(t *testing.T) {
	tr := &Trigger{
		Context: TriggerContext{
			Repo:        Repository{FullName: "acme/shop"},
			IssueNumber: 1,
			IssuTitle:   "PR",
			DiffContext: "+fix",
			FilePath:    "a.go",
			DiffLine:    10,
		},
	}
	s := buildGitHubSystemPrompt(tr, "/w")
	if !strings.Contains(s, "a.go") || !strings.Contains(s, "+fix") {
		t.Fatalf("%s", s)
	}
}

func TestTruncate_runner(t *testing.T) {
	if got := truncate("  hi  ", 10); got != "hi" {
		t.Fatalf("%q", got)
	}
	long := strings.Repeat("α", 50)
	got := truncate(long, 5)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("%q", got)
	}
}

func TestRunner_Handle_postPlaceholderFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := NewClient("tok")
	c.apiBaseURL = srv.URL
	r := NewRunner(c, nil, t.TempDir())

	err := r.Handle(context.Background(), &Trigger{
		Request: "hello",
		Context: TriggerContext{
			Repo: Repository{FullName: "o/r", DefaultBranch: "main"},
		},
		ReplyTarget: ReplyTarget{
			Owner:  "o",
			Repo:   "r",
			Number: 3,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunner_cloneRepo_localFileURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	origin := t.TempDir()
	runGit(t, origin, "init")
	runGit(t, origin, "config", "user.email", "runner@test.local")
	runGit(t, origin, "config", "user.name", "runner test")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, origin, "add", "README.md")
	runGit(t, origin, "commit", "-m", "init")
	runGit(t, origin, "branch", "-M", "main")

	absOrigin, err := filepath.Abs(origin)
	if err != nil {
		t.Fatal(err)
	}
	cloneURL := "file://" + absOrigin

	workBase := t.TempDir()
	gh := NewClient("") // no token; file:// clone is unchanged
	r := NewRunner(gh, nil, workBase)

	dir, cleanup, err := r.cloneRepo(context.Background(), &Trigger{
		ReplyTarget: ReplyTarget{Number: 7},
		Context: TriggerContext{
			Repo: Repository{
				FullName:      "fixture/local",
				CloneURL:      cloneURL,
				DefaultBranch: "main",
			},
		},
	})
	if err != nil {
		t.Fatalf("cloneRepo: %v", err)
	}
	t.Cleanup(cleanup)

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("cloned worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git missing: %v", err)
	}
}

func TestRunner_run_localRepoMockAPI(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	origin := t.TempDir()
	runGit(t, origin, "init")
	runGit(t, origin, "config", "user.email", "runner@test.local")
	runGit(t, origin, "config", "user.name", "runner test")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(origin, ".drover"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, ".drover", "settings.json"), []byte(`{"contextLimitEstimate":50000,"disableAutoCompaction":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, origin, "add", "README.md", ".drover/settings.json")
	runGit(t, origin, "commit", "-m", "init")
	runGit(t, origin, "branch", "-M", "main")

	absOrigin, err := filepath.Abs(origin)
	if err != nil {
		t.Fatal(err)
	}
	cloneURL := "file://" + absOrigin

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"mock reply"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	t.Cleanup(srv.Close)

	apiClient := api.NewClient("test-key", "claude-test-model")
	apiClient.SetBaseURL(srv.URL)

	gh := NewClient("")
	r := NewRunner(gh, apiClient, t.TempDir())

	out, err := r.run(context.Background(), &Trigger{
		Request: "say hi",
		ReplyTarget: ReplyTarget{
			Number: 1,
		},
		Context: TriggerContext{
			Repo: Repository{
				FullName:      "fixture/local",
				CloneURL:      cloneURL,
				DefaultBranch: "main",
			},
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "mock reply") {
		t.Fatalf("output: %q", out)
	}
	if !strings.Contains(out, "drover-code_") {
		t.Fatalf("expected footer: %q", out)
	}
}

func TestRunner_Handle_successPostPatchAndRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	origin := t.TempDir()
	runGit(t, origin, "init")
	runGit(t, origin, "config", "user.email", "runner@test.local")
	runGit(t, origin, "config", "user.name", "runner test")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, origin, "add", "README.md")
	runGit(t, origin, "commit", "-m", "init")
	runGit(t, origin, "branch", "-M", "main")

	absOrigin, err := filepath.Abs(origin)
	if err != nil {
		t.Fatal(err)
	}
	cloneURL := "file://" + absOrigin

	var patchBody string
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":9001}`)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/comments/9001"):
			b, _ := io.ReadAll(r.Body)
			patchBody = string(b)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected GitHub request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ghSrv.Close)

	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected anthropic path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"mock reply"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	t.Cleanup(anthSrv.Close)

	gh := NewClient("tok")
	gh.apiBaseURL = ghSrv.URL
	apiClient := api.NewClient("k", "m")
	apiClient.SetBaseURL(anthSrv.URL)
	r := NewRunner(gh, apiClient, t.TempDir())

	err = r.Handle(context.Background(), &Trigger{
		Request: "say hi",
		ReplyTarget: ReplyTarget{
			Owner:  "o",
			Repo:   "r",
			Number: 7,
		},
		Context: TriggerContext{
			Repo: Repository{
				FullName:      "o/r",
				CloneURL:      cloneURL,
				DefaultBranch: "main",
			},
		},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if patchBody == "" || !strings.Contains(patchBody, "mock reply") {
		t.Fatalf("expected final comment to include model text, got %q", patchBody)
	}
}

func TestRunner_Handle_patchFailsAfterSuccessfulRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	origin := t.TempDir()
	runGit(t, origin, "init")
	runGit(t, origin, "config", "user.email", "runner@test.local")
	runGit(t, origin, "config", "user.name", "runner test")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, origin, "add", "README.md")
	runGit(t, origin, "commit", "-m", "init")
	runGit(t, origin, "branch", "-M", "main")

	absOrigin, err := filepath.Abs(origin)
	if err != nil {
		t.Fatal(err)
	}
	cloneURL := "file://" + absOrigin

	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":9001}`)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/comments/9001"):
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		default:
			t.Errorf("unexpected GitHub request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ghSrv.Close)

	anthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"ok"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	t.Cleanup(anthSrv.Close)

	gh := NewClient("tok")
	gh.apiBaseURL = ghSrv.URL
	apiClient := api.NewClient("k", "m")
	apiClient.SetBaseURL(anthSrv.URL)
	r := NewRunner(gh, apiClient, t.TempDir())

	err = r.Handle(context.Background(), &Trigger{
		Request: "x",
		ReplyTarget: ReplyTarget{
			Owner:  "o",
			Repo:   "r",
			Number: 7,
		},
		Context: TriggerContext{
			Repo: Repository{
				FullName:      "o/r",
				CloneURL:      cloneURL,
				DefaultBranch: "main",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "update") {
		t.Fatalf("expected update error, got %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
