package tools

// Argument helpers shared by the built-in tools. Tool arguments arrive as a
// map[string]interface{} (decoded JSON or constructed by an SDK caller), so these
// extract typed values leniently — numbers may be float64 or a native Go integer.

// stringArg returns the string value for key, or ("", false) if absent or not a
// string.
func stringArg(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// intArg returns the integer value for key, or (0, false) if absent or not a
// number. JSON numbers decode to float64; native ints from SDK callers are also
// accepted.
func intArg(args map[string]interface{}, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	default:
		return 0, false
	}
}

// boolArg returns the boolean value for key, defaulting to false if absent or not
// a bool.
func boolArg(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}
