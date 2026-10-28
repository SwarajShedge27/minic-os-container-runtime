package cgroup

import (
	"math"
	"testing"
)

func TestCalculateCPUQuota(t *testing.T) {
	testCases := []struct {
		name        string
		percentage  int
		expected    string
		expectError bool
	}{
		{"FiftyPercent", 50, "50000 100000", false},
		{"OneCore", 100, "100000 100000", false},
		{"OneAndHalfCores", 150, "150000 100000", false},
		{"TwoCores", 200, "200000 100000", false},
		{"FourCores", 400, "400000 100000", false},
		{"ZeroPercent", 0, "", true},
		{"NegativePercent", -25, "", true},
		{"OverflowPercent", math.MaxInt, "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			quota, err := CalculateCPUQuota(tc.percentage)
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error for percentage %d, got quota %q", tc.percentage, quota)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if quota != tc.expected {
				t.Errorf("expected quota %q, got %q", tc.expected, quota)
			}
		})
	}
}

func TestValidatePIDLimit(t *testing.T) {
	testCases := []struct {
		name        string
		limit       int
		expectError bool
	}{
		{"ValidSmall", 1, false},
		{"ValidNormal", 50, false},
		{"ValidLarge", 10000, false},
		{"Zero", 0, true},
		{"Negative", -10, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePIDLimit(tc.limit)
			if tc.expectError && err == nil {
				t.Errorf("expected error for limit %d, got nil", tc.limit)
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error for limit %d: %v", tc.limit, err)
			}
		})
	}
}
