package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/core"
)

func TestBuiltInActionPreviewersAreAuthoritativeAndSideEffectFree(t *testing.T) {
	temp := t.TempDir()
	sentinel := filepath.Join(temp, "must-not-exist")
	tests := []struct {
		name      string
		handler   core.ToolHandler
		arguments map[string]interface{}
		kind      core.ActionPreviewKind
		operation core.ActionOperation
		contains  []string
	}{
		{
			name: "read file defaults without stat", handler: ReadFile{},
			arguments: map[string]interface{}{"path": filepath.Join(temp, "missing.txt")},
			kind:      core.ActionPreviewMetadata, operation: core.ActionOperationCustom,
			contains: []string{"missing.txt", "line 1", "all remaining lines"},
		},
		{
			name: "search defaults without rg", handler: SearchContent{},
			arguments: map[string]interface{}{"pattern": "needle", "ignore_case": true},
			kind:      core.ActionPreviewMetadata, operation: core.ActionOperationCustom,
			contains: []string{"needle", ".", "case-insensitive"},
		},
		{
			name: "find defaults without walk", handler: FindFiles{},
			arguments: map[string]interface{}{"pattern": "*.go", "root": filepath.Join(temp, "missing-root")},
			kind:      core.ActionPreviewMetadata, operation: core.ActionOperationCustom,
			contains: []string{"*.go", "missing-root", "node_modules"},
		},
		{
			name: "command text without launch", handler: ShellCommand{},
			arguments: map[string]interface{}{"command": "touch " + sentinel, "timeout_ms": 1250},
			kind:      core.ActionPreviewText, operation: core.ActionOperationCommand,
			contains: []string{"touch " + sentinel, "1.25s"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previewer, ok := test.handler.(core.ActionPreviewer)
			if !ok {
				t.Fatalf("%s does not implement ActionPreviewer", test.handler.Descriptor().Name)
			}
			preview, err := previewer.PreviewAction(context.Background(), test.arguments)
			if err != nil {
				t.Fatalf("PreviewAction: %v", err)
			}
			if preview.Kind != test.kind || preview.Operation != test.operation || preview.Kind == core.ActionPreviewUnavailable {
				t.Fatalf("preview = %+v", preview)
			}
			joined := preview.Summary + "\n" + preview.Text
			for key, value := range preview.Metadata {
				joined += "\n" + key + ":" + value
			}
			for _, fragment := range test.contains {
				if !strings.Contains(joined, fragment) {
					t.Errorf("preview %q does not contain %q", joined, fragment)
				}
			}
		})
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("command preview caused a side effect: %v", err)
	}
}

func TestMutatingFileToolsKeepPreparedActionContract(t *testing.T) {
	for _, handler := range []core.ToolHandler{WriteFile{}, EditFile{}} {
		if _, ok := handler.(core.PreparedActionHandler); !ok {
			t.Errorf("%s lost PreparedActionHandler", handler.Descriptor().Name)
		}
		if _, ok := handler.(core.ActionPreviewer); ok {
			t.Errorf("%s must use identity-bound preparation, not preview-only", handler.Descriptor().Name)
		}
	}
}
