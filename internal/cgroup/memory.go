package cgroup

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// ParseMemoryLimit parses and validates a memory limit specification.
// Valid formats:
//   - "" (empty, meaning omitted/no limit)
//   - "max" (case-insensitive, cgroups v2 unlimited)
//   - Numeric value with optional unit: K, M, G (case-insensitive) or raw bytes
// Zero, negative, malformed, or overflowing values return an error.
func ParseMemoryLimit(input string) (string, int64, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", 0, nil
	}

	if strings.EqualFold(trimmed, "max") {
		return "max", -1, nil
	}

	// Reject any internal whitespace
	for _, r := range trimmed {
		if unicode.IsSpace(r) {
			return "", 0, fmt.Errorf("invalid memory limit %q: contains whitespace", input)
		}
	}

	// Determine unit multiplier
	multiplier := int64(1)
	numStr := trimmed
	lastChar := trimmed[len(trimmed)-1]

	switch lastChar {
	case 'k', 'K':
		multiplier = 1024
		numStr = trimmed[:len(trimmed)-1]
	case 'm', 'M':
		multiplier = 1024 * 1024
		numStr = trimmed[:len(trimmed)-1]
	case 'g', 'G':
		multiplier = 1024 * 1024 * 1024
		numStr = trimmed[:len(trimmed)-1]
	default:
		if !unicode.IsDigit(rune(lastChar)) {
			return "", 0, fmt.Errorf("invalid memory limit %q: invalid unit suffix %q (supported: K, M, G, or raw bytes)", input, string(lastChar))
		}
	}

	if numStr == "" {
		return "", 0, fmt.Errorf("invalid memory limit %q: missing numeric value", input)
	}

	// Verify the remaining string is strictly positive integer digits
	if strings.HasPrefix(numStr, "-") {
		return "", 0, fmt.Errorf("invalid memory limit %q: cannot be negative", input)
	}

	for _, r := range numStr {
		if !unicode.IsDigit(r) {
			return "", 0, fmt.Errorf("invalid memory limit %q: invalid numeric characters", input)
		}
	}

	val, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid memory limit %q: %w", input, err)
	}

	if val <= 0 {
		return "", 0, fmt.Errorf("invalid memory limit %q: must be greater than zero", input)
	}

	// Check integer overflow
	if val > math.MaxInt64/multiplier {
		return "", 0, fmt.Errorf("invalid memory limit %q: integer overflow", input)
	}

	bytes := val * multiplier
	return strconv.FormatInt(bytes, 10), bytes, nil
}
