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

func TestRestrictedViewPreservesEffectiveParentDescriptorsAndOrder(t *testing.T) {
	parent := NewCatalog()
	for _, name := range []string{"alpha", "bravo", "charlie", "hidden"} {
		parent.MustRegister(stubTool{name: name})
	}

	advertised := []core.Tool{
		{Name: "bravo", Description: "caller bravo", Parameters: json.RawMessage(`{"type":"object","properties":{"b":{"type":"boolean"}}}`)},
		{Name: "charlie", Description: "caller charlie", Parameters: json.RawMessage(`{"type":"object","properties":{"c":{"type":"string"}}}`)},
		{Name: "ghost", Description: "advertised without handler", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "alpha", Description: "caller alpha", Parameters: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"}}}`)},
	}

	view := parent.RestrictedView(advertised, []string{"alpha", "charlie", "hidden", "ghost", "missing"})
	got := view.Advertise()
	if len(got) != 2 {
		t.Fatalf("restricted view advertised %d tools, want 2: %+v", len(got), got)
	}
	if got[0].Name != "charlie" || got[1].Name != "alpha" {
		t.Fatalf("restricted view order = %v, want [charlie alpha]", descriptorNames(got))
	}
	if got[0].Description != "caller charlie" || string(got[0].Parameters) != string(advertised[1].Parameters) {
		t.Fatalf("charlie descriptor was regenerated instead of preserved: %+v", got[0])
	}
	if got[1].Description != "caller alpha" || string(got[1].Parameters) != string(advertised[3].Parameters) {
		t.Fatalf("alpha descriptor was regenerated instead of preserved: %+v", got[1])
	}

	for _, name := range []string{"charlie", "alpha"} {
		if _, ok := view.Lookup(name); !ok {
			t.Errorf("restricted view cannot resolve advertised tool %q", name)
		}
	}
	for _, name := range []string{"bravo", "hidden", "ghost", "missing"} {
		if _, ok := view.Lookup(name); ok {
			t.Errorf("restricted view unexpectedly resolves %q", name)
		}
	}

	// Deriving a child view must not mutate the parent's executable registry.
	for _, name := range []string{"alpha", "bravo", "charlie", "hidden"} {
		if _, ok := parent.Lookup(name); !ok {
			t.Errorf("parent lost handler %q", name)
		}
	}
	if got := descriptorNames(parent.Advertise()); !equalNames(got, []string{"alpha", "bravo", "charlie", "hidden"}) {
		t.Fatalf("parent advertisement changed to %v", got)
	}
}

func TestRestrictedViewFirstDuplicateDescriptorWins(t *testing.T) {
	parent := NewCatalog()
	parent.MustRegister(stubTool{name: "read_file"})

	advertised := []core.Tool{
		{Name: "read_file", Description: "first", Parameters: json.RawMessage(`{"type":"object","properties":{"first":{"type":"string"}}}`)},
		{Name: "read_file", Description: "second", Parameters: json.RawMessage(`{"type":"object","properties":{"second":{"type":"string"}}}`)},
	}
	view := parent.RestrictedView(advertised, []string{"read_file", "read_file"})

	got := view.Advertise()
	if len(got) != 1 {
		t.Fatalf("duplicate parent descriptors produced %d child entries, want 1", len(got))
	}
	if got[0].Description != "first" || string(got[0].Parameters) != string(advertised[0].Parameters) {
		t.Fatalf("child kept %+v, want first parent descriptor", got[0])
	}
	if advertised[0].Description != "first" || advertised[1].Description != "second" {
		t.Fatalf("parent advertised list was mutated: %+v", advertised)
	}

	// A view owns its preserved descriptor bytes; later caller mutation cannot
	// silently change what the child advertises.
	advertised[0].Parameters[0] = '['
	if string(view.Advertise()[0].Parameters) == string(advertised[0].Parameters) {
		t.Fatalf("restricted view retained mutable parent descriptor storage")
	}
}

func TestRestrictedViewEmptyRequestUsesSafeReadOnlyDefaults(t *testing.T) {
	parent := NewCatalog()
	for _, name := range []string{"read_file", "write_file", "search_content", "custom_read", "find_files"} {
		parent.MustRegister(stubTool{name: name})
	}
	advertised := []core.Tool{
		{Name: "write_file", Description: "write"},
		{Name: "find_files", Description: "find"},
		{Name: "custom_read", Description: "custom but read-classified elsewhere"},
		{Name: "read_file", Description: "read"},
		{Name: "search_content", Description: "search"},
	}

	for _, requested := range [][]string{nil, {}} {
		view := parent.RestrictedView(advertised, requested)
		if got := descriptorNames(view.Advertise()); !equalNames(got, []string{"find_files", "read_file", "search_content"}) {
			t.Fatalf("safe default order = %v, want [find_files read_file search_content]", got)
		}
		for _, name := range []string{"write_file", "custom_read"} {
			if _, ok := view.Lookup(name); ok {
				t.Errorf("safe default unexpectedly includes %q", name)
			}
		}
	}
}

func TestRestrictedViewSafeDefaultsSkipUnavailableTools(t *testing.T) {
	parent := NewCatalog()
	parent.MustRegister(stubTool{name: "read_file"})
	parent.MustRegister(stubTool{name: "search_content"}) // executable but hidden

	advertised := []core.Tool{
		{Name: "read_file", Description: "read"},
		{Name: "find_files", Description: "advertised but not executable"},
	}
	view := parent.RestrictedView(advertised, nil)
	if got := descriptorNames(view.Advertise()); !equalNames(got, []string{"read_file"}) {
		t.Fatalf("safe defaults = %v, want only effective read_file", got)
	}
}

func descriptorNames(descriptors []core.Tool) []string {
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.Name)
	}
	return names
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
