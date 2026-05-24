package internal

// IntFromAny converts a JSON-decoded numeric value to int. Returns 0 for
// unsupported types. Handles float64 (JSON default), int, and int64.
func IntFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
