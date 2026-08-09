// Package mcpx implements the AIHub MCP service: a unified tool registry and
// adapters for the official MCP Go SDK over stdio and Streamable HTTP.
package mcpx

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// ToolDef describes a single MCP tool contributed by a module.
type ToolDef struct {
	// Name is the MCP tool name, e.g. "prompts.read".
	Name string
	// Description shown to clients.
	Description string
	// InputSchema is a JSON Schema (2020-12 object).
	InputSchema map[string]any
	// Handler receives validated arguments and returns a JSON-serializable result.
	Handler func(ctx context.Context, args map[string]any) (any, error)
	// Write marks the tool as a write tool (requires a write-capable scope).
	Write bool
	// Delete marks the tool as a delete tool (requires a delete-capable scope).
	Delete bool
	// Group is the module/group this tool belongs to, e.g. "prompts".
	Group string
}

// Registry aggregates ToolDefs from modules.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ToolDef
	order []string
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]ToolDef{}}
}

// Add registers a tool, replacing one with the same name.
func (r *Registry) Add(t ToolDef) error {
	if t.Name == "" || t.Handler == nil {
		return fmt.Errorf("invalid tool def: name and handler are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Name]; !ok {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = t
	return nil
}

// All returns tools in registration order.
func (r *Registry) All() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolDef, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// ScopesAllowed decides whether write/delete tools are visible for a scope set.
// Scopes come from API tokens. The special scopes "write" and "delete" enable
// all write/delete tools; module-level scopes like "prompts.write" enable the
// corresponding group's tools.
func ScopesAllowed(scopes []string, group string, write, delete bool) bool {
	set := map[string]bool{}
	for _, s := range scopes {
		set[s] = true
	}
	if write && (set["write"] || set[group+".write"]) {
		return true
	}
	if delete && (set["delete"] || set[group+".delete"]) {
		return true
	}
	if !write && !delete {
		return true // read tools are always allowed
	}
	return false
}

// Log is a small helper for structured tool errors.
func Log(l *slog.Logger, tool string, err error) {
	if l != nil {
		l.Error("mcp tool error", "tool", tool, "error", err)
	}
}

// JSONArgs unmarshals raw arguments into a typed value.
func JSONArgs(data json.RawMessage, dst any) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, dst)
}
