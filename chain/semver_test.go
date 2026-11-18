// Copyright (c) 2023-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"testing"
)

func TestSemverCompatible(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		required semver
		actual   semver
		expected bool
	}{
		// Identical versions are always compatible.
		{
			name:     "identical versions",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 1, Minor: 2, Patch: 3},
			expected: true,
		},
		{
			name:     "identical zero versions",
			required: semver{Major: 0, Minor: 0, Patch: 0},
			actual:   semver{Major: 0, Minor: 0, Patch: 0},
			expected: true,
		},
		{
			name:     "identical large versions",
			required: semver{Major: 450, Minor: 378, Patch: 210},
			actual:   semver{Major: 450, Minor: 378, Patch: 210},
			expected: true,
		},

		// Major versions must match exactly - a newer major is just as
		// incompatible as an older one.
		{
			name:     "major newer by one",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 2, Minor: 2, Patch: 3},
			expected: false,
		},
		{
			name:     "major older by one",
			required: semver{Major: 2, Minor: 2, Patch: 3},
			actual:   semver{Major: 1, Minor: 2, Patch: 3},
			expected: false,
		},

		// With a matching major, a newer minor is compatible regardless of
		// the patch version.
		{
			name:     "minor newer, patch equal",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 1, Minor: 3, Patch: 3},
			expected: true,
		},
		{
			name:     "minor newer, patch older",
			required: semver{Major: 1, Minor: 2, Patch: 9},
			actual:   semver{Major: 1, Minor: 3, Patch: 0},
			expected: true,
		},
		{
			name:     "minor newer, patch newer",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 1, Minor: 3, Patch: 4},
			expected: true,
		},

		// With a matching major, an older minor is never compatible,
		// regardless of the patch version.
		{
			name:     "minor older, patch equal",
			required: semver{Major: 1, Minor: 3, Patch: 3},
			actual:   semver{Major: 1, Minor: 2, Patch: 3},
			expected: false,
		},
		{
			name:     "minor older, patch newer",
			required: semver{Major: 1, Minor: 3, Patch: 0},
			actual:   semver{Major: 1, Minor: 2, Patch: 9},
			expected: false,
		},
		{
			name:     "minor older, patch older",
			required: semver{Major: 1, Minor: 3, Patch: 6},
			actual:   semver{Major: 1, Minor: 2, Patch: 2},
			expected: false,
		},

		// With matching major and minor, the patch must be greater than or
		// equal to the required patch.
		{
			name:     "minor equal, patch newer by one",
			required: semver{Major: 1, Minor: 2, Patch: 3},
			actual:   semver{Major: 1, Minor: 2, Patch: 4},
			expected: true,
		},
		{
			name:     "minor equal, patch older by one",
			required: semver{Major: 1, Minor: 2, Patch: 4},
			actual:   semver{Major: 1, Minor: 2, Patch: 3},
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := semverCompatible(tc.required, tc.actual)
			if result != tc.expected {
				t.Fatalf("got: %v, want: %v", result, tc.expected)
			}
		})
	}
}

func TestSemverToString(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			result := tc.version.String()
			if result != tc.expectedString {
				t.Fatalf("got: %v, want: %v", result, tc.expectedString)
			}
		})
	}
}
