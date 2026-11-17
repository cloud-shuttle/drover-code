// Package browser drives a local headless Chrome.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const (
	maxSnapshot = 8_000
	maxImage    = 2 << 20
)

// Browser is one headless Chrome for a local session.
// ponytail: one Chrome at a time; a second page when two pages must stay open together.
type Browser struct {
	mu   sync.Mutex
	sess *session
}

func (t *Browser) Name() string { return "browser" }

func (t *Browser) Description() string {
	return "Open a page in a local headless Chrome and read or change it. " +
		"Actions are open, snapshot, click, type, press, screenshot, and close. " +
		"open takes an http or https url. click and type take a CSS selector. " +
		"screenshot returns a PNG of the page. Network is allowed here. Local sessions only."
}

func (t *Browser) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("action", toolutil.NewSchema("string").Desc("open, snapshot, click, type, press, screenshot, or close").Enum("open", "snapshot", "click", "type", "press", "screenshot", "close")).
		Prop("url", toolutil.NewSchema("string").Desc("http or https URL for open")).
		Prop("selector", toolutil.NewSchema("string").Desc("CSS selector for click or type")).
		Prop("text", toolutil.NewSchema("string").Desc("Text to put into an input or textarea")).
		Prop("key", toolutil.NewSchema("string").Desc("Key name for press, such as Enter, Tab, or Escape")).
		Required("action").
		Build()
}

func (t *Browser) NeedsPermission(json.RawMessage) bool { return true }

func (t *Browser) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sess != nil {
		t.sess.Close()
		t.sess = nil
	}
}

type browserInput struct {
	Action   string `json:"action"`
	URL      string `json:"url"`
	Selector string `json:"selector"`
	Text     string `json:"text"`
	Key      string `json:"key"`
}

func (t *Browser) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil {
		return "", fmt.Errorf("browser: not available in this session")
	}
	var in browserInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("browser: bad input: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	t.mu.Lock()
	defer t.mu.Unlock()
	if action == "close" {
		if t.sess != nil {
			t.sess.Close()
			t.sess = nil
		}
		return "browser closed", nil
	}
	if action == "open" {
		if err := allowedURL(in.URL); err != nil {
			return "", err
		}
	}
	if t.sess == nil {
		if action != "open" {
			return "", fmt.Errorf("browser: open a page first")
		}
		sess, err := startChrome(ctx)
		if err != nil {
			return "", err
		}
		t.sess = sess
	}
	switch action {
	case "open":
		if err := t.sess.navigate(ctx, strings.TrimSpace(in.URL)); err != nil {
			t.fail()
			return "", err
		}
		return t.snapshot(ctx)
	case "snapshot":
		return t.snapshot(ctx)
	case "click":
		return t.click(ctx, in.Selector)
	case "type":
		return t.typeText(ctx, in.Selector, in.Text)
	case "press":
		return t.press(ctx, in.Key)
	case "screenshot":
		return t.screenshot(ctx)
	default:
		return "", fmt.Errorf("browser: unknown action %q", in.Action)
	}
}

func (t *Browser) fail() {
	if t.sess != nil {
		t.sess.Close()
		t.sess = nil
	}
}

func allowedURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("browser: url must be http or https")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("browser: url must be http or https")
	}
}

func (t *Browser) snapshot(ctx context.Context) (string, error) {
	text, err := t.sess.eval(ctx, `(document.title||"") + "\n" + location.href + "\n" + (document.body ? document.body.innerText : "")`)
	if err != nil {
		t.fail()
		return "", err
	}
	return clip(text, maxSnapshot), nil
}

func (t *Browser) click(ctx context.Context, selector string) (string, error) {
	sel, err := jsString(selector)
	if err != nil {
		return "", err
	}
	expr := fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return "not found"; el.click(); return "clicked"; })()`, sel)
	out, err := t.sess.eval(ctx, expr)
	if err != nil {
		t.fail()
		return "", err
	}
	if out == "not found" {
		return "", fmt.Errorf("browser: selector not found")
	}
	return out, nil
}

func (t *Browser) typeText(ctx context.Context, selector, text string) (string, error) {
	sel, err := jsString(selector)
	if err != nil {
		return "", err
	}
	jsText, err := jsString(text)
	if err != nil {
		return "", err
	}
	// ponytail: sets input and textarea values; add contenteditable when a page needs it.
	expr := fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) return "not found"; if (!("value" in el)) return "not an input"; el.focus(); el.value = %s; el.dispatchEvent(new Event("input", {bubbles:true})); el.dispatchEvent(new Event("change", {bubbles:true})); return "typed"; })()`, sel, jsText)
	out, err := t.sess.eval(ctx, expr)
	if err != nil {
		t.fail()
		return "", err
	}
	if out == "not found" {
		return "", fmt.Errorf("browser: selector not found")
	}
	if out == "not an input" {
		return "", fmt.Errorf("browser: selector is not an input")
	}
	return out, nil
}

func (t *Browser) press(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	code, vk, text, ok := keyInfo(key)
	if !ok {
		return "", fmt.Errorf("browser: unsupported key %q", key)
	}
	for _, typ := range []string{"keyDown", "keyUp"} {
		params := map[string]any{
			"type":                  typ,
			"key":                   code,
			"code":                  code,
			"windowsVirtualKeyCode": vk,
		}
		if typ == "keyDown" && text != "" {
			params["text"] = text
		}
		if _, err := t.sess.call(ctx, "Input.dispatchKeyEvent", params); err != nil {
			t.fail()
			return "", err
		}
	}
	return "pressed " + code, nil
}

func keyInfo(key string) (code string, vk int, text string, ok bool) {
	switch strings.ToLower(key) {
	case "enter", "return":
		return "Enter", 13, "\r", true
	case "tab":
		return "Tab", 9, "", true
	case "escape", "esc":
		return "Escape", 27, "", true
	case "backspace":
		return "Backspace", 8, "", true
	default:
		return "", 0, "", false
	}
}

func (t *Browser) screenshot(ctx context.Context) (string, error) {
	png, err := t.sess.screenshot(ctx)
	if err != nil {
		t.fail()
		return "", err
	}
	if len(png) > maxImage {
		return "", fmt.Errorf("browser: screenshot is too large")
	}
	note := fmt.Sprintf("Screenshot (%d bytes).", len(png))
	return api.PackImage(note, "image/png", png), nil
}

func jsString(s string) (string, error) {
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("browser: value is empty")
	}
	if utf8.RuneCountInString(s) > 8000 {
		return "", fmt.Errorf("browser: value is too long")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := range s {
		if i == n-1 {
			return s[:k] + "…"
		}
		i++
	}
	return s
}
