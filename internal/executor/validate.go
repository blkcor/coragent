package executor

import (
	"encoding/json"
	"fmt"
)

// argSchema is the minimal subset of JSON Schema the executor validates a call's
// arguments against: which properties exist, their primitive type, and which are
// required. Full JSON-Schema validation is out of scope for Phase 2; this catches
// the shape mismatches that matter — a missing required field or a wrong type.
type argSchema struct {
	Properties map[string]struct {
		Type string `json:"type"`
	} `json:"properties"`
	Required []string `json:"required"`
}

// validateArgs checks args against a tool's declared JSON-schema parameters. It
// returns a validation error describing the first mismatch, or nil when the
// arguments fit. An empty or unparriseable schema is treated as "no constraints".
func validateArgs(schema json.RawMessage, args map[string]interface{}) error {
	if len(schema) == 0 {
		return nil
	}
	var s argSchema
	if err := json.Unmarshal(schema, &s); err != nil {
		// A schema we cannot parse imposes no constraints rather than failing
		// every call; the tool descriptor is the author's responsibility.
		return nil
	}

	for _, req := range s.Required {
		if _, ok := args[req]; !ok {
			return fmt.Errorf("missing required argument %q", req)
		}
	}

	for name, prop := range s.Properties {
		v, ok := args[name]
		if !ok || prop.Type == "" {
			continue
		}
		if !typeMatches(prop.Type, v) {
			return fmt.Errorf("argument %q must be of type %s", name, prop.Type)
		}
	}
	return nil
}

// typeMatches reports whether v satisfies a JSON-schema primitive type. Numbers
// are lenient: arguments may arrive as float64 (decoded JSON) or as native Go
// integers (constructed in tests / by SDK callers).
func typeMatches(jsonType string, v interface{}) bool {
	switch jsonType {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number", "integer":
		switch v.(type) {
		case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
			return true
		default:
			return false
		}
	case "array":
		switch v.(type) {
		case []interface{}, []string:
			return true
		default:
			return false
		}
	case "object":
		_, ok := v.(map[string]interface{})
		return ok
	default:
		return true // unknown declared type imposes no constraint
	}
}
