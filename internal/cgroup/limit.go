package cgroup

import (
	"fmt"
	"math"
)

// CalculateCPUQuota calculates the quota string for cpu.max based on a percentage limit.
// A percentage of 100 corresponds to 1 CPU core (quota 100000 per 100000 period).
// Values above 100 (e.g. 200 for 2 cores) are valid. Zero and negative values are rejected.
func CalculateCPUQuota(limitPercentage int) (string, error) {
	if limitPercentage <= 0 {
		return "", fmt.Errorf("invalid CPU limit: %d (must be greater than zero)", limitPercentage)
	}

	if int64(limitPercentage) > math.MaxInt64/1000 {
		return "", fmt.Errorf("CPU limit %d exceeds maximum representable value", limitPercentage)
	}

	quota := int64(limitPercentage) * 1000
	return fmt.Sprintf("%d 100000", quota), nil
}

// ValidatePIDLimit validates that the given process count limit is positive.
func ValidatePIDLimit(limit int) error {
	if limit <= 0 {
		return fmt.Errorf("invalid PID limit: %d (must be greater than zero)", limit)
	}
	return nil
}
