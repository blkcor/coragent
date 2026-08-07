package transcript

import (
	"errors"
	"fmt"
)

var (
	// ErrRecordSequence rejects a transcript whose record sequence numbers
	// are not contiguous from 1. A gap or duplicate means a record was lost
	// or rewritten and loading fails closed.
	ErrRecordSequence = errors.New("transcript: record sequence is not contiguous from 1")
	// ErrUnpairedToolCall rejects a transcript containing a tool call with
	// no terminal tool result. Such a transcript can never be reused for a
	// Provider request.
	ErrUnpairedToolCall = errors.New("transcript: tool call has no terminal tool result")
	// ErrDuplicateToolResult rejects a transcript containing more than one
	// result for the same tool call.
	ErrDuplicateToolResult = errors.New("transcript: tool call has more than one tool result")
	// ErrDuplicateToolCall rejects a transcript containing two tool calls
	// with the same call ID.
	ErrDuplicateToolCall = errors.New("transcript: duplicate tool call ID")
	// ErrOrphanToolResult rejects a transcript containing a tool result that
	// references no earlier tool call.
	ErrOrphanToolResult = errors.New("transcript: tool result references no tool call")
	// ErrToolRunMismatch rejects a result correlated to a call from another run.
	ErrToolRunMismatch = errors.New("transcript: tool call and result run IDs differ")
	// ErrTerminalWithOpenCall rejects a terminal run outcome before all calls
	// proposed by that run have terminal results.
	ErrTerminalWithOpenCall  = errors.New("transcript: run terminated with an open tool call")
	ErrDuplicateSessionClose = errors.New("transcript: duplicate session close fact")
	ErrRecordsAfterClose     = errors.New("transcript: records appear after session close")
	// ErrDuplicateActionRequest rejects two action_prepared records with the
	// same request_id.
	ErrDuplicateActionRequest = errors.New("transcript: duplicate action_prepared request_id")
	// ErrOrphanActionRecord rejects an action lifecycle record whose
	// request_id has no preceding action_prepared.
	ErrOrphanActionRecord = errors.New("transcript: action record references unknown request_id")
	// ErrActionWithoutApproval rejects action_committing without a preceding
	// action_approved for the same request_id.
	ErrActionWithoutApproval = errors.New("transcript: action_committing without preceding action_approved")
	// ErrCommittedWithoutCommitting rejects action_committed without a
	// preceding action_committing for the same request_id.
	ErrCommittedWithoutCommitting = errors.New("transcript: action_committed without preceding action_committing")
	// ErrDuplicateActionCommitted rejects two action_committed records for
	// the same request_id.
	ErrDuplicateActionCommitted = errors.New("transcript: duplicate action_committed request_id")
	// ErrActionAfterTerminal rejects an action record that appears after the
	// lifecycle has already been terminated by denied, aborted, or committed.
	ErrActionAfterTerminal = errors.New("transcript: action record after terminal lifecycle state")
)

// actionState tracks the lifecycle state of one request_id.
type actionState struct {
	prepared    bool
	approved    bool
	denied      bool
	committing  bool
	committed   bool
	aborted     bool
	abortReason AbortReason
}

func (s actionState) isTerminal() bool {
	return s.denied || s.committed || s.aborted
}

// ValidateTranscript checks a loaded transcript end to end: per-record
// validity, contiguous sequence numbers from 1, and the tool-call pairing
// invariant — every ToolCall has exactly one terminal ToolResult, recorded
// after the call, before the transcript may be reused for a Provider
// request. Any violation stops loading with a typed cause; records are never
// skipped or rewritten.
func ValidateTranscript(records []Record) error {
	if err := ValidateRecords(records); err != nil {
		return err
	}
	calls, results, err := toolPairs(records)
	if err != nil {
		return err
	}
	for callID, seq := range calls {
		if _, ok := results[callID]; !ok {
			return fmt.Errorf("%w: %q (call at seq %d)", ErrUnpairedToolCall, callID, seq)
		}
	}
	return nil
}

