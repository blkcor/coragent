## 1. Core Implementation

- [x] 1.1 Add `inputHistory []string` and `historyIdx int` fields to `AppModel` in `tui/app.go`
- [x] 1.2 Initialize `historyIdx` to 0 in the model constructor (representing "current draft" position)
- [x] 1.3 In `submitDraft`, append non-empty drafts to `inputHistory` before calling `composer.Reset()`, and reset `historyIdx` to `len(inputHistory)`
- [x] 1.4 In `handleComposerKey`, intercept Up/Down keys when: composer is in non-slash mode, and the draft has no vertical room for cursor movement. Up decrements `historyIdx` and calls `SetValue`; Down increments and clears when past newest

## 2. Unit Tests

- [x] 2.1 Add table-driven tests in `tui/app_test.go` verifying: Up recalls previous input, Down walks forward, empty submissions are excluded, multi-line drafts keep Up/Down for cursor movement
