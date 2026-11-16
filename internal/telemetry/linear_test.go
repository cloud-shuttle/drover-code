package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExtractIssueID(t *testing.T) {
	// Test flag precedence
	if id := ExtractIssueID(".", "CLO-999"); id != "CLO-999" {
		t.Errorf("expected CLO-999 from flag, got %s", id)
	}

	// Test branch parsing using a temp git repo
	// Need to initialize a git repo to test branch parsing
	// but we don't want to rely on git being installed or configuring user.name for tests.
	// We will just unit test the regex part manually here if needed.
}

func TestParseIssueIDFromBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/CLO-123-telemetry", "CLO-123"},
		{"CLO-123-telemetry", "CLO-123"},
		{"bugfix/proj-45-fix", "PROJ-45"},
		{"main", ""},
		{"clo-abc", ""},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := parseIssueIDFromBranch(tt.branch)
			if got != tt.want {
				t.Errorf("parseIssueIDFromBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

type mockTransport struct {
	reqFunc func(req *http.Request) *http.Response
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.reqFunc(req), nil
}

func TestLinearClient_GetIssueDetails(t *testing.T) {
	transport := &mockTransport{
		reqFunc: func(r *http.Request) *http.Response {
			if r.Header.Get("Authorization") != "test-key" {
				t.Errorf("missing or wrong auth header")
			}

			var req graphqlRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}

			if req.Variables["identifier"] != "CLO-187" {
				t.Errorf("wrong identifier sent")
			}

			body := `{
				"data": {
					"issue": {
						"id": "uuid-123",
						"team": {
							"states": {
								"nodes": [
									{"id": "state-1", "name": "Todo"},
									{"id": "state-2", "name": "In Progress"}
								]
							}
						}
					}
				}
			}`
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}
		},
	}

	client := &LinearClient{
		apiKey: "test-key",
		client: &http.Client{Transport: transport},
	}

	id, states, err := client.GetIssueDetails(context.Background(), "CLO-187")
	if err != nil {
		t.Fatal(err)
	}

	if id != "uuid-123" {
		t.Errorf("expected uuid-123, got %s", id)
	}

	if states["in progress"] != "state-2" {
		t.Errorf("expected state-2, got %s", states["in progress"])
	}
}
