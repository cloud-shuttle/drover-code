package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponsesOutputIncludesImage(t *testing.T) {
	packed := PackImage("Image shot.png (image/png, 3 bytes).", "image/png", []byte{1, 2, 3})
	text, images := UnpackImages(packed)
	if text != "Image shot.png (image/png, 3 bytes)." || len(images) != 1 || images[0].MIME != "image/png" {
		t.Fatalf("unpack %q %#v", text, images)
	}
	msgs := marshalResponsesInput([]Message{
		ToolResultMessage([]ToolResultBlock{{
			ToolUseID: "call_1",
			Content:   text,
			Images:    images,
		}}),
	})
	raw, _ := json.Marshal(msgs)
	if !strings.Contains(string(raw), `"type":"input_image"`) || !strings.Contains(string(raw), "data:image/png;base64,") {
		t.Fatalf("wire %s", raw)
	}
	if strings.Contains(string(raw), "@@image") {
		t.Fatalf("marker leaked: %s", raw)
	}
}
