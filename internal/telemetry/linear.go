package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// LinearClient handles telemetry syncing to Linear.
type LinearClient struct {
	apiKey string
	client *http.Client
}

// NewLinearClient creates a new client if LINEAR_API_KEY is set.
func NewLinearClient() *LinearClient {
	key := os.Getenv("LINEAR_API_KEY")
	if key == "" {
		return nil
	}
	return &LinearClient{
		apiKey: key,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func (c *LinearClient) doGraphQL(ctx context.Context, query string, variables map[string]any, result any) error {
	reqBody, err := json.Marshal(graphqlRequest{
		Query:     query,
		Variables: variables,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.linear.app/graphql", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("linear api returned status %d", resp.StatusCode)
	}

	var res struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return err
	}

	if len(res.Errors) > 0 {
		return fmt.Errorf("linear graphql error: %s", res.Errors[0].Message)
	}

	if result != nil && len(res.Data) > 0 {
		return json.Unmarshal(res.Data, result)
	}

	return nil
}

// GetIssueDetails fetches the internal UUID of an issue (e.g. "CLO-187") and available workflow states.
func (c *LinearClient) GetIssueDetails(ctx context.Context, identifier string) (internalID string, states map[string]string, err error) {
	query := `query GetIssueInfo($identifier: String!) {
		issue(id: $identifier) {
			id
			team {
				states {
					nodes {
						id
						name
					}
				}
			}
		}
	}`

	var res struct {
		Issue struct {
			ID   string `json:"id"`
			Team struct {
				States struct {
					Nodes []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}

	if err := c.doGraphQL(ctx, query, map[string]any{"identifier": identifier}, &res); err != nil {
		return "", nil, err
	}

	if res.Issue.ID == "" {
		return "", nil, fmt.Errorf("issue not found")
	}

	statesMap := make(map[string]string)
	for _, n := range res.Issue.Team.States.Nodes {
		statesMap[strings.ToLower(n.Name)] = n.ID
	}

	return res.Issue.ID, statesMap, nil
}

// UpdateStatus moves the issue to the desired state if it exists.
func (c *LinearClient) UpdateStatus(ctx context.Context, internalID, stateID string) error {
	query := `mutation UpdateIssueStatus($issueId: String!, $stateId: String!) {
		issueUpdate(id: $issueId, input: { stateId: $stateId }) {
			success
		}
	}`

	return c.doGraphQL(ctx, query, map[string]any{
		"issueId": internalID,
		"stateId": stateID,
	}, nil)
}

// LinkTrace posts a comment with the Langfuse trace URL.
func (c *LinearClient) LinkTrace(ctx context.Context, internalID, traceID string) error {
	query := `mutation CreateComment($issueId: String!, $body: String!) {
		commentCreate(input: { issueId: $issueId, body: $body }) {
			success
		}
	}`

	body := fmt.Sprintf("🤖 **Agent Session Started**\n\nLangfuse Trace ID: `%s`", traceID)

	return c.doGraphQL(ctx, query, map[string]any{
		"issueId": internalID,
		"body":    body,
	}, nil)
}

// ExtractIssueID attempts to find a Linear issue ID (e.g., CLO-123) in a given string or git branch name.
func ExtractIssueID(workDir, providedFlag string) string {
	if providedFlag != "" {
		return providedFlag
	}

	// Try git branch
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", workDir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	branch := strings.TrimSpace(string(out))
	return parseIssueIDFromBranch(branch)
}

// parseIssueIDFromBranch extracts a Linear-style key (e.g. CLO-123) from a git branch name.
func parseIssueIDFromBranch(branch string) string {
	re := regexp.MustCompile(`([A-Za-z]+-\d+)`)
	match := re.FindString(branch)
	if match != "" {
		return strings.ToUpper(match)
	}
	return ""
}
