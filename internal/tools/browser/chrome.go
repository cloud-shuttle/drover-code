package browser

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type session struct {
	cmd  *exec.Cmd
	dir  string
	ws   *wsConn
	next int
}

func findChrome() (string, error) {
	var candidates []string
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		)
	}
	candidates = append(candidates, "google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome")
	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, nil
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("browser: Chrome is not installed")
}

func startChrome(ctx context.Context) (*session, error) {
	bin, err := findChrome()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "drover-browser-")
	if err != nil {
		return nil, fmt.Errorf("browser: %w", err)
	}
	cmd := exec.Command(bin,
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--user-data-dir="+dir,
		"--remote-debugging-port=0",
		"--remote-allow-origins=*",
		"about:blank",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("browser: %w", err)
	}
	s := &session{cmd: cmd, dir: dir}
	port, err := waitDevToolsPort(ctx, dir)
	if err != nil {
		s.Close()
		return nil, err
	}
	pageURL, err := pageSocket(ctx, port)
	if err != nil {
		s.Close()
		return nil, err
	}
	ws, err := dialWS(pageURL)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("browser: %w", err)
	}
	s.ws = ws
	if _, err := s.call(ctx, "Page.enable", map[string]any{}); err != nil {
		s.Close()
		return nil, err
	}
	if _, err := s.call(ctx, "Runtime.enable", map[string]any{}); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func waitDevToolsPort(ctx context.Context, dir string) (string, error) {
	path := filepath.Join(dir, "DevToolsActivePort")
	deadline := time.Now().Add(10 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		b, err := os.ReadFile(path)
		if err == nil {
			sc := bufio.NewScanner(strings.NewReader(string(b)))
			if sc.Scan() {
				port := strings.TrimSpace(sc.Text())
				if port != "" {
					return port, nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", fmt.Errorf("browser: Chrome did not open a debugging port")
}

func pageSocket(ctx context.Context, port string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/json/list", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var targets []struct {
				Type                 string `json:"type"`
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			if json.Unmarshal(body, &targets) == nil {
				for _, t := range targets {
					if t.Type == "page" && t.WebSocketDebuggerURL != "" {
						return t.WebSocketDebuggerURL, nil
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/json/list", nil)
	}
	return "", fmt.Errorf("browser: Chrome did not open a page")
}

func (s *session) Close() {
	if s == nil {
		return
	}
	if s.ws != nil {
		_ = s.ws.Close()
	}
	killProcess(s.cmd)
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}

type cdpError struct {
	Message string `json:"message"`
}

func (s *session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.next++
	id := s.next
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := s.ws.writeText(payload); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(20 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		msg, err := s.ws.readText(deadline)
		if err != nil {
			return nil, fmt.Errorf("browser: %s: %w", method, err)
		}
		var head struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *cdpError       `json:"error"`
		}
		if json.Unmarshal(msg, &head) != nil || head.ID == 0 {
			continue
		}
		if head.ID != id {
			continue
		}
		if head.Error != nil {
			return nil, fmt.Errorf("browser: %s: %s", method, head.Error.Message)
		}
		return head.Result, nil
	}
}

func (s *session) navigate(ctx context.Context, rawURL string) error {
	if _, err := s.call(ctx, "Page.navigate", map[string]any{"url": rawURL}); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		val, err := s.eval(ctx, "document.readyState")
		if err == nil && (val == "complete" || val == "interactive") {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("browser: page did not load")
}

func (s *session) eval(ctx context.Context, expression string) (string, error) {
	raw, err := s.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.ExceptionDetails != nil {
		return "", fmt.Errorf("browser: %s", result.ExceptionDetails.Text)
	}
	var value string
	if err := json.Unmarshal(result.Result.Value, &value); err != nil {
		return "", fmt.Errorf("browser: script did not return text")
	}
	return value, nil
}

func (s *session) screenshot(ctx context.Context) ([]byte, error) {
	raw, err := s.call(ctx, "Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return nil, err
	}
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(result.Data)
}
