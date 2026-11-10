package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// Search queries the public web and returns titles, URLs, and snippets.
type Search struct {
	client   *http.Client
	endpoint string
}

func NewSearch() *Search {
	return &Search{
		client:   &http.Client{Timeout: fetchTimeout},
		endpoint: "https://html.duckduckgo.com/html/",
	}
}

func (t *Search) Name() string { return "web_search" }
func (t *Search) Description() string {
	return "Search the web. Returns up to 8 titles, URLs, and snippets. Use web_fetch to read a result."
}
func (t *Search) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("query", toolutil.NewSchema("string").Desc("Search query")).
		Required("query").
		Build()
}
func (t *Search) NeedsPermission(json.RawMessage) bool { return false }

type searchHit struct {
	Title   string
	URL     string
	Snippet string
}

func (t *Search) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("web_search: bad input: %w", err)
	}
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return "", fmt.Errorf("web_search: query cannot be empty")
	}
	form := url.Values{"q": {q}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("web_search: http %d", resp.StatusCode)
	}
	hits := ParseDuckDuckGo(string(body))
	if len(hits) == 0 {
		return "No results.", nil
	}
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, h.Title, h.URL, h.Snippet)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

var (
	resultLink = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	resultSnip = regexp.MustCompile(`(?s)<(?:a|td)[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</(?:a|td)>`)
	tagRe      = regexp.MustCompile(`<[^>]+>`)
)

// ParseDuckDuckGo extracts result rows from DuckDuckGo HTML.
func ParseDuckDuckGo(page string) []searchHit {
	links := resultLink.FindAllStringSubmatch(page, 8)
	snips := resultSnip.FindAllStringSubmatch(page, 8)
	out := make([]searchHit, 0, len(links))
	for i, m := range links {
		hit := searchHit{
			Title: cleanHTML(m[2]),
			URL:   cleanDDGURL(m[1]),
		}
		if i < len(snips) {
			hit.Snippet = cleanHTML(snips[i][1])
		}
		if hit.URL == "" || hit.Title == "" {
			continue
		}
		out = append(out, hit)
	}
	return out
}

func cleanHTML(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func cleanDDGURL(raw string) string {
	raw = html.UnescapeString(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		if decoded, err := url.QueryUnescape(uddg); err == nil {
			return decoded
		}
		return uddg
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return u.String()
	}
	return ""
}
