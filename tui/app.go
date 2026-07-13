package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	idleQuitWindow          = 1500 * time.Millisecond
	quitDrainLimit          = 2 * time.Second
	closeLimit              = 2 * time.Second
	activityPeriod          = 120 * time.Millisecond
	permissionArgumentLimit = 64 * 1024
	permissionGrantLimit    = 16 * 1024

	// XTSHIFTESCAPE n=0 tells supporting terminals that Coragent does not need
	// Shift-modified mouse reports, leaving Shift available for native selection.
	terminalShiftSelection = "\x1b[>0s"
)

type RunState uint8

const (
	RunBooting RunState = iota
	RunIdle
	RunRunning
	RunCancelling
	RunStartupError
	RunQuitting
)

type FocusState uint8

const (
	FocusComposer FocusState = iota
	FocusPermission
)

type ScrollMode uint8

const (
	ScrollPinnedBottom ScrollMode = iota
	ScrollBrowsingHistory
)

type ScrollState struct {
	Mode            ScrollMode
	Top             int
	Unread          int
	AnchorBlockID   string
	AnchorLine      int
	AnchorScreenRow int
}

type TerminalState struct {
	Width  int
	Height int
	Class  LayoutClass
}

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                                { return time.Now() }
func (systemClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

type AppOption func(*AppModel)

func WithClock(clock Clock) AppOption {
	return func(model *AppModel) {
		if clock != nil {
			model.clock = clock
		}
	}
}

func WithVisualMode(mode VisualMode) AppOption {
	return func(model *AppModel) {
		model.theme = ThemeForMode(mode)
	}
}

type permissionState struct {
	Prompt        PermissionPrompt
	Submitting    bool
	Decision      PermissionDecision
	Selected      permissionAction
	Feedback      string
	Scroll        int
	View          permissionView
	Editor        composerModel
	ArgumentDraft string
	GrantDraft    string
	Grants        SandboxGrants
}

type permissionView uint8

const (
	permissionDecision permissionView = iota
	permissionArguments
	permissionGrants
)

type permissionAction uint8

const (
	permissionActionUnknown permissionAction = iota
	permissionActionAllowOnce
	permissionActionAllowRemember
	permissionActionDenyOnce
	permissionActionDenyRemember
	permissionActionEditArguments
	permissionActionSandboxGrants
)

func enabledPermissionActions(prompt PermissionPrompt, tooSmall bool) []permissionAction {
	if tooSmall {
		return []permissionAction{permissionActionDenyOnce}
	}
	actions := make([]permissionAction, 0, 6)
	if permissionAllows(prompt) {
		actions = append(actions, permissionActionAllowOnce)
		if prompt.Capabilities.Remember && prompt.RememberScope != "" {
			actions = append(actions, permissionActionAllowRemember)
		}
	}
	if permissionDenies(prompt) {
		actions = append(actions, permissionActionDenyOnce)
		if prompt.Capabilities.Remember && prompt.RememberScope != "" {
			actions = append(actions, permissionActionDenyRemember)
		}
	}
	if prompt.Capabilities.ReviseArguments && prompt.Capabilities.SchemaAwareEdit {
		actions = append(actions, permissionActionEditArguments)
	}
	if prompt.Capabilities.SandboxGrants && prompt.GrantOptions.Support == SupportSupported {
		actions = append(actions, permissionActionSandboxGrants)
	}
	if len(actions) == 0 {
		return []permissionAction{permissionActionDenyOnce}
	}
	return actions
}

func selectedPermissionAction(actions []permissionAction, selected permissionAction) (permissionAction, int) {
	for index, action := range actions {
		if action == selected {
			return action, index
		}
	}
	if len(actions) == 0 {
		return permissionActionDenyOnce, 0
	}
	return actions[0], 0
}

// AppModel is the sole owner of frontend state. Run, focus, scroll, mode, and
// terminal dimensions remain orthogonal so one transition cannot erase the
// others.
type AppModel struct {
	port  SessionPort
	clock Clock
	theme Theme

	runState RunState
	focus    FocusState
	scroll   ScrollState
	mode     SessionMode
	terminal TerminalState
	layout   Layout

	info       SessionInfo
	transcript TranscriptStore
	composer   composerModel
	activity   RunActivity
	usage      *ContextUsage
	frame      int

	stream       <-chan UIEvent
	runCancel    context.CancelFunc
	runID        string
	terminalSeen bool

	permission          *permissionState
	overlay             *overlayState
	slash               *slashRegistry
	slashSuggest        slashSuggestState
	modeChangePending   bool
	pendingSubmission   string
	pendingContinuation bool

	inputHistory []string
	historyIdx   int

	quitArmedAt  time.Time
	animationOn  bool
	closing      bool
	closed       bool
	forcedExit   bool
	fatalErr     error
	closeErr     error
	startupError error

	enhancedKeyboard bool
	selection        textSelection
	clipboardWrite   func(string) error
}

func NewAppModel(port SessionPort, options ...AppOption) *AppModel {
	layout := LayoutForSize(80, 24)
	model := &AppModel{
		port:           port,
		clock:          systemClock{},
		theme:          DefaultTheme(),
		runState:       RunBooting,
		focus:          FocusComposer,
		scroll:         ScrollState{Mode: ScrollPinnedBottom},
		mode:           ModeUnsupported,
		terminal:       TerminalState{Width: layout.Width, Height: layout.Height, Class: layout.Class},
		layout:         layout,
		transcript:     NewTranscriptStore(),
		slash:          newSlashRegistry(),
		activity:       ActivityIdle,
		clipboardWrite: writeSystemClipboard,
	}
	for _, option := range options {
		if option != nil {
			option(model)
		}
	}
	model.composer = newComposerModel(model.theme, layout)
	return model
}

func (model *AppModel) Init() tea.Cmd {
	return describeSessionCmd(model.port)
}

func (model *AppModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.selection.clear()
		model.layout = LayoutForSize(message.Width, message.Height)
		model.terminal = TerminalState{Width: model.layout.Width, Height: model.layout.Height, Class: model.layout.Class}
		if model.layout.Class != LayoutTooSmall {
			model.composer.Configure(model.theme, model.layout)
			if model.permission != nil && model.permission.View != permissionDecision {
				model.permission.Editor.Configure(model.theme, model.layout)
			}
		}
		model.reconcileTranscriptScroll()
		return model, model.syncComposerFocus()
	case tea.KeyboardEnhancementsMsg:
		model.enhancedKeyboard = message.SupportsKeyDisambiguation()
		return model, nil
	case startupLoadedMsg:
		return model, model.handleStartup(message)
	case runOpenedMsg:
		return model, model.handleRunOpened(message)
	case eventReadMsg:
		return model, model.handleEventRead(message)
	case eventEOFMsg:
		return model, model.handleEventEOF(message)
	case permissionReplyMsg:
		return model, model.handlePermissionReply(message)
	case modeChangedMsg:
		return model, model.handleModeChanged(message)
	case activityTickMsg:
		return model, model.handleActivityTick()
	case quitArmExpiredMsg:
		if !model.quitArmedAt.IsZero() && !message.At.Before(model.quitArmedAt.Add(idleQuitWindow)) {
			model.quitArmedAt = time.Time{}
		}
		return model, nil
	case quitDrainExpiredMsg:
		if model.runState == RunQuitting && !model.closing {
			model.forcedExit = true
			return model, model.beginClose()
		}
		return model, nil
	case closeDoneMsg:
		model.closed = true
		model.closeErr = message.Err
		if message.TimedOut {
			model.forcedExit = true
		}
		return model, func() tea.Msg { return tea.Quit() }
	case tea.PasteMsg:
		model.selection.clear()
		if model.permission != nil && model.permission.View != permissionDecision && !model.permission.Submitting {
			model.permission.Editor.InsertString(SanitizeString(message.Content))
		} else if model.routeToComposer() {
			model.composer.InsertString(SanitizeString(message.Content))
		}
		return model, nil
	case tea.MouseWheelMsg:
		model.selection.clear()
		return model, model.handleMouseWheel(message)
	case tea.MouseClickMsg:
		model.selection.begin(message.Mouse(), model.render(), model.layout.Width, model.layout.Height)
		return model, nil
	case tea.MouseMotionMsg:
		model.selection.update(message.Mouse(), model.render(), model.layout.Width, model.layout.Height)
		return model, nil
	case tea.MouseReleaseMsg:
		text, ok := model.selection.finish(message.Mouse(), model.render(), model.layout.Width, model.layout.Height)
		if !ok {
			return model, nil
		}
		return model, copySelectionCmd(model.clipboardWrite, text)
	case tea.KeyPressMsg:
		model.selection.clear()
		return model, model.handleKey(message)
	default:
		return model, nil
	}
}

