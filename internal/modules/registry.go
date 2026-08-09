// Package modules defines the compile-time module registry for AIHub.
package modules

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aihub/aihub/internal/config"
	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/storage"
)

// AuthGateway is implemented by the core module and used by other modules
// to protect their routes.
type AuthGateway interface {
	// RequireAuth requires a valid web session.
	RequireAuth(h httpx.HandlerFunc) httpx.HandlerFunc
	// RequireToken requires a bearer API token carrying at least one scope.
	RequireToken(scopes ...string) func(httpx.HandlerFunc) httpx.HandlerFunc
	// RequireWrite wraps a handler so API-token callers need write scope
	// (generic "write" or "<group>.write").
	RequireWrite(group string) func(httpx.HandlerFunc) httpx.HandlerFunc
	// RequireDelete wraps a handler so API-token callers need delete scope
	// (generic "delete" or "<group>.delete").
	RequireDelete(group string) func(httpx.HandlerFunc) httpx.HandlerFunc
	// TokenScopes extracts the effective scopes from a request context.
	TokenScopes(ctx context.Context) []string
}

// Deps carries shared dependencies to each module.
type Deps struct {
	Cfg      *config.Config
	DB       *db.Pool
	Store    *storage.Storage
	Logger   *slog.Logger
	Registry *Registry
	Auth     AuthGateway
	// MCP is the shared MCP tool registry; modules add their tools on Register.
	MCP *mcpx.Registry
	// Extra can be used for cross-module service sharing after registration.
	Extra map[string]any
}

// Module is the compile-time module contract.
type Module interface {
	// ID is the stable module identifier, e.g. "core", "prompts".
	ID() string
	// Version is the module semantic version.
	Version() string
	// DependsOn lists module IDs that must be enabled first.
	DependsOn() []string
	// Migrations returns module-owned database migrations.
	Migrations() []db.Migration
	// Register wires HTTP routes and services into the app.
	Register(r *httpx.Router, deps *Deps) error
	// MCPTools returns the module's MCP tool definitions.
	MCPTools() []mcpx.ToolDef
	// Health reports module health.
	Health(ctx context.Context, deps *Deps) error
}

// Registry holds all registered modules in registration order.
type Registry struct {
	mu      sync.Mutex
	modules map[string]Module
	order   []string
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{modules: map[string]Module{}}
}

// Register adds a module, validating its dependencies.
func (r *Registry) Register(m Module) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.modules[m.ID()]; ok {
		return fmt.Errorf("module %q already registered", m.ID())
	}
	for _, dep := range m.DependsOn() {
		if _, ok := r.modules[dep]; !ok {
			return fmt.Errorf("module %q depends on unregistered module %q", m.ID(), dep)
		}
	}
	r.modules[m.ID()] = m
	r.order = append(r.order, m.ID())
	return nil
}

// Get returns a module by id.
func (r *Registry) Get(id string) (Module, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.modules[id]
	return m, ok
}

// All returns modules in registration order.
func (r *Registry) All() []Module {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Module, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.modules[id])
	}
	return out
}

// Enabled returns modules enabled by configuration.
func (r *Registry) Enabled(cfg *config.Config) []Module {
	out := []Module{}
	for _, m := range r.All() {
		if cfg.ModuleEnabled(m.ID()) {
			out = append(out, m)
		}
	}
	return out
}

// Migrations aggregates migrations from all registered modules.
func (r *Registry) Migrations() []db.Migration {
	out := []db.Migration{}
	for _, m := range r.All() {
		out = append(out, m.Migrations()...)
	}
	return out
}
