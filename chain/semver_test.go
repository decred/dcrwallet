package chain

import (
	"testing"
)

// Testing a variety of version schemes for the serverCompatibe()
func TestSemverCompatible(t *testing.T) {
	testCases := []struct {
		name     string
		required semver
		actual   semver
		expected bool
	}{
		{
			name:     "Equal Versions Case1",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 1, Minor: 2, Patch: 3},
			expected: true,
		},
		{
			name:     "Equal Versions Case2",
			required: semver{Major: 450, Minor: 378, Patch: 210},
			actual:   semver{Major: 450, Minor: 378, Patch: 210},
			expected: true,
		},
		{
			name:     "Equal Versions Case3",
			required: semver{Major: 78, Minor: 94, Patch: 80},
			actual:   semver{Major: 78, Minor: 94, Patch: 80},
			expected: true,
		},
		{
			name:     "Different versions Case1",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 2, Minor: 0, Patch: 0},
			expected: false,
		},
		{
			name:     "Different versions Case2",
			required: semver{Major: 1, Minor: 2, Patch: 4},
			actual:   semver{Major: 1, Minor: 0, Patch: 2},
			expected: false,
		},
		{
			name:     "Different versions Case3",
			required: semver{Major: 1, Minor: 3, Patch: 6},
			actual:   semver{Major: 1, Minor: 3, Patch: 2},
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := semverCompatible(tc.required, tc.actual)
			if result != tc.expected {
				t.Fatalf("got: %v, want: %v", result, tc.expected)
			}
		})
	}
}

// Testing a variety of version schemes to print the right version output
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
				t.Fatalf("got: %v, want: %v", result, tc.expectedString)
			}
		})
	}
}