func (model *AppModel) View() tea.View {
	content := model.selection.render(model.render(), model.layout.Width, model.layout.Height)
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "coragent"
	view.MouseMode = tea.MouseModeCellMotion
	if !model.selection.dragging && model.layout.Class != LayoutTooSmall {
		if cursor := model.permissionEditorCursor(); cursor != nil {
			view.Cursor = cursor
		} else if model.routeToComposer() {
			if cursor := model.composer.Cursor(); cursor != nil {
				cursor.X += model.layout.HorizontalPadding
				cursor.Y += model.composerCursorRow()
				view.Cursor = cursor
			}
		}
	}
	if model.theme.Mode.Color != ColorNoColor {
		view.BackgroundColor = semanticColor(model.theme.Mode.Color, model.theme.Palette.Canvas, 232)
		view.ForegroundColor = semanticColor(model.theme.Mode.Color, model.theme.Palette.Text, 254)
	}
	return view
}

func (model *AppModel) permissionEditorCursor() *tea.Cursor {
	if model.permission == nil || model.permission.View == permissionDecision || model.permission.Submitting {
		return nil
	}
	cursor := model.permission.Editor.Cursor()
	if cursor == nil {
		return nil
	}
	transcriptBudget := max(1, model.layout.Height-3)
	modalBudget := min(transcriptBudget, max(9, transcriptBudget*2/3))
	modal := renderPermissionLines(model.theme, permissionRenderOptions{
		Width: model.layout.ContentWidth, MaxRows: modalBudget, Prompt: model.permission.Prompt,
		Selected: model.permission.Selected, Feedback: model.permission.Feedback, View: model.permission.View,
		EditorLines: model.permissionEditorLines(), Grants: model.permission.Grants,
	})
	modalStart := max(0, transcriptBudget-len(modal))
	cursor.X += model.layout.HorizontalPadding + 2
	cursor.Y += 1 + modalStart + 3
	return cursor
}

// ExitStatus reports frontend-local shutdown state after Bubble Tea returns.
// Callers use it only after terminal restoration to decide whether a safe
// stderr diagnostic and non-zero process exit are required.
type ExitStatus struct {
	Fatal   error
	Close   error
	Startup error
	Forced  bool
}

// Status returns a snapshot of the final frontend shutdown state.
func (model *AppModel) Status() ExitStatus {
	if model == nil {
		return ExitStatus{}
	}
	return ExitStatus{
		Fatal:   model.fatalErr,
		Close:   model.closeErr,
		Startup: model.startupError,
		Forced:  model.forcedExit,
	}
}

func (model *AppModel) handleStartup(message startupLoadedMsg) tea.Cmd {
	if message.Err != nil {
		model.startupError = fmt.Errorf("session startup: %w", message.Err)
		model.runState = RunStartupError
		model.activity = ActivityIdle
		return nil
	}
	model.info = message.Info
	if model.info.Mode == "" {
		model.info.Mode = ModeUnsupported
	}
	// Dynamically register loaded skills as slash-command entries so they
	// appear in the suggestion dropdown and can be invoked from the composer.
	for _, cat := range model.info.Capabilities {
		if cat.Kind == "skill" {
			model.slash.RegisterSkills(cat.Items)
			break
		}
	}
	model.mode = model.info.Mode
	model.runState = RunIdle
	model.focus = FocusComposer
	model.activity = ActivityIdle
	switch model.info.Sandbox {
	case "fallback":
		message := "Safety notice: command confinement uses a policy fallback, not OS enforcement."
		if model.info.SandboxReason != "" {
			message += " " + model.info.SandboxReason
		}
		model.transcript.AddNotice(message, model.clock.Now())
	case "externally owned":
		model.transcript.AddNotice("Safety notice: command confinement is externally owned.", model.clock.Now())
	}
	return tea.Batch(model.syncComposerFocus(), tea.Raw(terminalShiftSelection))
}

func (model *AppModel) handleRunOpened(message runOpenedMsg) tea.Cmd {
	if message.Err != nil || message.Stream == nil {
		failure := message.Err
		if failure == nil {
			failure = errors.New("session returned a nil event stream")
		}
		model.transcript.AddNotice("Run could not start: "+failure.Error(), model.clock.Now())
		if model.composer.Value() == "" {
			model.composer.SetValue(model.pendingSubmission)
		}
		model.pendingSubmission = ""
		model.runState = RunIdle
		model.activity = ActivityIdle
		model.runCancel = nil
		return model.syncComposerFocus()
	}
	model.pendingSubmission = ""
	model.stream = message.Stream
	model.terminalSeen = false
	model.runID = ""
	return tea.Batch(readEventCmd(message.Stream), model.ensureAnimation())
}

func (model *AppModel) handleEventRead(message eventReadMsg) tea.Cmd {
	if message.Stream != model.stream || model.stream == nil {
		return nil
	}
	var transition tea.Cmd
	if model.terminalSeen {
		transition = model.protocolFailure(errors.New("observed event arrived after run terminal"), false)
	} else if model.fatalErr != nil && message.Event.Kind != EventRunFinished {
		// Safe drain: do not apply payloads after a protocol rejection.
	} else {
		transition = model.applyEvent(message.Event)
	}
	// Every successful receive immediately arms the next receive, including the
	// authoritative terminal item so the expected EOF is drained.
	return tea.Batch(transition, readEventCmd(message.Stream))
}

func (model *AppModel) handleEventEOF(message eventEOFMsg) tea.Cmd {
	if message.Stream != model.stream || model.stream == nil {
		return nil
	}
	model.stream = nil
	if !model.terminalSeen {
		return model.protocolFailure(errors.New("observed event stream closed before run terminal"), true)
	}
	if model.runState == RunQuitting && !model.closing {
		return model.beginClose()
	}
	return nil
}

func (model *AppModel) applyEvent(event UIEvent) tea.Cmd {
	at := event.Timestamp
	if at.IsZero() {
		at = model.clock.Now()
	}

	changedTranscript := false
	switch event.Kind {
	case EventRunStarted:
		if strings.TrimSpace(event.RunID) == "" {
			return model.protocolFailure(errors.New("run_started has no run ID"), false)
		}
		model.runID = event.RunID
		if err := model.transcript.StartRun(event.RunID); err != nil {
			return model.protocolFailure(err, false)
		}
		model.runState = RunRunning
		model.activity = ActivityThinking
	case EventStatusChanged:
		if event.Activity == "" {
			return model.protocolFailure(errors.New("status_changed has no activity"), false)
		}
		model.activity = event.Activity
	case EventAssistantStarted:
		if err := model.transcript.StartAssistant(event.AssistantID, at); err != nil {
			return model.protocolFailure(err, false)
		}
		changedTranscript = true
	case EventAssistantTextDelta:
		if err := model.transcript.AppendAssistant(event.AssistantID, event.Text, at); err != nil {
			return model.protocolFailure(err, false)
		}
		changedTranscript = true
	case EventAssistantReasoningSummaryDelta:
		if err := model.transcript.AppendReasoning(event.AssistantID, event.Text, at); err != nil {
			return model.protocolFailure(err, false)
		}
		changedTranscript = true
	case EventAssistantFinished:
		if err := model.transcript.FinishAssistantWithReason(event.AssistantID, event.Termination); err != nil {
			return model.protocolFailure(err, false)
		}
		changedTranscript = true
	case EventToolStarted:
		if err := model.transcript.StartTool(event.CallID, event.ToolName, event.Arguments, at); err != nil {
			return model.protocolFailure(err, false)
		}
		model.activity = ActivityCallingTool
		changedTranscript = true
	case EventToolPrepared:
		if err := model.transcript.PrepareToolPreview(event.CallID, event.ToolName, event.Arguments, event.Revision, event.Preview); err != nil {
			return model.protocolFailure(err, false)
		}
		model.activity = ActivityCallingTool
		changedTranscript = true
	case EventPermissionRequested:
		if event.Permission == nil || strings.TrimSpace(event.Permission.RequestID) == "" || (event.Permission.Reply == nil && event.Permission.RichReply == nil) {
			return model.protocolFailure(errors.New("permission_requested has an invalid prompt"), false)
		}
		if model.permission != nil {
			return model.protocolFailure(errors.New("permission_requested arrived while another prompt is open"), false)
		}
		if model.overlay != nil && model.overlay.Kind != overlayBypass {
			model.overlay = nil
		}
		if err := model.transcript.AwaitPermission(*event.Permission, at); err != nil {
			return model.protocolFailure(err, false)
		}
		argumentDraft := prettyJSON(event.Permission.Arguments)
		model.permission = &permissionState{
			Prompt: *event.Permission, ArgumentDraft: argumentDraft,
			GrantDraft: "{\n  \"read_roots\": [],\n  \"write_roots\": [],\n  \"network\": false\n}",
		}
		model.permission.Selected, _ = selectedPermissionAction(
			enabledPermissionActions(model.permission.Prompt, model.layout.Class == LayoutTooSmall),
			permissionActionUnknown,
		)
		model.focus = FocusPermission
		model.composer.Blur()
		model.activity = ActivityPermission
		changedTranscript = true
	case EventToolExecuting:
		if err := model.transcript.ExecuteTool(event.CallID); err != nil {
			return model.protocolFailure(err, false)
		}
		model.activity = ActivityCallingTool
		changedTranscript = true
	case EventToolFinished:
		if err := model.transcript.FinishToolDetails(event.CallID, event.Result, event.Tool, at, event.Revision, event.Duration); err != nil {
			return model.protocolFailure(err, false)
		}
		model.activity = ActivityThinking
		changedTranscript = true
	case EventContextUsage:
		if event.Usage == nil {
			return model.protocolFailure(errors.New("context_usage has no payload"), false)
		}
		usage := *event.Usage
		model.usage = &usage
	case EventOmission:
		if event.Omission == nil {
			return model.protocolFailure(errors.New("omission has no payload"), false)
		}
		model.transcript.ApplyOmission(*event.Omission, at)
		if event.Omission.Kind == "provider_length" && event.Omission.Continuation == "new_user_turn" {
			model.pendingContinuation = true
		}
		changedTranscript = true
	case EventHookOutcome:
		if event.Hook == nil {
			return model.protocolFailure(errors.New("hook_outcome has no payload"), false)
		}
		model.transcript.ApplyHook(*event.Hook, at)
		changedTranscript = true
	case EventSubagentStarted:
		if event.Subagent == nil {
			return model.protocolFailure(errors.New("subagent_started has no payload"), false)
		}
		if err := model.transcript.StartSubagent(*event.Subagent, at); err != nil {
			return model.protocolFailure(err, false)
		}
		changedTranscript = true
	case EventSubagentFinished:
		if event.Subagent == nil {
			return model.protocolFailure(errors.New("subagent_finished has no payload"), false)
		}
		if err := model.transcript.FinishSubagent(*event.Subagent, at); err != nil {
			return model.protocolFailure(err, false)
		}
		changedTranscript = true
	case EventWarning:
		if strings.TrimSpace(event.Text) == "" {
			return model.protocolFailure(errors.New("warning has no text"), false)
		}
		model.transcript.AddCorrelatedNotice(event.CallID, event.Text, false, at)
		changedTranscript = true
	case EventError:
		if strings.TrimSpace(event.Text) == "" {
			return model.protocolFailure(errors.New("error has no text"), false)
		}
		model.transcript.AddCorrelatedNotice(event.CallID, event.Text, true, at)
		changedTranscript = true
	case EventNotice:
		if strings.TrimSpace(event.Text) == "" {
			return model.protocolFailure(errors.New("notice has no text"), false)
		}
		model.transcript.AddNotice(event.Text, at)
		changedTranscript = true
	case EventRunFinished:
		return model.applyTerminal(event, at)
	default:
		return model.protocolFailure(fmt.Errorf("unknown UI event kind %q", SanitizeString(string(event.Kind))), false)
	}

	if changedTranscript {
		model.noteLiveOutput()
	}
	return model.ensureAnimation()
}

func prettyJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	var document interface{}
	if json.Unmarshal([]byte(value), &document) != nil {
		return value
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return value
	}
	return string(encoded)
}

func (model *AppModel) applyTerminal(event UIEvent, at time.Time) tea.Cmd {
	switch event.Terminal {
	case RunCompleted, RunFailed, RunCancelled, RunReachedStepLimit:
	default:
		return model.protocolFailure(fmt.Errorf("run_finished has unknown outcome %q", SanitizeString(string(event.Terminal))), false)
	}
	model.terminalSeen = true
	model.activity = ActivityIdle
	model.animationOn = false
	if model.runCancel != nil {
		model.runCancel()
		model.runCancel = nil
	}
	inconsistent := model.transcript.SettleActive(event.Terminal)
	if model.permission != nil {
		model.permission = nil
	}
	switch event.Terminal {
	case RunFailed:
		message := "Run failed"
		if event.Err != "" {
			message += ": " + event.Err
		}
		model.transcript.AddNotice(message, at)
		model.noteLiveOutput()
	case RunReachedStepLimit:
		model.transcript.AddNotice("Run stopped at the step limit.", at)
		model.noteLiveOutput()
	}
	if inconsistent {
		model.transcript.AddNotice("Protocol inconsistency: run completed with active transcript items.", at)
		model.noteLiveOutput()
	}
	if model.runState == RunQuitting || model.fatalErr != nil {
		model.runState = RunQuitting
		return model.beginClose()
	}
	model.runState = RunIdle
	model.focus = FocusComposer
	return model.syncComposerFocus()
}

func (model *AppModel) handlePermissionReply(message permissionReplyMsg) tea.Cmd {
	if model.permission == nil || model.permission.Prompt.RequestID != message.RequestID {
		return nil
	}
	if message.Err != nil {
		model.permission.Submitting = false
		model.permission.Feedback = message.Err.Error()
		return nil
	}
	switch message.Result.Status {
	case ReplyAccepted, ReplyAlreadyResolved:
		if message.Result.Status == ReplyAccepted && model.permission.Decision == DecisionReviseArguments {
			model.transcript.ReprepareTool(model.permission.Prompt.CallID, model.permission.Prompt.Revision)
			model.activity = ActivityCallingTool
			model.noteLiveOutput()
		}
		model.permission = nil
		if model.runState != RunQuitting {
			model.focus = FocusComposer
		}
		if model.activity == ActivityPermission {
			model.activity = ActivityThinking
		}
	case ReplyValidationRejected:
		model.permission.Submitting = false
		model.permission.Feedback = message.Result.Feedback
	default:
		model.permission.Submitting = false
		model.permission.Feedback = "permission reply returned an unknown status"
	}
	return model.syncComposerFocus()
}

func (model *AppModel) handleModeChanged(message modeChangedMsg) tea.Cmd {
	model.modeChangePending = false
	if message.Err != nil {
		if model.overlay != nil && model.overlay.Kind == overlayBypass {
			model.overlay.Submitting = false
			model.overlay.Feedback = message.Err.Error()
			return nil
		}
		if model.permission != nil {
			model.permission.Feedback = "Mode unchanged: " + message.Err.Error()
			return nil
		}
		model.transcript.AddNotice("Mode unchanged: "+message.Err.Error(), model.clock.Now())
		model.noteLiveOutput()
		return nil
	}
	model.mode = message.Mode
	model.info.Mode = message.Mode
	if model.overlay != nil && model.overlay.Kind == overlayBypass {
		command := model.closeOverlay()
		if model.permission != nil {
			model.permission.Feedback = "Mode changed to " + sessionModeLabel(message.Mode) + "; this request still needs a decision"
		}
		return command
	}
	if model.permission != nil {
		model.permission.Feedback = "Mode changed to " + sessionModeLabel(message.Mode) + "; this request still needs a decision"
	}
	return nil
}

func (model *AppModel) handleActivityTick() tea.Cmd {
	model.animationOn = false
	if !model.isActive() || model.theme.Mode.ReducedMotion {
		return nil
	}
	model.frame++
	return model.ensureAnimation()
}

func (model *AppModel) handleKey(message tea.KeyPressMsg) tea.Cmd {
	key := message.String()
	if key == "ctrl+q" {
		return model.beginQuit()
	}
	// Ctrl+C is a global run-safety key. Ordinary overlays may consume Escape,
	// navigation, and confirmation keys, but they must never shadow cancellation.
	// A live permission keeps its stronger deny-current-plus-cancel behavior even
	// when the bypass confirmation is layered above it.
	if key == "ctrl+c" {
		if model.permission != nil {
			return model.handlePermissionKey(message, key)
		}
		if model.runState == RunRunning || model.runState == RunCancelling {
			return model.requestCancel()
		}
	}

	if model.overlay != nil {
		return model.handleOverlayKey(message, key)
	}
	if key == "shift+tab" && model.permission != nil {
		return model.cycleMode()
	}
	if model.permission != nil {
		return model.handlePermissionKey(message, key)
	}

	if model.runState == RunBooting || model.runState == RunStartupError || model.runState == RunQuitting {
		if key == "enter" {
			return model.submitDraft()
		}
		return nil
	}

	if key == "ctrl+c" {
		return model.handleIdleControlC()
	}
	if key == "esc" && (model.runState == RunRunning || model.runState == RunCancelling) {
		return model.requestCancel()
	}
	if key == "shift+tab" {
		return model.cycleMode()
	}
	if key == "ctrl+i" {
		return model.openOverlay(overlayInspector)
	}
	if key == "ctrl+/" || key == "ctrl+_" {
		return model.openOverlay(overlayHelp)
	}

	return model.handleComposerKey(message, key)
}

func (model *AppModel) requestBypass() tea.Cmd {
	if model.runState != RunIdle && model.runState != RunRunning {
		return nil
	}
	if !model.info.ModeChangeable || model.mode == ModeExternal || model.mode == ModeUnsupported || model.port == nil || model.mode == ModeBypass {
		return nil
	}
	return model.openOverlay(overlayBypass)
}

