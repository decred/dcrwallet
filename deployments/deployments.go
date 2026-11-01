// Copyright (c) 2018-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package deployments

import (
	"context"

	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/wire"
)

// HardcodedDeployment specifies hardcoded block heights that a deployment
// activates at.  If the value is negative, the deployment is either inactive or
// can't be determined due to the uniqueness properties of the network.
//
// Since these are hardcoded deployments, and cannot support every possible
// network, conditional logic should only be applied when a deployment is
// active, not when it is inactive.
type HardcodedDeployment struct {
	MainNetActivationHeight  int32
	TestNet3ActivationHeight int32
	SimNetActivationHeight   int32
}

// DCP0001 specifies hard forking changes to the stake difficulty algorithm as
// defined by https://github.com/decred/dcps/blob/master/dcp-0001/dcp-0001.mediawiki.
var DCP0001 = HardcodedDeployment{
	MainNetActivationHeight:  149248,
	TestNet3ActivationHeight: 0,
	SimNetActivationHeight:   0,
}

// DCP0002 specifies the activation of the OP_SHA256 hard fork as defined by
// https://github.com/decred/dcps/blob/master/dcp-0002/dcp-0002.mediawiki.
var DCP0002 = HardcodedDeployment{
	MainNetActivationHeight:  189568,
	TestNet3ActivationHeight: 0,
	SimNetActivationHeight:   0,
}

// DCP0003 specifies the activation of a CSV soft fork as defined by
// https://github.com/decred/dcps/blob/master/dcp-0003/dcp-0003.mediawiki.
var DCP0003 = HardcodedDeployment{
	MainNetActivationHeight:  189568,
	TestNet3ActivationHeight: 0,
	SimNetActivationHeight:   0,
}

// DCP0005 specifies the activation of a block header commitments soft fork as
// defined by https://github.com/decred/dcps/blob/master/dcp-0005/dcp-0005.mediawiki.
var DCP0005 = HardcodedDeployment{
	MainNetActivationHeight:  431488,
	TestNet3ActivationHeight: 323328,
	SimNetActivationHeight:   0,
}

// DCP0010 specifies the activation of a subsidy split change (10% PoW, 80% PoS,
// and 10% Treasury) soft fork as defined by
// https://github.com/decred/dcps/blob/master/dcp-0010/dcp-0010.mediawiki.
var DCP0010 = HardcodedDeployment{
	MainNetActivationHeight:  657280,
	TestNet3ActivationHeight: 877728,
	SimNetActivationHeight:   0,
}

// DCP0011 specifies the activation of a PoW change to BLAKE3 soft fork as
// defined by
// https://github.com/decred/dcps/blob/master/dcp-0011/dcp-0011.mediawiki.
var DCP0011 = HardcodedDeployment{
	MainNetActivationHeight:  794368,
	TestNet3ActivationHeight: 1170048,
	SimNetActivationHeight:   0,
}

// DCP0012 specifies the activation of a subsidy split change (1% PoW, 89% PoS,
// and 10% Treasury) soft fork as defined by
// https://github.com/decred/dcps/blob/master/dcp-0012/dcp-0012.mediawiki.
var DCP0012 = HardcodedDeployment{
	MainNetActivationHeight:  794368,
	TestNet3ActivationHeight: 1170048,
	SimNetActivationHeight:   0,
}

// Active returns whether the hardcoded deployment is active at height on the
// network specified by params.  Active always returns false for unrecognized
// networks.
func (d *HardcodedDeployment) Active(height int32, net wire.CurrencyNet) bool {
	var activationHeight int32 = -1
	switch net {
	case wire.MainNet:
		activationHeight = d.MainNetActivationHeight
	case wire.TestNet3:
		activationHeight = d.TestNet3ActivationHeight
	case wire.SimNet:
		activationHeight = d.SimNetActivationHeight
	}
	return activationHeight >= 0 && height >= activationHeight
}

// Querier defines the interface for a chain backend that can (trustfully)
// query for agenda deployment information.
type Querier interface {
	// Deployments should return information about existing agendas,
	// including their deployment status.
	Deployments(context.Context) (map[string]dcrdtypes.AgendaInfo, error)
}
