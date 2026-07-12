package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/config"
)

func TestSettingsRepresentationsAreSecretFree(t *testing.T) {
	network := true
	settings := Settings{value: config.Settings{
		Model: &config.ModelSettings{
			Name:    "gpt-test",
			BaseURL: "https://user:password@example.test/v1?api_key=query-secret",
			APIKey:  "resolved-api-secret",
		},
		Hooks: []config.HookSettings{{
			Name:    "secret-hook",
			Moment:  "before-tool",
			Command: []string{"sh", "-c", "send hook-command-secret"},
		}},
		Permission: &config.PermissionSettings{
			Mode:  "plan",
			Allow: []string{"command:permission-secret"},
		},
		Sandbox: &config.SandboxSettings{Network: &network},
	}}

	jsonValue, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	logger.Info("settings", "value", settings)

	joined := strings.Join([]string{
		fmt.Sprint(settings),
		fmt.Sprintf("%#v", settings),
		string(jsonValue),
		logs.String(),
	}, "\n")
	for _, secret := range []string{"password", "query-secret", "resolved-api-secret", "hook-command-secret", "permission-secret"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("public representation leaked %q:\n%s", secret, joined)
		}
	}
	if !strings.Contains(joined, "gpt-test") || !strings.Contains(joined, "https://example.test") {
		t.Fatalf("safe identity missing from representations:\n%s", joined)
	}
}