func (model *AppModel) handlePermissionKey(message tea.KeyPressMsg, key string) tea.Cmd {
	if model.permission == nil {
		return nil
	}
	if key == "ctrl+c" {
		if model.permission.Submitting {
			return model.requestCancel()
		}
		// A caller-owned legacy reply path may be live but temporarily unwritable.
		// Cancellation must not wait behind that reply or the two operations can
		// deadlock: the reply waits for the run to cancel while cancel waits for
		// the reply command to return.
		deny := model.submitPermission(DecisionDenyOnce)
		cancel := model.requestCancel()
		return tea.Batch(deny, cancel)
	}
	if model.permission.Submitting {
		return nil
	}
	if model.permission.View != permissionDecision {
		return model.handlePermissionEditorKey(message, key)
	}
	actions := enabledPermissionActions(model.permission.Prompt, model.layout.Class == LayoutTooSmall)
	selected, selectedIndex := selectedPermissionAction(actions, model.permission.Selected)
	model.permission.Selected = selected
	switch key {
	case "up", "k":
		selectedIndex = max(0, selectedIndex-1)
		model.permission.Selected = actions[selectedIndex]
		return nil
	case "down", "j":
		selectedIndex = min(len(actions)-1, selectedIndex+1)
		model.permission.Selected = actions[selectedIndex]
		return nil
	case "pgup", "ctrl+u":
		page, _ := model.permissionReviewScrollMetrics()
		model.permission.Scroll = max(0, model.permission.Scroll-page)
		return nil
	case "pgdown", "ctrl+d":
		page, maxScroll := model.permissionReviewScrollMetrics()
		model.permission.Scroll = min(maxScroll, model.permission.Scroll+page)
		return nil
	case "home":
		model.permission.Scroll = 0
		return nil
	case "end", "G":
		_, model.permission.Scroll = model.permissionReviewScrollMetrics()
		return nil
	case "enter", " ", "space":
		return model.activatePermissionAction(model.permission.Selected)
	case "d", "esc":
		return model.activatePermissionAction(permissionActionDenyOnce)
	case "a":
		if model.layout.Class == LayoutTooSmall {
			model.permission.Feedback = fmt.Sprintf("resize to at least %dx%d before allowing", MinimumTerminalWidth, MinimumTerminalHeight)
			return nil
		}
		if !permissionAllows(model.permission.Prompt) {
			model.permission.Feedback = "allow is not supported by this request"
			return nil
		}
		return model.activatePermissionAction(permissionActionAllowOnce)
	case "A":
		if model.layout.Class == LayoutTooSmall {
			model.permission.Feedback = fmt.Sprintf("resize to at least %dx%d before remembering", MinimumTerminalWidth, MinimumTerminalHeight)
			return nil
		}
		if !model.permission.Prompt.Capabilities.Remember || model.permission.Prompt.RememberScope == "" {
			model.permission.Feedback = "remember is unavailable for this action"
			return nil
		}
		return model.activatePermissionAction(permissionActionAllowRemember)
	case "D":
		if model.layout.Class == LayoutTooSmall {
			model.permission.Feedback = fmt.Sprintf("resize to at least %dx%d before remembering", MinimumTerminalWidth, MinimumTerminalHeight)
			return nil
		}
		if !model.permission.Prompt.Capabilities.Remember || model.permission.Prompt.RememberScope == "" {
			model.permission.Feedback = "remember is unavailable for this action"
			return nil
		}
		return model.activatePermissionAction(permissionActionDenyRemember)
	case "e":
		if model.layout.Class == LayoutTooSmall {
			model.permission.Feedback = fmt.Sprintf("resize to at least %dx%d before editing", MinimumTerminalWidth, MinimumTerminalHeight)
			return nil
		}
		if !model.permission.Prompt.Capabilities.ReviseArguments || !model.permission.Prompt.Capabilities.SchemaAwareEdit {
			model.permission.Feedback = "argument revision is unavailable for this request"
			return nil
		}
		return model.activatePermissionAction(permissionActionEditArguments)
	case "s":
		if model.layout.Class == LayoutTooSmall {
			model.permission.Feedback = fmt.Sprintf("resize to at least %dx%d before editing grants", MinimumTerminalWidth, MinimumTerminalHeight)
			return nil
		}
		if !model.permission.Prompt.Capabilities.SandboxGrants || model.permission.Prompt.GrantOptions.Support != SupportSupported {
			model.permission.Feedback = "one-call sandbox grants are unavailable for this request"
			return nil
		}
		return model.activatePermissionAction(permissionActionSandboxGrants)
	default:
		return nil
	}
}

func (model *AppModel) permissionReviewScrollMetrics() (page, maxScroll int) {
	if model.permission == nil || model.layout.Class == LayoutTooSmall {
		return 1, 0
	}
	transcriptBudget := max(1, model.layout.Height-2) // header and footer
	modalBudget := min(transcriptBudget, max(9, transcriptBudget*2/3))
	_, viewportRows, maxScroll, _ := permissionReviewMetrics(model.theme, permissionRenderOptions{
		Width: model.layout.ContentWidth, MaxRows: modalBudget, Prompt: model.permission.Prompt,
		Submitting: model.permission.Submitting, Selected: model.permission.Selected,
		Feedback: model.permission.Feedback, Scroll: model.permission.Scroll,
		Grants: model.permission.Grants,
	})
	return max(1, viewportRows-1), maxScroll
}

func (model *AppModel) activatePermissionAction(action permissionAction) tea.Cmd {
	if model.permission == nil {
		return nil
	}
	if model.layout.Class == LayoutTooSmall && action != permissionActionDenyOnce {
		model.permission.Feedback = fmt.Sprintf("resize to at least %dx%d before allowing or editing", MinimumTerminalWidth, MinimumTerminalHeight)
		return nil
	}
	actions := enabledPermissionActions(model.permission.Prompt, model.layout.Class == LayoutTooSmall)
	if _, index := selectedPermissionAction(actions, action); index >= len(actions) || actions[index] != action {
		model.permission.Feedback = "that decision is unavailable for this request"
		return nil
	}
	model.permission.Selected = action
	switch action {
	case permissionActionAllowOnce:
		return model.submitPermissionResponse(PermissionResponse{Decision: DecisionAllowOnce, Grants: cloneSandboxGrants(model.permission.Grants)})
	case permissionActionAllowRemember:
		return model.submitPermissionResponse(PermissionResponse{Decision: DecisionAllowRemember, Remember: true, Grants: cloneSandboxGrants(model.permission.Grants)})
	case permissionActionDenyOnce:
		return model.submitPermissionResponse(PermissionResponse{Decision: DecisionDenyOnce})
	case permissionActionDenyRemember:
		return model.submitPermissionResponse(PermissionResponse{Decision: DecisionDenyRemember, Remember: true})
	case permissionActionEditArguments:
		return model.openPermissionEditor(permissionArguments)
	case permissionActionSandboxGrants:
		return model.openPermissionEditor(permissionGrants)
	default:
		return nil
	}
}

func (model *AppModel) submitPermission(decision PermissionDecision) tea.Cmd {
	return model.submitPermissionResponse(PermissionResponse{Decision: decision})
}

func (model *AppModel) submitPermissionResponse(response PermissionResponse) tea.Cmd {
	if model.permission == nil || model.permission.Submitting {
		return nil
	}
	model.permission.Submitting = true
	model.permission.Decision = response.Decision
	model.permission.Feedback = ""
	prompt := model.permission.Prompt
	return replyPermissionResponseCmd(prompt, response)
}

func permissionAllows(prompt PermissionPrompt) bool {
	capabilities := prompt.Capabilities
	// Hand-authored legacy reducer fixtures predate capability flags. Preserve
	// their allow/deny-only behavior without fabricating richer controls.
	return capabilities.Allow || capabilities == (PermissionCapabilities{})
}

func permissionDenies(prompt PermissionPrompt) bool {
	capabilities := prompt.Capabilities
	return capabilities.Deny || capabilities == (PermissionCapabilities{})
}

func cloneSandboxGrants(grants SandboxGrants) SandboxGrants {
	return SandboxGrants{
		ReadRoots:  append([]string(nil), grants.ReadRoots...),
		WriteRoots: append([]string(nil), grants.WriteRoots...),
		Network:    grants.Network,
	}
}

func (model *AppModel) openPermissionEditor(view permissionView) tea.Cmd {
	if model.permission == nil {
		return nil
	}
	model.permission.View = view
	model.permission.Editor = newComposerModel(model.theme, model.layout)
	if view == permissionArguments {
		model.permission.Editor.SetCharLimit(permissionArgumentLimit)
		model.permission.Editor.SetValue(model.permission.ArgumentDraft)
	} else {
		model.permission.Editor.SetCharLimit(permissionGrantLimit)
		model.permission.Editor.SetValue(model.permission.GrantDraft)
	}
	return model.permission.Editor.Focus()
}

func (model *AppModel) handlePermissionEditorKey(message tea.KeyPressMsg, key string) tea.Cmd {
	state := model.permission
	if state == nil {
		return nil
	}
	if model.layout.Class == LayoutTooSmall && key != "esc" {
		state.Feedback = fmt.Sprintf("resize to at least %dx%d to continue editing", MinimumTerminalWidth, MinimumTerminalHeight)
		return nil
	}
	switch key {
	case "esc":
		state.Editor.Blur()
		state.View = permissionDecision
		state.Feedback = ""
		return nil
	case "ctrl+s":
		if state.View == permissionArguments {
			state.ArgumentDraft = state.Editor.Value()
			var arguments map[string]interface{}
			if err := json.Unmarshal([]byte(state.ArgumentDraft), &arguments); err != nil {
				state.Feedback = "arguments: " + err.Error()
				return nil
			}
			if arguments == nil {
				state.Feedback = "arguments must be a JSON object"
				return nil
			}
			return model.submitPermissionResponse(PermissionResponse{
				Decision: DecisionReviseArguments, RevisedArguments: arguments,
			})
		}
		state.GrantDraft = state.Editor.Value()
		grants, err := parseGrantDraft(state.GrantDraft, state.Prompt.GrantOptions)
		if err != nil {
			state.Feedback = "grants: " + err.Error()
			return nil
		}
		state.Grants = grants
		state.Editor.Blur()
		state.View = permissionDecision
		state.Feedback = "one-call grants updated; choose allow to apply them"
		return nil
	case "ctrl+j":
		state.Editor.InsertString("\n")
		return nil
	default:
		message.Text = SanitizeString(message.Text)
		return state.Editor.Update(message)
	}
}

