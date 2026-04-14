// Package tools defines the Tool interface and Registry.
// Individual tool implementations (bash, file ops, search, etc.) live in
// sub-packages and register themselves; only the registry is imported by
// the agent loop.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudshuttle/drover-code/internal/api"
)

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	NeedsPermission(input json.RawMessage) bool
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry holds all registered tools and provides the interface used by
// the agent loop.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		panic(fmt.Sprintf("tools: duplicate registration for %q", t.Name()))
	}
	r.tools[t.Name()] = t
}

// Get returns the named tool, or nil if not found.
func (r *Registry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Definitions returns the ToolDefinition slice sent to the Anthropic API.
func (r *Registry) Definitions() []api.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]api.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, api.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}

// Execute dispatches a tool call by name and returns its output.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	return t.Execute(ctx, input)
}

// NeedsPermission reports whether the named tool requires a permission prompt
// for the given input. Returns false if the tool is not registered.
func (r *Registry) NeedsPermission(name string, input json.RawMessage) bool {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	return t.NeedsPermission(input)
}

// PermissionRequest describes a pending tool call that needs authorisation.
type PermissionRequest struct {
	ToolName string
	Input    json.RawMessage
	Summary  string
}

// Decision is the user's (or policy's) response to a permission request.
type Decision int

const (
	Allow Decision = iota
	AlwaysAllow
	Deny
)

// PermissionFunc is called by the agent loop before executing any tool
// that reports NeedsPermission() == true.
type PermissionFunc func(ctx context.Context, req PermissionRequest) Decision

// AllowAll is a PermissionFunc that approves every tool call without prompting.
func AllowAll(_ context.Context, _ PermissionRequest) Decision { return Allow }