// ValidateRecords validates the durable log without requiring incomplete
// calls to be paired. Store loading uses this form so startup reconciliation
// can repair interrupted read-only calls before model context is assembled.
func ValidateRecords(records []Record) error {
	closed := false
	for i, rec := range records {
		want := uint64(i + 1)
		if rec.Seq != want {
			return fmt.Errorf("%w: record at position %d has seq %d", ErrRecordSequence, i, rec.Seq)
		}
		if err := rec.Validate(); err != nil {
			return fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
		}
		if closed {
			if rec.Kind == KindSessionClosed {
				return fmt.Errorf("%w: seq %d", ErrDuplicateSessionClose, rec.Seq)
			}
			return fmt.Errorf("%w: seq %d", ErrRecordsAfterClose, rec.Seq)
		}
		if rec.Kind == KindSessionClosed {
			closed = true
		}
	}
	if _, _, err := toolPairs(records); err != nil {
		return err
	}
	return validateActionLifecycle(records)
}

// validateActionLifecycle checks action record pairing rules:
//
//	prepared → approved / denied / aborted
//	approved → committing → committed / aborted
//
// Duplicate prepared, duplicate committed, records after terminal state,
// and records with no preceding prepared are all rejected.
func validateActionLifecycle(records []Record) error {
	states := make(map[string]*actionState)
	for _, rec := range records {
		var reqID string
		switch rec.Kind {
		case KindActionPrepared:
			var p ActionPreparedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			reqID = p.RequestID
			if _, exists := states[reqID]; exists {
				return fmt.Errorf("%w: %q (seq %d)", ErrDuplicateActionRequest, reqID, rec.Seq)
			}
			states[reqID] = &actionState{prepared: true}
		case KindActionApproved:
			var p ActionApprovedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			reqID = p.RequestID
			s, ok := states[reqID]
			if !ok {
				return fmt.Errorf("%w: approved %q (seq %d)", ErrOrphanActionRecord, reqID, rec.Seq)
			}
			if s.approved {
				return fmt.Errorf("%w: approved %q (seq %d)", ErrDuplicateActionRequest, reqID, rec.Seq)
			}
			if s.isTerminal() {
				return fmt.Errorf("%w: approved %q after terminal state (seq %d)", ErrActionAfterTerminal, reqID, rec.Seq)
			}
			s.approved = true
		case KindActionDenied:
			var p ActionDeniedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			reqID = p.RequestID
			s, ok := states[reqID]
			if !ok {
				return fmt.Errorf("%w: denied %q (seq %d)", ErrOrphanActionRecord, reqID, rec.Seq)
			}
			if s.isTerminal() {
				return fmt.Errorf("%w: denied %q after terminal state (seq %d)", ErrActionAfterTerminal, reqID, rec.Seq)
			}
			s.denied = true
		case KindActionCommitting:
			var p ActionCommittingPayload
			if err := rec.DecodePayload(&p); err != nil {
				return fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			reqID = p.RequestID
			s, ok := states[reqID]
			if !ok {
				return fmt.Errorf("%w: committing %q (seq %d)", ErrOrphanActionRecord, reqID, rec.Seq)
			}
			if s.isTerminal() {
				return fmt.Errorf("%w: committing %q after terminal state (seq %d)", ErrActionAfterTerminal, reqID, rec.Seq)
			}
			if !s.approved {
				return fmt.Errorf("%w: committing %q (seq %d)", ErrActionWithoutApproval, reqID, rec.Seq)
			}
			s.committing = true
		case KindActionCommitted:
			var p ActionCommittedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			reqID = p.RequestID
			s, ok := states[reqID]
			if !ok {
				return fmt.Errorf("%w: committed %q (seq %d)", ErrOrphanActionRecord, reqID, rec.Seq)
			}
			if s.committed {
				return fmt.Errorf("%w: committed %q (seq %d)", ErrDuplicateActionCommitted, reqID, rec.Seq)
			}
			if s.isTerminal() {
				return fmt.Errorf("%w: committed %q after terminal state (seq %d)", ErrActionAfterTerminal, reqID, rec.Seq)
			}
			if !s.committing {
				return fmt.Errorf("%w: committed %q (seq %d)", ErrCommittedWithoutCommitting, reqID, rec.Seq)
			}
			s.committed = true
		case KindActionAborted:
			var p ActionAbortedPayload
			if err := rec.DecodePayload(&p); err != nil {
				return fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			reqID = p.RequestID
			s, ok := states[reqID]
			if !ok {
				return fmt.Errorf("%w: aborted %q (seq %d)", ErrOrphanActionRecord, reqID, rec.Seq)
			}
			if s.isTerminal() {
				return fmt.Errorf("%w: aborted %q after terminal state (seq %d)", ErrActionAfterTerminal, reqID, rec.Seq)
			}
			s.aborted = true
			s.abortReason = p.Reason
		}
	}
	return nil
}

// OpenToolCalls returns complete call payloads that have no terminal result,
// preserving Provider order. It rejects all other pairing corruption.
func OpenToolCalls(records []Record) ([]ToolCallPayload, error) {
	if err := ValidateRecords(records); err != nil {
		return nil, err
	}
	_, results, err := toolPairs(records)
	if err != nil {
		return nil, err
	}
	var open []ToolCallPayload
	for _, rec := range records {
		if rec.Kind != KindToolCall {
			continue
		}
		var p ToolCallPayload
		if err := rec.DecodePayload(&p); err != nil {
			return nil, err
		}
		if _, ok := results[p.CallID]; !ok {
			open = append(open, p)
		}
	}
	return open, nil
}

// OpenToolCallsForRun returns the unpaired calls for the one durable active
// run being reconciled. An open call from any other run makes the durable state
// ambiguous and is rejected before recovery appends a fact.
func OpenToolCallsForRun(records []Record, runID string) ([]ToolCallPayload, error) {
	if runID == "" {
		return nil, fmt.Errorf("%w: recovery run ID is empty", ErrUnpairedToolCall)
	}
	if err := ValidateRecords(records); err != nil {
		return nil, err
	}
	_, results, err := toolPairs(records)
	if err != nil {
		return nil, err
	}
	var open []ToolCallPayload
	for _, rec := range records {
		if rec.Kind != KindToolCall {
			continue
		}
		var payload ToolCallPayload
		if err := rec.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if _, paired := results[payload.CallID]; paired {
			continue
		}
		if rec.RunID != runID {
			return nil, fmt.Errorf("%w: open call %q belongs to non-active run %q, active run %q", ErrUnpairedToolCall, payload.CallID, rec.RunID, runID)
		}
		open = append(open, payload)
	}
	return open, nil
}

func toolPairs(records []Record) (map[string]uint64, map[string]uint64, error) {
	calls := make(map[string]uint64)   // call ID -> call record seq
	results := make(map[string]uint64) // call ID -> result record seq
	callRuns := make(map[string]string)
	for _, rec := range records {
		switch rec.Kind {
		case KindToolCall:
			var p ToolCallPayload
			if err := rec.DecodePayload(&p); err != nil {
				return nil, nil, fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			if _, dup := calls[p.CallID]; dup {
				return nil, nil, fmt.Errorf("%w: %q (seq %d)", ErrDuplicateToolCall, p.CallID, rec.Seq)
			}
			calls[p.CallID] = rec.Seq
			callRuns[p.CallID] = rec.RunID
		case KindToolResult:
			var p ToolResultPayload
			if err := rec.DecodePayload(&p); err != nil {
				return nil, nil, fmt.Errorf("transcript: record %d: %w", rec.Seq, err)
			}
			if _, ok := calls[p.CallID]; !ok {
				return nil, nil, fmt.Errorf("%w: %q (seq %d)", ErrOrphanToolResult, p.CallID, rec.Seq)
			}
			if _, dup := results[p.CallID]; dup {
				return nil, nil, fmt.Errorf("%w: %q (seq %d)", ErrDuplicateToolResult, p.CallID, rec.Seq)
			}
			if callRuns[p.CallID] != rec.RunID {
				return nil, nil, fmt.Errorf("%w: %q call run %q, result run %q", ErrToolRunMismatch, p.CallID, callRuns[p.CallID], rec.RunID)
			}
			results[p.CallID] = rec.Seq
		case KindRunOutcome:
			for callID, callRun := range callRuns {
				if callRun == rec.RunID {
					if _, ok := results[callID]; !ok {
						return nil, nil, fmt.Errorf("%w: %q in run %q", ErrTerminalWithOpenCall, callID, rec.RunID)
					}
				}
			}
		}
	}
	return calls, results, nil
}
