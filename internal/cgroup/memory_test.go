package cgroup

import (
	"testing"
)

func TestParseMemoryLimit(t *testing.T) {
	testCases := []struct {
		name          string
		input         string
		expectedValue string
		expectedBytes int64
		expectError   bool
	}{
		// Valid cases
		{"OmittedEmpty", "", "", 0, false},
		{"OmittedWhitespace", "   ", "", 0, false},
		{"MaxLower", "max", "max", -1, false},
		{"MaxUpper", "MAX", "max", -1, false},
		{"MaxMixed", "Max", "max", -1, false},
		{"RawBytes", "1048576", "1048576", 1048576, false},
		{"KilobytesUpper", "500K", "512000", 512000, false},
		{"KilobytesLower", "500k", "512000", 512000, false},
		{"MegabytesUpper", "100M", "104857600", 104857600, false},
		{"MegabytesLower", "100m", "104857600", 104857600, false},
		{"GigabytesUpper", "1G", "1073741824", 1073741824, false},
		{"GigabytesLower", "1g", "1073741824", 1073741824, false},
		{"SmallestValid", "1", "1", 1, false},

		// Invalid cases - zero & negative
		{"ZeroRaw", "0", "", 0, true},
		{"ZeroMegabytes", "0M", "", 0, true},
		{"ZeroKilobytes", "0k", "", 0, true},
		{"ZeroGigabytes", "0G", "", 0, true},
		{"NegativeRaw", "-10", "", 0, true},
		{"NegativeMegabytes", "-100M", "", 0, true},
		{"NegativeKilobytes", "-50k", "", 0, true},

		// Invalid cases - syntax & suffixes
		{"InvalidSuffixMB", "100MB", "", 0, true},
		{"InvalidSuffixMiB", "100MiB", "", 0, true},
		{"InvalidSuffixKB", "100kb", "", 0, true},
		{"InvalidSuffixB", "100B", "", 0, true},
		{"DecimalNotAllowed", "1.5G", "", 0, true},
		{"WhitespaceInside", "100 M", "", 0, true},
		{"LettersOnly", "abc", "", 0, true},
		{"UnitOnlyM", "M", "", 0, true},
		{"UnitOnlyK", "K", "", 0, true},
		{"UnitOnlyG", "G", "", 0, true},

		// Invalid cases - overflow
		{"OverflowNumber", "9223372036854775808", "", 0, true},
		{"OverflowMultiplierG", "9223372036854775807G", "", 0, true},
		{"OverflowMultiplierM", "9223372036854775807M", "", 0, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val, bytes, err := ParseMemoryLimit(tc.input)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for input %q, got value=%q bytes=%d", tc.input, val, bytes)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tc.input, err)
			}
			if val != tc.expectedValue {
				t.Errorf("expected value %q, got %q", tc.expectedValue, val)
			}
			if bytes != tc.expectedBytes {
				t.Errorf("expected bytes %d, got %d", tc.expectedBytes, bytes)
			}
		})
	}
}
