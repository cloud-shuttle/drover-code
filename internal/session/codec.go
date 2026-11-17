package session

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cloudshuttle/drover-code/internal/api"
)

type fileTranscript struct {
	ID       string        `json:"id"`
	WorkDir  string        `json:"workdir"`
	Updated  time.Time     `json:"updated"`
	Messages []fileMessage `json:"messages"`
}

type fileMessage struct {
	Role    string      `json:"role"`
	Content []fileBlock `json:"content"`
}

type fileBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Images    []fileImage     `json:"images,omitempty"`
}

type fileImage struct {
	MIME   string `json:"mime"`
	Base64 string `json:"base64"`
}

func encodeTranscript(id, workDir string, updated time.Time, msgs []api.Message) ([]byte, error) {
	fileMsgs := make([]fileMessage, len(msgs))
	for i, msg := range msgs {
		role := "user"
		if msg.Role == api.RoleAssistant {
			role = "assistant"
		}
		blocks := make([]fileBlock, len(msg.Content))
		for j, block := range msg.Content {
			encoded, err := encodeBlock(block)
			if err != nil {
				return nil, err
			}
			blocks[j] = encoded
		}
		fileMsgs[i] = fileMessage{Role: role, Content: blocks}
	}
	payload, err := json.MarshalIndent(fileTranscript{
		ID:       id,
		WorkDir:  workDir,
		Updated:  updated,
		Messages: fileMsgs,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func encodeBlock(block api.ContentBlock) (fileBlock, error) {
	switch b := block.(type) {
	case api.TextBlock:
		return fileBlock{Type: "text", Text: b.Text}, nil
	case api.ToolUseBlock:
		return fileBlock{Type: "tool_use", ID: b.ID, Name: b.Name, Input: b.Input}, nil
	case api.ToolResultBlock:
		images := make([]fileImage, len(b.Images))
		for i, img := range b.Images {
			images[i] = fileImage{MIME: img.MIME, Base64: img.Base64}
		}
		return fileBlock{
			Type:      "tool_result",
			ToolUseID: b.ToolUseID,
			Content:   b.Content,
			IsError:   b.IsError,
			Images:    images,
		}, nil
	default:
		return fileBlock{}, fmt.Errorf("session: unknown content block %T", block)
	}
}

func readTranscript(path string) (Transcript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Transcript{}, err
	}
	var file fileTranscript
	if err := json.Unmarshal(data, &file); err != nil {
		return Transcript{}, fmt.Errorf("session %s: %w", path, err)
	}
	msgs := make([]api.Message, len(file.Messages))
	for i, msg := range file.Messages {
		role := api.RoleUser
		switch msg.Role {
		case "user":
		case "assistant":
			role = api.RoleAssistant
		default:
			return Transcript{}, fmt.Errorf("session %s: unknown role %q", path, msg.Role)
		}
		blocks := make([]api.ContentBlock, len(msg.Content))
		for j, block := range msg.Content {
			decoded, err := decodeBlock(block)
			if err != nil {
				return Transcript{}, fmt.Errorf("session %s: %w", path, err)
			}
			blocks[j] = decoded
		}
		msgs[i] = api.Message{Role: role, Content: blocks}
	}
	return Transcript{
		ID:       file.ID,
		WorkDir:  file.WorkDir,
		Updated:  file.Updated,
		Messages: msgs,
	}, nil
}

func decodeBlock(block fileBlock) (api.ContentBlock, error) {
	switch block.Type {
	case "text":
		return api.TextBlock{Text: block.Text}, nil
	case "tool_use":
		return api.ToolUseBlock{ID: block.ID, Name: block.Name, Input: block.Input}, nil
	case "tool_result":
		images := make([]api.ToolImage, len(block.Images))
		for i, img := range block.Images {
			images[i] = api.ToolImage{MIME: img.MIME, Base64: img.Base64}
		}
		return api.ToolResultBlock{
			ToolUseID: block.ToolUseID,
			Content:   block.Content,
			IsError:   block.IsError,
			Images:    images,
		}, nil
	default:
		return nil, fmt.Errorf("unknown content block %q", block.Type)
	}
}