func parseGrantDraft(value string, options GrantOptions) (SandboxGrants, error) {
	var wire struct {
		ReadRoots  []string `json:"read_roots"`
		WriteRoots []string `json:"write_roots"`
		Network    bool     `json:"network"`
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return SandboxGrants{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return SandboxGrants{}, errors.New("multiple JSON values are not allowed")
		}
		return SandboxGrants{}, err
	}
	if len(wire.ReadRoots) > 0 && !options.ReadRoots {
		return SandboxGrants{}, errors.New("read roots are not supported")
	}
	if len(wire.WriteRoots) > 0 && !options.WriteRoots {
		return SandboxGrants{}, errors.New("write roots are not supported")
	}
	if wire.Network && !options.Network {
		return SandboxGrants{}, errors.New("network access is not supported")
	}
	return SandboxGrants{ReadRoots: wire.ReadRoots, WriteRoots: wire.WriteRoots, Network: wire.Network}, nil
}

func (model *AppModel) handleComposerKey(message tea.KeyPressMsg, key string) tea.Cmd {
	// History navigation: Up/Down recall previous submissions when there is
	// no vertical room for cursor movement (single-line draft, or cursor at
	// the first/last line boundary).
	if !model.slashSuggest.active {
		totalLines := model.composer.textarea.LineCount()
		cursorLine := model.composer.textarea.Line()
		switch key {
		case "up", "ctrl+p":
			if cursorLine == 0 && model.historyIdx > 0 {
				model.historyIdx--
				model.composer.SetValue(model.inputHistory[model.historyIdx])
				model.slashSuggest.updateSuggestions(model.slash, model.composer.Value())
				return nil
			}
		case "down", "ctrl+n":
			if cursorLine >= totalLines-1 && model.historyIdx < len(model.inputHistory) {
				model.historyIdx++
				if model.historyIdx >= len(model.inputHistory) {
					model.composer.SetValue("")
				} else {
					model.composer.SetValue(model.inputHistory[model.historyIdx])
				}
				model.slashSuggest.updateSuggestions(model.slash, model.composer.Value())
				return nil
			}
		}
	}

	// Slash-suggestion navigation keys intercept before the composer.
	if model.slashSuggest.active {
		switch key {
		case "tab":
			return model.acceptSlashSuggestion()
		case "enter":
			// Auto-complete the command name, preserving any arguments already typed.
			if sel := model.slashSuggest.selectedCommand(); sel != nil {
				draft := strings.TrimSpace(model.composer.Value())
				rest := strings.TrimPrefix(draft, "/")
				if spaceIdx := strings.Index(rest, " "); spaceIdx >= 0 {
					rest = rest[spaceIdx:] // preserve " plan" etc.
				} else {
					rest = ""
				}
				model.composer.SetValue("/" + sel.Name + rest + " ")
			}
			model.slashSuggest.active = false
			return model.submitDraft()
		case "esc":
			model.slashSuggest.active = false
			model.slashSuggest.suppressed = false
			return nil
		case "up", "ctrl+p":
			if len(model.slashSuggest.matches) > 0 {
				model.slashSuggest.selected = max(0, model.slashSuggest.selected-1)
			}
			return nil
		case "down", "ctrl+n":
			if len(model.slashSuggest.matches) > 0 {
				model.slashSuggest.selected = min(len(model.slashSuggest.matches)-1, model.slashSuggest.selected+1)
			}
			return nil
		}
	}

	var cmd tea.Cmd
	switch key {
	case "ctrl+j", "shift+enter", "alt+enter":
		model.composer.InsertString("\n")
	case "enter":
		return model.submitDraft()
	default:
		message.Text = SanitizeString(message.Text)
		cmd = model.composer.Update(message)
	}

	// After every composer mutation, recompute slash suggestions.
	model.slashSuggest.updateSuggestions(model.slash, model.composer.Value())
	return cmd
}

func (model *AppModel) acceptSlashSuggestion() tea.Cmd {
	sel := model.slashSuggest.selectedCommand()
	if sel == nil {
		return nil
	}
	draft := model.composer.Value()
	trimmed := strings.TrimSpace(draft)
	// Find the slash-word prefix and replace it.
	rest := strings.TrimPrefix(trimmed, "/")
	spaceIdx := strings.Index(rest, " ")
	if spaceIdx >= 0 {
		rest = rest[spaceIdx:]
	} else {
		rest = ""
	}
	model.composer.SetValue("/" + sel.Name + rest + " ")
	model.slashSuggest.active = false
	// Suppress reactivation until the user edits the command word.
	model.slashSuggest.suppressed = true
	model.slashSuggest.lastQuery = sel.Name
	return model.composer.Focus()
}

func (model *AppModel) submitDraft() tea.Cmd {
	draft := model.composer.Value()
	if model.runState == RunIdle && strings.TrimSpace(draft) == "" && model.pendingContinuation {
		model.composer.SetValue("Please continue from the point where the previous reply was cut off.")
		model.pendingContinuation = false
		return model.syncComposerFocus()
	}

	// Slash commands: intercept /-prefixed input before agent submission.
	if strings.HasPrefix(strings.TrimSpace(draft), "/") {
		model.slashSuggest.active = false
		model.pendingContinuation = false
		shouldBlock := model.runState == RunBooting || model.runState == RunStartupError || model.runState == RunQuitting || model.closing
		if shouldBlock && !isExitCommand(draft) {
			model.composer.Reset()
			return model.syncComposerFocus()
		}

		// Skill commands route to the agent run (ParseInvocations handles
		// skill body injection as transient context). Built-in commands are
		// dispatched locally.
		trimmed := strings.TrimSpace(draft)
		parts := strings.SplitN(trimmed[1:], " ", 2)
		name := strings.TrimSpace(parts[0])
		if cmd := model.slash.Lookup(name); cmd != nil && cmd.Kind == "skill" {
			model.composer.Reset()
			model.transcript.AddNotice("Loaded skill: "+name, model.clock.Now())
			model.noteLiveOutput()
			return model.submitToAgent(draft)
		}

		cmd := model.slash.Dispatch(model, draft)
		model.composer.Reset()
		return tea.Batch(cmd, model.syncComposerFocus())
	}

	if model.runState != RunIdle || strings.TrimSpace(draft) == "" || model.port == nil {
		return nil
	}
	return model.submitToAgent(draft)
}

// submitToAgent sends the input to the agent run and transitions to Running state.
func (model *AppModel) submitToAgent(input string) tea.Cmd {
	model.transcript.AddUser(input, model.clock.Now())
	model.noteLiveOutput()
	model.pendingSubmission = input
	model.inputHistory = append(model.inputHistory, input)
	model.historyIdx = len(model.inputHistory)
	model.pendingContinuation = false
	model.composer.Reset()
	model.runState = RunRunning
	model.activity = ActivityThinking
	model.quitArmedAt = time.Time{}
	runContext, cancel := context.WithCancel(context.Background())
	model.runCancel = cancel
	return tea.Batch(openRunCmd(model.port, runContext, input), model.ensureAnimation())
}

func (model *AppModel) cycleMode() tea.Cmd {
	if model.runState != RunIdle && model.runState != RunRunning {
		return nil
	}
	if !model.info.ModeChangeable || model.mode == ModeExternal || model.mode == ModeUnsupported || model.modeChangePending || model.port == nil {
		return nil
	}
	next := ModeDefault
	switch model.mode {
	case ModeDefault:
		next = ModeAutoAcceptEdits
	case ModeAutoAcceptEdits:
		next = ModePlan
	case ModePlan:
		next = ModeBypass
	case ModeBypass:
		next = ModeDefault
	}
	model.modeChangePending = true
	return setModeCmd(model.port, next)
}

func (model *AppModel) requestCancel() tea.Cmd {
	if model.runState == RunCancelling || model.runState == RunQuitting || model.runCancel == nil {
		return nil
	}
	model.runState = RunCancelling
	model.activity = ActivityCancelling
	return cancelCmd(model.runCancel)
}

func (model *AppModel) handleIdleControlC() tea.Cmd {
	now := model.clock.Now()
	if !model.quitArmedAt.IsZero() && now.Sub(model.quitArmedAt) <= idleQuitWindow {
		return model.beginQuit()
	}
	model.quitArmedAt = now
	return quitArmTimerCmd(model.clock, now)
}

