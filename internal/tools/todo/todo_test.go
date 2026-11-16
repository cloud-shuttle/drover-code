package todo

import (
	"context"
	"encoding/json"
	"testing"
)

func TestList_Replace(t *testing.T) {
	var list List
	out, err := list.Execute(context.Background(), json.RawMessage(`{"items":[{"text":"Read Paper","done":false},{"text":"Write notes","done":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "Todo:\n- [ ] Read Paper\n- [x] Write notes" {
		t.Fatalf("out %q", out)
	}
}
