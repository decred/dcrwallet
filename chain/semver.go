// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import "fmt"

type semver struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Patch uint32 `json:"patch"`
}

func semverCompatible(required, actual semver) bool {
	switch {
	case required.Major != actual.Major:
		return false
	case required.Minor > actual.Minor:
		return false
	case required.Minor == actual.Minor && required.Patch > actual.Patch:
		return false
	default:
		return true
	}
}

func (s semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
}
