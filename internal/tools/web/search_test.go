package web

import "testing"

func TestParseDuckDuckGo(t *testing.T) {
	page := `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fdocs">Example <b>Docs</b></a>
<a class="result__snippet">The docs page.</a>`
	hits := ParseDuckDuckGo(page)
	if len(hits) != 1 {
		t.Fatalf("hits %#v", hits)
	}
	if hits[0].Title != "Example Docs" || hits[0].URL != "https://example.com/docs" || hits[0].Snippet != "The docs page." {
		t.Fatalf("hit %#v", hits[0])
	}
}