func (model *AppModel) beginQuit() tea.Cmd {
	if model.runState == RunQuitting {
		return nil
	}
	model.runState = RunQuitting
	model.activity = ActivityCancelling
	model.quitArmedAt = time.Time{}
	model.composer.Blur()

	var reply tea.Cmd
	if model.permission != nil && !model.permission.Submitting {
		reply = model.submitPermission(DecisionDenyOnce)
	}
	cancel := cancelCmd(model.runCancel)
	if model.stream != nil && !model.terminalSeen {
		return tea.Batch(reply, cancel, quitDrainTimerCmd(model.clock))
	}
	return tea.Batch(reply, cancel, model.beginClose())
}

func (model *AppModel) protocolFailure(failure error, streamClosed bool) tea.Cmd {
	if failure == nil {
		failure = errors.New("unknown frontend protocol failure")
	}
	if model.fatalErr == nil {
		model.fatalErr = failure
		model.transcript.AddNotice("Fatal session protocol error: "+failure.Error(), model.clock.Now())
		model.noteLiveOutput()
	}
	model.runState = RunQuitting
	model.activity = ActivityCancelling
	model.forcedExit = true
	model.composer.Blur()

	var reply tea.Cmd
	if model.permission != nil && !model.permission.Submitting {
		reply = model.submitPermission(DecisionDenyOnce)
	}
	cancel := cancelCmd(model.runCancel)
	if streamClosed {
		return tea.Batch(reply, cancel, model.beginClose())
	}
	return tea.Batch(reply, cancel, quitDrainTimerCmd(model.clock))
}

func (model *AppModel) beginClose() tea.Cmd {
	if model.closing || model.closed {
		return nil
	}
	model.closing = true
	return closeSessionCmd(model.port, model.clock)
}

func (model *AppModel) ensureAnimation() tea.Cmd {
	if model.animationOn || !model.isActive() || model.theme.Mode.ReducedMotion {
		return nil
	}
	model.animationOn = true
	return activityTimerCmd(model.clock)
}

func (model *AppModel) isActive() bool {
	if model.runState == RunCancelling || model.runState == RunQuitting {
		return true
	}
	if model.runState != RunRunning {
		return false
	}
	return model.activity == ActivityThinking || model.activity == ActivityCallingTool || model.activity == ActivityCancelling
}

func (model *AppModel) noteLiveOutput() {
	if model.scroll.Mode == ScrollBrowsingHistory {
		model.scroll.Unread++
	}
}

func (model *AppModel) routeToComposer() bool {
	return model.permission == nil && model.overlay == nil && model.focus == FocusComposer && !model.closing
}

func (model *AppModel) syncComposerFocus() tea.Cmd {
	if model.routeToComposer() && model.layout.Class != LayoutTooSmall {
		if !model.composer.Focused() {
			return model.composer.Focus()
		}
		return nil
	}
	model.composer.Blur()
	return nil
}

func (model *AppModel) composerCursorRow() int {
	row := model.layout.Height - model.composerHeight(model.layout.ContentWidth)
	if model.slashSuggest.active {
		row += min(len(model.slashSuggest.matches), 8)
	}
	return row
}

func (model *AppModel) render() string {
	if model.layout.Class == LayoutTooSmall {
		return model.renderTooSmall()
	}
	width := model.layout.ContentWidth
	header := model.renderHeader(width)
	composerRows := model.composerHeight(width)
	blockingOverlay := model.permission != nil || model.overlay != nil
	if blockingOverlay {
		composerRows = 0
	}
	fixedRows := 2 + composerRows
	transcriptBudget := max(1, model.layout.Height-fixedRows)
	transcript := model.renderTranscript(width, transcriptBudget)
	footer := model.renderFooter(width)
	composer := model.renderComposer(width, composerRows)

	padding := strings.Repeat(" ", model.layout.HorizontalPadding)
	lines := make([]string, 0, model.layout.Height)
	lines = append(lines, padding+fitRendered(header, width)+padding)
	for _, line := range transcript {
		lines = append(lines, padding+fitRendered(line, width)+padding)
	}
	if !blockingOverlay {
		for _, line := range composer {
			lines = append(lines, padding+fitRendered(line, width)+padding)
		}
	}
	lines = append(lines, padding+fitRendered(footer, width)+padding)
	return strings.Join(lines, "\n")
}

func (model *AppModel) renderTooSmall() string {
	width := max(1, model.layout.Width)
	if model.overlay != nil {
		return strings.Join(model.renderOverlay(width, max(1, model.layout.Height)), "\n")
	}
	if model.permission != nil {
		return strings.Join(renderPermissionLines(model.theme, permissionRenderOptions{
			Width:       width,
			MaxRows:     max(1, model.layout.Height),
			Prompt:      model.permission.Prompt,
			Submitting:  model.permission.Submitting,
			Selected:    model.permission.Selected,
			Feedback:    model.permission.Feedback,
			TooSmall:    true,
			Scroll:      model.permission.Scroll,
			View:        model.permission.View,
			EditorLines: model.permissionEditorLines(),
			Grants:      model.permission.Grants,
		}), "\n")
	}
	lines := []string{
		"CORAGENT · TERMINAL TOO SMALL",
		fmt.Sprintf("current %dx%d · minimum %dx%d", model.layout.Width, model.layout.Height, MinimumTerminalWidth, MinimumTerminalHeight),
		"Resize to continue.",
	}
	if model.isActive() {
		lines = append(lines, "Esc/Ctrl+C cancel · Ctrl+Q quit")
	} else {
		lines = append(lines, "Ctrl+Q quit")
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], width, "")
	}
	return strings.Join(lines, "\n")
}

func (model *AppModel) renderHeader(width int) string {
	mark := "◉"
	if model.theme.Mode.ASCII {
		mark = "(*)"
	}
	hero := model.theme.AccentStyle.Render(mark) + " " + model.theme.StrongStyle.Render("coragent")
	return ansi.Truncate(hero, width, "")
}

func (model *AppModel) ledgerStatusLabel() string {
	if model.permission != nil {
		return "REVIEW"
	}
	if model.modeChangePending {
		return "MODE PENDING"
	}
	switch model.runState {
	case RunBooting:
		return "STARTING"
	case RunRunning:
		if model.activity != "" && model.activity != ActivityIdle {
			return strings.ToUpper(SanitizeString(string(model.activity)))
		}
		return "RUNNING"
	case RunCancelling:
		return "CANCELLING"
	case RunStartupError:
		return "STARTUP ERROR"
	case RunQuitting:
		return "CLOSING"
	default:
		return "READY"
	}
}

func sessionModeLabel(mode SessionMode) string {
	switch mode {
	case ModeDefault:
		return "DEFAULT"
	case ModeAutoAcceptEdits:
		return "AUTO EDIT"
	case ModePlan:
		return "PLAN"
	case ModeBypass:
		return "! BYPASS"
	case ModeExternal:
		return "EXTERNAL"
	default:
		return "UNSUPPORTED"
	}
}

func (model *AppModel) renderTranscript(width, rows int) []string {
	if model.overlay != nil {
		return model.renderOverlay(width, rows)
	}
	if model.permission != nil {
		all := model.transcript.RenderRows(model.theme, width, model.frame)
		permissionWidth := max(1, width)
		modalBudget := min(rows, max(9, rows*2/3))
		modal := renderPermissionLines(model.theme, permissionRenderOptions{
			Width:       permissionWidth,
			MaxRows:     modalBudget,
			Prompt:      model.permission.Prompt,
			Submitting:  model.permission.Submitting,
			Selected:    model.permission.Selected,
			Feedback:    model.permission.Feedback,
			Scroll:      model.permission.Scroll,
			View:        model.permission.View,
			EditorLines: model.permissionEditorLines(),
			Grants:      model.permission.Grants,
		})
		transcriptRows := max(0, rows-len(modal))
		visible := make([]string, 0, rows)
		if transcriptRows > 0 {
			top := model.resolvedScrollTop(all, transcriptRows)
			visible = selectTranscriptRows(all, transcriptRows, top)
		}
		gapRows := max(0, rows-len(visible)-len(modal))
		for range gapRows {
			visible = append(visible, "")
		}
		visible = append(visible, modal...)
		if len(visible) > rows {
			visible = visible[len(visible)-rows:]
		}
		return visible
	}
	all := model.renderedTranscriptRows(width)
	if len(all) == 0 {
		return model.renderStartupHero(width, rows)
	}
	top := model.resolvedScrollTop(all, rows)
	visible := selectTranscriptRows(all, rows, top)
	bar := renderTranscriptScrollbar(model.theme, len(all), rows, top)
	contentWidth := transcriptRenderWidth(width)
	for index := range visible {
		visible[index] = fitRendered(visible[index], contentWidth) + bar[index]
	}
	return visible
}

// renderStartupHero draws a chip/core ASCII-art graphic centered in the
// available transcript area. It adapts to three width tiers and falls back to
// plain text when the terminal is too narrow.
func (model *AppModel) renderStartupHero(width, rows int) []string {
	dot := "◉"
	brand := "c o r a g e n t"
	if model.theme.Mode.ASCII {
		dot = "+"
		brand = "coragent"
	}
	accent := model.theme.AccentStyle
	brandStyle := model.theme.StrongStyle

	switch {
	case width >= 51:
		return model.chipHero(width, rows, dot, brand, accent, brandStyle, true)
	case width >= 37:
		return model.chipHero(width, rows, dot, brand, accent, brandStyle, false)
	case width >= 20:
		return model.minimalHero(width, rows, dot, brand, accent, brandStyle)
	default:
		return model.textHero(width, rows, brand, brandStyle)
	}
}

