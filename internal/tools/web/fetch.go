// Package web implements the web_fetch tool.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const (
	fetchTimeout  = 30 * time.Second
	maxFetchBytes = 2 * 1024 * 1024 // 2 MB response cap
	userAgent     = "drover-code/1.0"
)

// Fetch fetches a URL and returns its content as plain text.
type Fetch struct {
	client *http.Client
}

func NewFetch() *Fetch {
	return &Fetch{
		client: &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

type fetchInput struct {
	URL string `json:"url"`
	Raw bool   `json:"raw"`
}

func (t *Fetch) Name() string { return "web_fetch" }
func (t *Fetch) Description() string {
	return "Fetch the content of a URL and return it as plain text. " +
		"HTML pages are converted to readable text. " +
		"Useful for reading documentation, checking APIs, or researching online."
}
func (t *Fetch) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("url", toolutil.NewSchema("string").Desc("URL to fetch. Must include scheme (https:// or http://)")).
		Prop("raw", toolutil.NewSchema("boolean").Desc("Return raw body without HTML stripping (default: false)")).
		Required("url").
		Build()
}
func (t *Fetch) NeedsPermission(_ json.RawMessage) bool { return false }

func (t *Fetch) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp fetchInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("web_fetch: bad input: %w", err)
	}
	if inp.URL == "" {
		return "", fmt.Errorf("web_fetch: url cannot be empty")
	}
	if !strings.HasPrefix(inp.URL, "http://") && !strings.HasPrefix(inp.URL, "https://") {
		return "", fmt.Errorf("web_fetch: url must start with http:// or https://")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inp.URL, nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("web_fetch: HTTP %d for %s", resp.StatusCode, inp.URL)
	}

	limited := io.LimitReader(resp.Body, maxFetchBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("web_fetch: read body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	result := string(body)

	if !inp.Raw && strings.Contains(contentType, "text/html") {
		result = htmlToText(result)
	}

	if !utf8.ValidString(result) {
		result = strings.ToValidUTF8(result, "")
	}

	header := fmt.Sprintf("URL: %s\nStatus: %d\nContent-Type: %s\n\n",
		inp.URL, resp.StatusCode, contentType)

	return toolutil.Truncate(header + result), nil
}

func htmlToText(html string) string {
	var b strings.Builder
	inTag := false
	inScript := false
	inStyle := false

	i := 0
	runes := []rune(html)
	n := len(runes)

	for i < n {
		r := runes[i]

		if inTag {
			if r == '>' {
				inTag = false
			}
			i++
			continue
		}

		if r == '<' {
			remaining := string(runes[i:])
			low := strings.ToLower(remaining)
			switch {
			case strings.HasPrefix(low, "<script"):
				inScript = true
			case strings.HasPrefix(low, "</script"):
				inScript = false
			case strings.HasPrefix(low, "<style"):
				inStyle = true
			case strings.HasPrefix(low, "</style"):
				inStyle = false
			case strings.HasPrefix(low, "<br") || strings.HasPrefix(low, "<p") ||
				strings.HasPrefix(low, "<div") || strings.HasPrefix(low, "<li") ||
				strings.HasPrefix(low, "<tr") || strings.HasPrefix(low, "<h"):
				b.WriteRune('\n')
			}
			inTag = true
			i++
			continue
		}

		if inScript || inStyle {
			i++
			continue
		}

		b.WriteRune(r)
		i++
	}

	result := b.String()
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", `"`)
	result = strings.ReplaceAll(result, "&#39;", "'")
	result = strings.ReplaceAll(result, "&nbsp;", " ")

	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}