func TestBootstrapBuildsStandardSession(t *testing.T) {
	settings := Settings{value: config.LoadFrom(config.Settings{
		Model: &config.ModelSettings{
			Name:    "gpt-test",
			BaseURL: "https://example.test/v1",
		},
	})}

	session, err := Bootstrap(settings, BootstrapOptions{WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if session == nil {
		t.Fatal("Bootstrap returned a nil session")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBootstrapSendsCoragentProductIdentity(t *testing.T) {
	type chatMessage struct {
		Role    string  `json:"role"`
		Content *string `json:"content"`
	}
	type chatRequest struct {
		Messages []chatMessage `json:"messages"`
	}
	type capturedRequest struct {
		request chatRequest
		err     error
	}

	captured := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		err := json.NewDecoder(r.Body).Decode(&request)
		captured <- capturedRequest{request: request, err: err}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	const (
		modelName = "private-backend-model"
		apiKey    = "private-bootstrap-api-key"
		input     = "who are u"
	)
	settings := Settings{value: config.LoadFrom(config.Settings{
		Model: &config.ModelSettings{
			Name:    modelName,
			BaseURL: server.URL,
			APIKey:  apiKey,
		},
	})}

	session, err := Bootstrap(settings, BootstrapOptions{WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() {
		if err := session.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	events, err := session.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range events {
	}

	got := <-captured
	if got.err != nil {
		t.Fatalf("decode chat request: %v", got.err)
	}
	if len(got.request.Messages) < 2 {
		t.Fatalf("messages = %#v, want system framing followed by user input", got.request.Messages)
	}
	system := got.request.Messages[0]
	if system.Role != "system" || system.Content == nil || strings.TrimSpace(*system.Content) == "" {
		t.Fatalf("first message = %#v, want non-empty system framing", system)
	}
	lowerSystem := strings.ToLower(*system.Content)
	if !strings.Contains(lowerSystem, "coragent") {
		t.Fatalf("system framing does not identify the product as coragent: %q", *system.Content)
	}
	if !strings.Contains(lowerSystem, "replaceable") || !strings.Contains(lowerSystem, "backend") {
		t.Fatalf("system framing does not distinguish product identity from its replaceable backend: %q", *system.Content)
	}
	for _, privateValue := range []string{modelName, apiKey, server.URL} {
		if strings.Contains(*system.Content, privateValue) {
			t.Fatalf("system framing leaked private bootstrap configuration %q", privateValue)
		}
	}

	user := got.request.Messages[1]
	if user.Role != "user" || user.Content == nil || *user.Content != input {
		t.Fatalf("second message = %#v, want user input %q", user, input)
	}
}

func TestNewSessionKeepsExplicitSystemPromptAuthoritative(t *testing.T) {
	const explicit = "Caller-owned system framing"
	provider := &conversationCapturingProvider{calls: make(chan Conversation, 1)}
	session, err := NewSessionWithError(SessionConfig{Provider: provider, SystemPrompt: explicit})
	if err != nil {
		t.Fatalf("NewSessionWithError: %v", err)
	}
	defer func() {
		if err := session.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	conversation := session.Conversation()
	if len(conversation.Turns) != 1 {
		t.Fatalf("conversation turns = %#v, want exactly the caller's system turn", conversation.Turns)
	}
	if got := conversation.Turns[0]; got.Role != "system" || got.Content != explicit {
		t.Fatalf("system turn = %#v, want explicit caller framing %q", got, explicit)
	}

	events, err := session.Run(context.Background(), "identify this SDK session")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for range events {
	}
	captured := <-provider.calls
	if len(captured.Turns) < 2 {
		t.Fatalf("provider conversation = %#v, want explicit system and user turns", captured.Turns)
	}
	if got := captured.Turns[0]; got.Role != "system" || got.Content != explicit {
		t.Fatalf("provider system turn = %#v, want explicit caller framing %q", got, explicit)
	}
}

type conversationCapturingProvider struct {
	calls chan Conversation
}

func (provider *conversationCapturingProvider) StreamReply(_ context.Context, conversation Conversation, _ []Tool, _ StreamOptions) <-chan RunEvent {
	provider.calls <- conversation
	events := make(chan RunEvent, 1)
	events <- RunEvent{Type: ReplyEndedEvent, ReplyEnded: &ReplyEnded{Reason: Finished}}
	close(events)
	return events
}

func TestBootstrapRejectsInvalidPublicSettings(t *testing.T) {
	settings := Settings{value: config.LoadFrom(config.Settings{
		Model: &config.ModelSettings{
			Name:    "gpt-test",
			BaseURL: "https://example.test/v1",
		},
		Permission: &config.PermissionSettings{Mode: "surprise"},
	})}

	_, err := Bootstrap(settings, BootstrapOptions{WorkingDirectory: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "permission.mode") {
		t.Fatalf("Bootstrap error = %v, want permission.mode validation", err)
	}
}

func TestBootstrapValidationErrorsDoNotEchoSecretSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings config.Settings
		secret   string
		context  string
	}{
		{
			name: "permission rule",
			settings: config.Settings{
				Model: &config.ModelSettings{Name: "gpt-test", BaseURL: "https://example.test/v1"},
				Permission: &config.PermissionSettings{
					Mode:  "default",
					Allow: []string{"Bearer top-secret-without-a-rule-kind"},
				},
			},
			secret:  "top-secret-without-a-rule-kind",
			context: "permission.allow[0]",
		},
		{
			name: "provider URL",
			settings: config.Settings{
				Model: &config.ModelSettings{
					Name:    "gpt-test",
					BaseURL: "https://example.test/%zz?token=url-secret",
				},
				Permission: &config.PermissionSettings{Mode: "default"},
			},
			secret:  "url-secret",
			context: "model.base_url",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Bootstrap(Settings{value: test.settings}, BootstrapOptions{WorkingDirectory: t.TempDir()})
			if err == nil {
				t.Fatal("Bootstrap accepted invalid settings")
			}
			if strings.Contains(err.Error(), test.secret) {
				t.Fatalf("validation error leaked %q: %v", test.secret, err)
			}
			if !strings.Contains(err.Error(), test.context) {
				t.Fatalf("validation error lost safe field context %q: %v", test.context, err)
			}
		})
	}
}

func TestLoadSettingsUsesCanonicalHomeProjectMergeAndEnvironmentResolution(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CORAGENT_TEST_API_KEY", "resolved-secret")
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWorkingDirectory) }()

	homePath := filepath.Join(home, ".coragent", "settings.json")
	projectPath := filepath.Join(project, ".coragent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(homePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(homePath, []byte(`{
  "model":{"name":"home-model","base_url":"https://home.example/v1","api_key":"${CORAGENT_TEST_API_KEY}"},
  "permission":{"mode":"default","allow":["command:git status"]}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(`{
  "model":{"name":"project-model"},
  "permission":{"mode":"plan","deny":["command:rm"]}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.value.Model == nil || settings.value.Model.Name != "project-model" || settings.value.Model.BaseURL != "https://home.example/v1" || settings.value.Model.APIKey != "resolved-secret" {
		t.Fatalf("model merge = %+v", settings.value.Model)
	}
	if settings.value.Permission == nil || settings.value.Permission.Mode != "plan" || len(settings.value.Permission.Allow) != 1 || len(settings.value.Permission.Deny) != 1 {
		t.Fatalf("permission merge = %+v", settings.value.Permission)
	}
	if strings.Contains(settings.String(), "resolved-secret") {
		t.Fatal("public settings representation exposed resolved environment value")
	}
}

func TestLoadSettingsMissingFilesUsesDefaultsAndMalformedFileNamesPath(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWorkingDirectory) }()

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("missing files: %v", err)
	}
	if settings.value.Model == nil || settings.value.Model.Name == "" || settings.value.Permission == nil || settings.value.Permission.Mode != "default" {
		t.Fatalf("defaults = %+v", settings.value)
	}

	path := filepath.Join(project, ".coragent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"model":`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadSettings()
	if err == nil || !strings.Contains(err.Error(), filepath.Join(".coragent", "settings.json")) {
		t.Fatalf("malformed settings error = %v, want offending settings path", err)
	}
}
