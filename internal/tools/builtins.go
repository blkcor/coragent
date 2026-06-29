package tools

import "github.com/blkcor/coragent/internal/core"

// Builtins returns the six built-in tool handlers in a stable order: read, write,
// edit, run, content-search, file-find.
func Builtins() []core.ToolHandler {
	return []core.ToolHandler{
		ReadFile{},
		WriteFile{},
		EditFile{},
		ShellCommand{},
		SearchContent{},
		FindFiles{},
	}
}

// RegisterBuiltins registers the six built-in tools into a catalog. The built-ins
// have distinct names, so it uses MustRegister: a collision would be a programmer
// error surfaced at wire-up.
func RegisterBuiltins(c *Catalog) {
	for _, h := range Builtins() {
		c.MustRegister(h)
	}
}

// NewDefaultCatalog returns a catalog pre-loaded with the six built-in tools.
func NewDefaultCatalog() *Catalog {
	c := NewCatalog()
	RegisterBuiltins(c)
	return c
}
