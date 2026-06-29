package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

// stubTool is a minimal ToolHandler for catalog tests.
type stubTool struct {
	name     string
	runsCmds bool
}

func (s stubTool) Descriptor() core.Tool {
	return core.Tool{Name: s.name, Description: "stub " + s.name, Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (s stubTool) Execute(context.Context, map[string]interface{}) (string, error) {
	return s.name, nil
}
func (s stubTool) RunsCommands() bool { return s.runsCmds }

func TestCatalogAdvertisesExactlyTheRegisteredSet(t *testing.T) {
	c := NewCatalog()
	for _, n := range []string{"read", "write", "shell"} {
		if err := c.Register(stubTool{name: n}); err != nil {
			t.Fatalf("register %q: %v", n, err)
		}
	}

	adv := c.Advertise()
	if len(adv) != 3 {
		t.Fatalf("want 3 advertised tools, got %d", len(adv))
	}
	got := map[string]bool{}
	for _, d := range adv {
		got[d.Name] = true
	}
	for _, n := range []string{"read", "write", "shell"} {
		if !got[n] {
			t.Errorf("advertised set missing %q", n)
		}
	}
}

func TestCatalogAdvertiseOrderIsStableAcrossRuns(t *testing.T) {
	build := func() []string {
		c := NewCatalog()
		for _, n := range []string{"alpha", "bravo", "charlie", "delta"} {
			_ = c.Register(stubTool{name: n})
		}
		var names []string
		for _, d := range c.Advertise() {
			names = append(names, d.Name)
		}
		return names
	}

	first, second := build(), build()
	if len(first) != len(second) {
		t.Fatalf("length differs: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order differs at %d: %v vs %v", i, first, second)
		}
	}
	// Registration order is preserved.
	want := []string{"alpha", "bravo", "charlie", "delta"}
	for i := range want {
		if first[i] != want[i] {
			t.Fatalf("advertised order %v, want %v", first, want)
		}
	}
}

func TestCatalogRejectsDuplicateNameKeepingFirst(t *testing.T) {
	c := NewCatalog()
	if err := c.Register(stubTool{name: "edit", runsCmds: false}); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := c.Register(stubTool{name: "edit", runsCmds: true})
	if err == nil {
		t.Fatalf("expected duplicate registration to be rejected")
	}

	// The first tool must remain intact (not replaced by the rejected one).
	h, ok := c.Lookup("edit")
	if !ok {
		t.Fatalf("first tool lost after duplicate rejection")
	}
	if h.RunsCommands() {
		t.Errorf("first tool was overwritten by the rejected duplicate")
	}
	if len(c.Advertise()) != 1 {
		t.Errorf("duplicate must not add a second entry, got %d", len(c.Advertise()))
	}
}
