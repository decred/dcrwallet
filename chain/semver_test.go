package chain

import (
	"testing"
)

func TestSemverCompatible(t *testing.T) {
	testCases := []struct {
		name     string
		required semver
		actual   semver
		expected bool
	}{
		{
			name:     "Equal Versions",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 1, Minor: 2, Patch: 3},
			expected: true,
		},
		{
			name:     "Different versions",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 2, Minor: 0, Patch: 0},
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := semverCompatible(tc.required, tc.actual)
			if result != tc.expected {
				t.Errorf("Expected: %v, got: %v", tc.expected, result)
			}
		})
	}
	t.Log("Test Passed")
}

func TestSemverToString(t *testing.T) {
	testCases := []struct {
		name           string
		version        semver
		expectedString string
	}{
		{
			name:           "version Representation Case1",
			version:        semver{Major: 1, Minor: 2, Patch: 3},
			expectedString: "1.2.3",
		},
		{
			name:           "version Representation Case2",
			version:        semver{Major: 2, Minor: 0, Patch: 1},
			expectedString: "2.0.1",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.version.String()
			if result != tc.expectedString {
				t.Errorf("Expected: %s, got: %s", tc.expectedString, result)
			}
		})
	}
	t.Log("Test Passed")
}