// chipHero draws a nested chip package: outer border → chip body → 2×2 core
// grid → brand name below. doubleBorder selects ╔═╗ vs ╭─╮.
func (model *AppModel) chipHero(width, rows int, dot, brand string, accent, brandStyle lipgloss.Style, doubleBorder bool) []string {
	var tl, tr, bl, br, h, v string
	var itl, itr, ibl, ibr, ih, iv string
	if doubleBorder {
		tl, tr, bl, br, h, v = "╔", "╗", "╚", "╝", "═", "║"
		itl, itr, ibl, ibr, ih, iv = "┌", "┐", "└", "┘", "─", "│"
	} else {
		tl, tr, bl, br, h, v = "╭", "╮", "╰", "╯", "─", "│"
		itl, itr, ibl, ibr, ih, iv = "┌", "┐", "└", "┘", "─", "│"
	}

	dots := accent.Render(dot)
	gap := strings.Repeat(" ", max(1, (width-30)/2))
	gap = gap[:min(len(gap), 5)]
	coreRow := dots + gap + dots
	innerPad := 2
	chipW := CellWidth(coreRow) + innerPad*2
	chipTop := itl + strings.Repeat(ih, chipW) + itr
	chipMid1 := iv + strings.Repeat(" ", innerPad) + coreRow + strings.Repeat(" ", innerPad) + iv
	chipMid2 := iv + strings.Repeat(" ", innerPad) + coreRow + strings.Repeat(" ", innerPad) + iv
	chipBot := ibl + strings.Repeat(ih, chipW) + ibr

	outerPad := 2
	outerW := chipW + outerPad*2
	outerTop := tl + strings.Repeat(h, outerW) + tr
	outerMid1 := v + strings.Repeat(" ", outerPad) + chipTop + strings.Repeat(" ", outerPad) + v
	outerMid2 := v + strings.Repeat(" ", outerPad) + chipMid1 + strings.Repeat(" ", outerPad) + v
	outerMid3 := v + strings.Repeat(" ", outerPad) + chipMid2 + strings.Repeat(" ", outerPad) + v
	outerMid4 := v + strings.Repeat(" ", outerPad) + chipBot + strings.Repeat(" ", outerPad) + v
	outerBot := bl + strings.Repeat(h, outerW) + br

	brandLine := brandStyle.Render(brand)
	if CellWidth(brand) < outerW+2 {
		brandLine = brandStyle.Render(strings.ReplaceAll(brand, " ", strings.Repeat(" ", (outerW+2-CellWidth(brand))/(len(brand)/2))))
	}

	hero := []string{outerTop, outerMid1, outerMid2, outerMid3, outerMid4, outerBot, "", brandLine}
	return centerLines(hero, width, rows)
}

// minimalHero draws a simpler single-box 2×2 core grid without the outer
// chip package.
func (model *AppModel) minimalHero(width, rows int, dot, brand string, accent, brandStyle lipgloss.Style) []string {
	dots := accent.Render(dot)
	gap := strings.Repeat(" ", max(1, (width-16)/2))
	gap = gap[:min(len(gap), 3)]
	coreRow := dots + gap + dots

	var tl, tr, bl, br, h, v string
	tl, tr, bl, br, h, v = "╭", "╮", "╰", "╯", "─", "│"

	pad := 2
	boxW := CellWidth(coreRow) + pad*2
	boxTop := tl + strings.Repeat(h, boxW) + tr
	boxMid1 := v + strings.Repeat(" ", pad) + coreRow + strings.Repeat(" ", pad) + v
	boxMid2 := v + strings.Repeat(" ", pad) + coreRow + strings.Repeat(" ", pad) + v
	boxBot := bl + strings.Repeat(h, boxW) + br

	brandLine := brandStyle.Render(brand)

	hero := []string{boxTop, boxMid1, boxMid2, boxBot, "", brandLine}
	return centerLines(hero, width, rows)
}

// textHero renders a plain centered brand name for very narrow terminals.
func (model *AppModel) textHero(width, rows int, brand string, brandStyle lipgloss.Style) []string {
	hero := []string{"", brandStyle.Render(brand)}
	return centerLines(hero, width, rows)
}

func centerLines(lines []string, width, rows int) []string {
	result := make([]string, rows)
	start := max(0, (rows-len(lines))/2)
	for i, line := range lines {
		idx := start + i
		if idx >= rows {
			break
		}
		textW := ansi.StringWidth(line)
		if textW >= width {
			result[idx] = ansi.Truncate(line, width, "")
		} else {
			pad := (width - textW) / 2
			result[idx] = strings.Repeat(" ", pad) + line
		}
	}
	for i := range result {
		if result[i] == "" {
			result[i] = strings.Repeat(" ", width)
		}
	}
	return result
}

func (model *AppModel) permissionEditorLines() []string {
	if model.permission == nil || model.permission.View == permissionDecision {
		return nil
	}
	return model.permission.Editor.Lines()
}

func (model *AppModel) renderFooter(width int) string {
	separator := visualSeparator(model.theme)

	// Left side: project ● branch ● mode — then activity / browsing status.
	leftParts := make([]string, 0, 4)
	dotSep := "  ●  "
	if model.theme.Mode.ASCII {
		dotSep = "  *  "
	}
	primary := make([]string, 0, 3)
	if project := SanitizeString(model.info.Project); project != "" {
		primary = append(primary, filepath.Base(project))
	}
	if branch := model.info.Branch; branch != "" {
		primary = append(primary, model.theme.AccentStyle.Render(branch))
	}
	modeLabel := sessionModeLabel(model.mode)
	modeStyle := model.theme.AccentStyle
	switch model.mode {
	case ModeAutoAcceptEdits:
		modeStyle = model.theme.SuccessStyle
	case ModePlan:
		modeStyle = model.theme.InfoStyle
	case ModeBypass:
		modeStyle = model.theme.DangerStyle
	}
	modeText := modeStyle.Render(modeLabel) + " " + model.theme.MutedStyle.Render("(shift+tab)")
	primary = append(primary, modeText)
	if len(primary) > 0 {
		leftParts = append(leftParts, strings.Join(primary, dotSep))
	}
	if model.activity != "" && model.activity != ActivityIdle {
		leftParts = append(leftParts, model.theme.InfoStyle.Render(string(model.activity)))
	}
	left := strings.Join(leftParts, " " + separator + " ")

	// Right side: sandbox, model, context.
	sandbox := SanitizeString(model.info.Sandbox)
	if sandbox == "" {
		sandbox = "unknown"
	}
	meta := []string{"sandbox " + sandbox}
	if model.layout.ShowModel && model.info.Model != "" {
		meta = append(meta, SanitizeString(model.info.Model))
	}
	contextLabel, contextLevel := model.contextUsageLabel()
	meta = append(meta, contextLabel)

	leftPlain := strings.Join(leftParts, " "+separator+" ")
	for len(meta) > 1 && CellWidth(strings.Join(meta, separator))+CellWidth(leftPlain)+2 > width {
		meta = meta[:len(meta)-1]
	}
	right := ""
	for index := range meta {
		style := model.theme.MutedStyle
		if meta[index] == contextLabel {
			switch contextLevel {
			case 2:
				style = model.theme.DangerStyle
			case 1:
				style = model.theme.WarningStyle
			}
		}
		if index > 0 {
			right += model.theme.MutedStyle.Render(separator)
		}
		right += style.Render(meta[index])
	}

	rightPlain := strings.Join(meta, separator)
	space := max(1, width-CellWidth(leftPlain)-CellWidth(rightPlain))
	line := left + strings.Repeat(" ", space) + right
	return ansi.Truncate(line, width, "")
}

func (model *AppModel) contextUsageLabel() (string, int) {
	if model.usage == nil {
		return "0%", 0
	}
	usage := model.usage
	tokens := formatTokenCount(usage.Used)
	source := ""
	if usage.Source == "estimated" {
		source = " est"
	}
	window := usage.Window.Value
	if !usage.Window.Known || window == 0 {
		window = uint64(modelWindow(model.info.Model))
		if window == 0 {
			return tokens + source, 0
		}
	}
	ratio := float64(usage.Used) / float64(window)
	percent := int(ratio * 100)
	pctStr := fmt.Sprintf("%d%%", percent)
	if ratio < 0.1 && ratio > 0 {
		pctStr = fmt.Sprintf("%.1f%%", ratio*100)
	}
	level := 0
	if percent >= 95 {
		level = 2
	} else if percent >= 80 {
		level = 1
	}
	return fmt.Sprintf("%s · %s%s", pctStr, tokens, source), level
}

func modelWindow(model string) int {
	if strings.HasSuffix(model, "[1m]") {
		return 1_000_000
	}
	// When model name is reported by the provider (not from config), it won't
	// carry the suffix. Default to the standard 200K assumption.
	if model != "" {
		return 200_000
	}
	return 0
}

func formatTokenCount(value uint64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	}
	return fmt.Sprintf("%d", value)
}

