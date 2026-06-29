// Package tools provides the tool catalog and the built-in tool implementations.
//
// The catalog is the one registry of capabilities: tools register by name, the
// agent is advertised exactly the registered set in a stable order, and a
// duplicate name is rejected at wire-up so a collision surfaces immediately
// rather than silently dropping a tool at runtime.
package tools

import (
	"fmt"

	"github.com/blkcor/coragent/internal/core"
)

// Catalog is the insertion-ordered registry of tools. It stores executable
// handlers by name and advertises their descriptors to the model in registration
// order — deterministic across runs for the same registration sequence.
type Catalog struct {
	order  []string
	byName map[string]core.ToolHandler
}

// NewCatalog returns an empty catalog ready for registration.
func NewCatalog() *Catalog {
	return &Catalog{byName: make(map[string]core.ToolHandler)}
}

// Register adds a tool under its descriptor name. A second tool registered under
// an already-used name is rejected and the first tool is left intact, so the
// collision is found at wire-up instead of losing a tool at runtime.
func (c *Catalog) Register(h core.ToolHandler) error {
	name := h.Descriptor().Name
	if name == "" {
		return fmt.Errorf("tools: cannot register a tool with an empty name")
	}
	if _, exists := c.byName[name]; exists {
		return fmt.Errorf("tools: a tool named %q is already registered", name)
	}
	c.byName[name] = h
	c.order = append(c.order, name)
	return nil
}

// MustRegister registers a tool and panics on collision. It is for static wire-up
// of known-unique tools (the built-ins) where a duplicate is a programmer error.
func (c *Catalog) MustRegister(h core.ToolHandler) {
	if err := c.Register(h); err != nil {
		panic(err)
	}
}

// Lookup returns the handler registered under name, or false if none is.
func (c *Catalog) Lookup(name string) (core.ToolHandler, bool) {
	h, ok := c.byName[name]
	return h, ok
}

// Advertise returns one descriptor per registered tool, in registration order.
// The order is identical across runs for the same registration sequence, so runs
// are reproducible.
func (c *Catalog) Advertise() []core.Tool {
	out := make([]core.Tool, 0, len(c.order))
	for _, name := range c.order {
		out = append(out, c.byName[name].Descriptor())
	}
	return out
}
