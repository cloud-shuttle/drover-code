package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClient_ListAndCall(t *testing.T) {
	var session string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     uint64         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess-1")
		} else if r.Header.Get("Mcp-Session-Id") != "sess-1" {
			http.Error(w, "session", http.StatusBadRequest)
			return
		}
		session = r.Header.Get("Mcp-Session-Id")
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize", "notifications/initialized":
			if req.ID == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + jsonID(req.ID) + `,"result":{"protocolVersion":"2025-03-26"}}`))
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + jsonID(req.ID) + `,"result":{"tools":[{"name":"get_guide","description":"read"}]}}`))
		case "tools/call":
			name, _ := req.Params["name"].(string)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + jsonID(req.ID) + `,"result":{"content":[{"type":"text","text":"called ` + name + `"}]}}`))
		default:
			http.Error(w, "method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, map[string]string{"Authorization": "Bearer secret"})
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := c.Call(ctx, "tools/list", map[string]any{}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "get_guide" {
		t.Fatalf("tools %#v", list.Tools)
	}
	tool := &MCPTool{client: c, name: "mcp_paper_get_guide", callName: "get_guide"}
	out, err := tool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "called get_guide" {
		t.Fatalf("out %q", out)
	}
	if session != "sess-1" {
		t.Fatalf("session %q", session)
	}
}

func TestParseRPC_SSE(t *testing.T) {
	raw := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"ok\":true}}\n\n")
	msg, err := parseRPC(raw, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg.Result), `"ok":true`) {
		t.Fatalf("result %s", msg.Result)
	}
}

func jsonID(id uint64) string {
	b, _ := json.Marshal(id)
	return string(b)
}