func (model *AppModel) composerHeight(width int) int {
	if model.layout.Class == LayoutTooSmall {
		return 0
	}
	maxRows := max(model.layout.ComposerMinRows, model.layout.ComposerMaxRows)
	base := min(maxRows, max(model.layout.ComposerMinRows, model.composer.Height()+2))
	if model.slashSuggest.active {
		base += min(len(model.slashSuggest.matches), 8)
	}
	return base
}

func (model *AppModel) composerPlaceholder() string {
	switch model.runState {
	case RunIdle:
		return "Describe what you want changed..."
	case RunRunning, RunCancelling:
		return "Esc interrupts the current run"
	case RunBooting:
		return "Starting session..."
	case RunStartupError:
		return "Startup failed · Ctrl+Q quit"
	default:
		return "Closing session..."
	}
}

func (model *AppModel) renderSlashSuggestions(width int, rows *int) []string {
	if !model.slashSuggest.active || len(model.slashSuggest.matches) == 0 {
		return nil
	}
	maxVisible := min(len(model.slashSuggest.matches), 8)
	*rows -= maxVisible

	glyph := "▶"
	space := "  "
	pad := " "
	if model.theme.Mode.ASCII {
		glyph = ">"
	}

	lines := make([]string, 0, maxVisible)
	for i, cmd := range model.slashSuggest.matches {
		if i >= maxVisible {
			break
		}
		marker := space
		style := model.theme.MutedStyle
		if i == model.slashSuggest.selected {
			marker = glyph
			style = model.theme.AccentStyle
		}

		aliasStr := ""
		if len(cmd.Aliases) > 0 {
			aliasParts := make([]string, len(cmd.Aliases))
			for j, a := range cmd.Aliases {
				aliasParts[j] = "/" + a
			}
			aliasStr = "(" + strings.Join(aliasParts, ", ") + ")"
		}

		// Skill entries show a source badge ([user] or [project]) instead of aliases.
		sourceStr := ""
		if cmd.Kind == "skill" && cmd.Source != "" {
			sourceStr = "[" + cmd.Source + "]"
		}

		namePart := "/" + cmd.Name
		descPart := cmd.Description

		// Layout: "▶ /name  (aliases|source)  description"
		fixed := CellWidth(marker) + 1 + CellWidth(namePart) + 1
		badge := aliasStr
		if badge == "" {
			badge = sourceStr
		}
		if badge != "" {
			fixed += CellWidth(badge) + 1
		}
		descWidth := max(0, width-fixed-1)
		if descWidth > 0 && CellWidth(descPart) > descWidth {
			descPart = ansi.Truncate(descPart, descWidth, model.theme.Glyphs.Ellipsis)
		}

		line := style.Render(marker) + pad + style.Render(namePart)
		if badge != "" {
			line += pad + model.theme.MutedStyle.Render(badge)
		}
		if descWidth > 0 && descPart != "" {
			line += pad + model.theme.MutedStyle.Render(descPart)
		}
		lines = append(lines, line)
	}
	return lines
}

func (model *AppModel) renderComposer(width, rows int) []string {
	if rows <= 0 {
		return nil
	}

	// Show slash-command suggestions above the composer inline.
	suggestLines := model.renderSlashSuggestions(width, &rows)

	rows = max(3, rows)
	placeholder := model.composerPlaceholder()
	if model.theme.Mode.ASCII {
		placeholder = strings.ReplaceAll(placeholder, " · ", " | ")
	}
	model.composer.SetPlaceholder(placeholder)
	wrapped := model.composer.Lines()
	innerRows := rows - 2
	if len(wrapped) > innerRows {
		wrapped = wrapped[len(wrapped)-innerRows:]
	}
	borderStyle := model.theme.BorderStyle
	if model.focus == FocusComposer && model.permission == nil {
		borderStyle = model.theme.AccentStyle
	}
	rule := strings.Repeat(model.theme.Border.Top, width)
	lines := make([]string, 0, len(suggestLines)+2+innerRows)
	for _, s := range suggestLines {
		lines = append(lines, fitRendered(s, width))
	}
	lines = append(lines, borderStyle.Render(rule))
	for index := 0; index < innerRows; index++ {
		line := ""
		if index < len(wrapped) {
			line = wrapped[index]
		}
		lines = append(lines, fitRendered(line, width))
	}
	lines = append(lines, borderStyle.Render(rule))
	return lines
}

func selectTranscriptRows(lines []RenderedTranscriptLine, rows, top int) []string {
	if rows <= 0 {
		return nil
	}
	if len(lines) == 0 {
		return make([]string, rows)
	}
	maxTop := max(0, len(lines)-rows)
	top = min(max(0, top), maxTop)
	end := min(len(lines), top+rows)
	visible := make([]string, 0, rows)
	for _, line := range lines[top:end] {
		visible = append(visible, line.Text)
	}
	if len(visible) < rows {
		visible = append(visible, make([]string, rows-len(visible))...)
	}
	return visible
}

func fitRendered(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = ansi.Truncate(value, width, "")
	return value + strings.Repeat(" ", max(0, width-CellWidth(value)))
}

type startupLoadedMsg struct {
	Info SessionInfo
	Err  error
}

type runOpenedMsg struct {
	Stream <-chan UIEvent
	Err    error
}

type eventReadMsg struct {
	Stream <-chan UIEvent
	Event  UIEvent
}

type eventEOFMsg struct{ Stream <-chan UIEvent }

type permissionReplyMsg struct {
	RequestID string
	Result    PermissionReplyResult
	Err       error
}

type modeChangedMsg struct {
	Mode SessionMode
	Err  error
}

type activityTickMsg struct{}
type quitDrainExpiredMsg struct{}
type quitArmExpiredMsg struct{ At time.Time }

type closeDoneMsg struct {
	Err      error
	TimedOut bool
}

func describeSessionCmd(port SessionPort) tea.Cmd {
	return func() tea.Msg {
		if port == nil {
			return startupLoadedMsg{Err: errors.New("session port is nil")}
		}
		info, err := port.Describe(context.Background())
		return startupLoadedMsg{Info: info, Err: err}
	}
}

func openRunCmd(port SessionPort, ctx context.Context, input string) tea.Cmd {
	return func() tea.Msg {
		stream, err := port.Run(ctx, input)
		return runOpenedMsg{Stream: stream, Err: err}
	}
}

func readEventCmd(stream <-chan UIEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-stream
		if !ok {
			return eventEOFMsg{Stream: stream}
		}
		return eventReadMsg{Stream: stream, Event: event}
	}
}

func replyPermissionResponseCmd(prompt PermissionPrompt, response PermissionResponse) tea.Cmd {
	return func() tea.Msg {
		if prompt.RichReply != nil {
			result, err := prompt.RichReply(context.Background(), response)
			return permissionReplyMsg{RequestID: prompt.RequestID, Result: result, Err: err}
		}
		if prompt.Reply == nil {
			return permissionReplyMsg{RequestID: prompt.RequestID, Err: errors.New("permission reply path is unavailable")}
		}
		if response.RevisedArguments != nil || len(response.Grants.ReadRoots) > 0 || len(response.Grants.WriteRoots) > 0 || response.Grants.Network || response.Remember {
			return permissionReplyMsg{RequestID: prompt.RequestID, Err: errors.New("permission reply path does not support rich decisions")}
		}
		result, err := prompt.Reply(context.Background(), response.Decision)
		return permissionReplyMsg{RequestID: prompt.RequestID, Result: result, Err: err}
	}
}

func setModeCmd(port SessionPort, mode SessionMode) tea.Cmd {
	return func() tea.Msg {
		err := port.SetMode(context.Background(), mode)
		return modeChangedMsg{Mode: mode, Err: err}
	}
}

func cancelCmd(cancel context.CancelFunc) tea.Cmd {
	if cancel == nil {
		return nil
	}
	return func() tea.Msg {
		cancel()
		return nil
	}
}

func activityTimerCmd(clock Clock) tea.Cmd {
	return func() tea.Msg {
		<-clock.After(activityPeriod)
		return activityTickMsg{}
	}
}

func quitArmTimerCmd(clock Clock, armedAt time.Time) tea.Cmd {
	return func() tea.Msg {
		at := <-clock.After(idleQuitWindow)
		if at.IsZero() {
			at = armedAt.Add(idleQuitWindow)
		}
		return quitArmExpiredMsg{At: at}
	}
}

func quitDrainTimerCmd(clock Clock) tea.Cmd {
	return func() tea.Msg {
		<-clock.After(quitDrainLimit)
		return quitDrainExpiredMsg{}
	}
}

func closeSessionCmd(port SessionPort, clock Clock) tea.Cmd {
	return func() tea.Msg {
		if port == nil {
			return closeDoneMsg{}
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- port.Close(ctx) }()
		select {
		case err := <-result:
			return closeDoneMsg{Err: err}
		case <-clock.After(closeLimit):
			return closeDoneMsg{Err: context.DeadlineExceeded, TimedOut: true}
		}
	}
}

func copySelectionCmd(writeNative func(string) error, text string) tea.Cmd {
	terminal := tea.SetClipboard(text)
	if writeNative == nil {
		return terminal
	}
	native := func() tea.Msg {
		_ = writeNative(text)
		return nil
	}
	return tea.Batch(native, terminal)
}
