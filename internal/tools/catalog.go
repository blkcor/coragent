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
	order               []string
	byName              map[string]core.ToolHandler
	descriptorOverrides map[string]core.Tool
}

// NewCatalog returns an empty catalog ready for registration.
func NewCatalog() *Catalog {
	return &Catalog{
		byName:              make(map[string]core.ToolHandler),
		descriptorOverrides: make(map[string]core.Tool),
	}
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
		if descriptor, ok := c.descriptorOverrides[name]; ok {
			out = append(out, cloneDescriptor(descriptor))
			continue
		}
		out = append(out, c.byName[name].Descriptor())
	}
	return out
}

// RestrictedView returns an independent catalog containing only tools that are
// both executable in c and present in the parent's effective advertised list.
// The returned view preserves the parent's exact descriptors and advertisement
// order. When requested is empty, the fixed safe read-only default is used.
//
// Parent descriptor lists may contain duplicate names even though executable
// catalogs cannot. Child derivation keeps the first descriptor for each name and
// ignores later duplicates without mutating either input.
func (c *Catalog) RestrictedView(advertised []core.Tool, requested []string) *Catalog {
	allowed := requestedNameSet(requested)
	view := NewCatalog()
	seen := make(map[string]struct{}, len(advertised))

	for _, descriptor := range advertised {
		name := descriptor.Name
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}

		if _, ok := allowed[name]; !ok {
			continue
		}
		handler, ok := c.Lookup(name)
		if !ok {
			continue
		}

		view.order = append(view.order, name)
		view.byName[name] = handler
		view.descriptorOverrides[name] = cloneDescriptor(descriptor)
	}

	return view
}

func requestedNameSet(requested []string) map[string]struct{} {
	if len(requested) == 0 {
		return map[string]struct{}{
			"read_file":      {},
			"search_content": {},
			"find_files":     {},
		}
	}

	allowed := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		allowed[name] = struct{}{}
	}
	return allowed
}

func cloneDescriptor(descriptor core.Tool) core.Tool {
	clone := descriptor
	clone.Parameters = append([]byte(nil), descriptor.Parameters...)
	return clone
}
