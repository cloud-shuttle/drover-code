package api

import "encoding/json"

// ----------------------------------------------------------------------------
// Roles
// ----------------------------------------------------------------------------

// Role is either user or assistant.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ----------------------------------------------------------------------------
// Content blocks — the discriminated union for message content
// ----------------------------------------------------------------------------

// ContentBlock is a single piece of content in a message.
// Implementations: TextBlock, ToolUseBlock, ToolResultBlock.
type ContentBlock interface {
	isContentBlock()
}

// TextBlock is a plain text content block.
type TextBlock struct {
	Text string
}

func (TextBlock) isContentBlock() {}

// ToolUseBlock represents a tool call emitted by the model.
// Appears only in assistant messages.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolUseBlock) isContentBlock() {}

// ToolResultBlock carries the result of a tool execution back to the model.
// Appears only in user messages that follow an assistant tool use.
type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

func (ToolResultBlock) isContentBlock() {}

// ----------------------------------------------------------------------------
// Messages
// ----------------------------------------------------------------------------

// Message is a single turn in the conversation.
type Message struct {
	Role    Role
	Content []ContentBlock
}

// UserMessage constructs a simple text message from the user.
func UserMessage(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: []ContentBlock{TextBlock{Text: text}},
	}
}

// AssistantMessage constructs an assistant message from a slice of content blocks.
// The blocks are whatever came back from a streaming response: text and/or tool calls.
func AssistantMessage(blocks []ContentBlock) Message {
	return Message{
		Role:    RoleAssistant,
		Content: blocks,
	}
}

// ToolResultMessage constructs the user-turn message that delivers tool results
// back to the model after tool calls have been executed.
func ToolResultMessage(results []ToolResultBlock) Message {
	content := make([]ContentBlock, len(results))
	for i, r := range results {
		content[i] = r
	}
	return Message{
		Role:    RoleUser,
		Content: content,
	}
}

// ----------------------------------------------------------------------------
// Tool definitions — sent to the API so the model knows what tools exist
// ----------------------------------------------------------------------------

// ToolDefinition describes a single tool to the Anthropic API.
// InputSchema must be a valid JSON Schema object.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ----------------------------------------------------------------------------
// Stream events — emitted by the SSE stream reader
// ----------------------------------------------------------------------------

// StreamEvent is the discriminated union of all events that arrive
// on a streaming Anthropic API response.
type StreamEvent interface {
	isStreamEvent()
}

// Delta is the discriminated union of content carried inside a
// content_block_delta event.
type Delta interface {
	isDelta()
}

// TextDelta carries a text fragment during a streaming text block.
type TextDelta struct {
	Text string
}

func (TextDelta) isDelta() {}

// InputJSONDelta carries a JSON fragment during a streaming tool_use block.
// The fragments must be concatenated in order to form the complete JSON input.
type InputJSONDelta struct {
	PartialJSON string
}

func (InputJSONDelta) isDelta() {}

// ContentBlockStartEvent signals that a new content block has begun.
// ContentBlock will be a TextBlock (empty Text) or a ToolUseBlock (empty Input).
type ContentBlockStartEvent struct {
	Index        int
	ContentBlock ContentBlock
}

func (ContentBlockStartEvent) isStreamEvent() {}

// ContentBlockDeltaEvent carries an incremental update to a content block.
type ContentBlockDeltaEvent struct {
	Index int
	Delta Delta
}

func (ContentBlockDeltaEvent) isStreamEvent() {}

// ContentBlockStopEvent signals that a content block is complete.
type ContentBlockStopEvent struct {
	Index int
}

func (ContentBlockStopEvent) isStreamEvent() {}

// MessageDeltaEvent carries end-of-message metadata: stop reason and token usage.
type MessageDeltaEvent struct {
	StopReason   string
	InputTokens  int
	OutputTokens int
}

func (MessageDeltaEvent) isStreamEvent() {}

// MessageStopEvent is the final event in a stream.
type MessageStopEvent struct{}

func (MessageStopEvent) isStreamEvent() {}

// ----------------------------------------------------------------------------
// Usage
// ----------------------------------------------------------------------------

// Usage records token consumption for a single API call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

