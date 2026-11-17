package api

import (
	"encoding/base64"
	"strings"
)

// ToolImage is a picture attached to a tool result.
type ToolImage struct {
	MIME   string
	Base64 string
}

// PackImage appends one image to a short text note. UnpackImages removes it
// before the note is stored on the conversation.
func PackImage(note, mime string, data []byte) string {
	return strings.TrimRight(note, "\n") + "\n@@image " + mime + " " + base64.StdEncoding.EncodeToString(data)
}

// UnpackImages splits packed image lines out of a tool result.
func UnpackImages(s string) (string, []ToolImage) {
	var text strings.Builder
	var images []ToolImage
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(line, "@@image ") {
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(line)
			continue
		}
		rest := strings.TrimPrefix(line, "@@image ")
		mime, b64, ok := strings.Cut(rest, " ")
		if !ok || mime == "" || b64 == "" {
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(line)
			continue
		}
		images = append(images, ToolImage{MIME: mime, Base64: b64})
	}
	return strings.TrimRight(text.String(), "\n"), images
}

func responsesOutput(r ToolResultBlock) any {
	text, images := r.Content, r.Images
	if len(images) == 0 {
		text, images = UnpackImages(text)
	}
	if r.IsError && text != "" && !strings.HasPrefix(strings.ToLower(text), "error") {
		text = "Error: " + text
	}
	if len(images) == 0 {
		return text
	}
	parts := []any{map[string]any{"type": "input_text", "text": text}}
	for _, im := range images {
		parts = append(parts, map[string]any{
			"type":      "input_image",
			"image_url": "data:" + im.MIME + ";base64," + im.Base64,
		})
	}
	return parts
}

func chatToolContent(r ToolResultBlock) any {
	text, images := r.Content, r.Images
	if len(images) == 0 {
		text, images = UnpackImages(text)
	}
	if r.IsError && text != "" && !strings.HasPrefix(strings.ToLower(text), "error") {
		text = "Error: " + text
	}
	if len(images) == 0 {
		return text
	}
	parts := []any{map[string]any{"type": "text", "text": text}}
	for _, im := range images {
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:" + im.MIME + ";base64," + im.Base64,
			},
		})
	}
	return parts
}
