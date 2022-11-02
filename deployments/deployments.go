// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package deployments

import (
	"context"

	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/rpc/client/dcrd"
	"github.com/decred/dcrd/chaincfg/v3"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
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
	TestNet2ActivationHeight int32
	TestNet3ActivationHeight int32
	SimNetActivationHeight   int32
}

// DCP0001 specifies hard forking changes to the stake difficulty algorithm as
// defined by https://github.com/decred/dcps/blob/master/dcp-0001/dcp-0001.mediawiki.
var DCP0001 = HardcodedDeployment{
	MainNetActivationHeight:  149248,
	TestNet2ActivationHeight: 46128,
	TestNet3ActivationHeight: 0,
	SimNetActivationHeight:   0,
}

// DCP0002 specifies the activation of the OP_SHA256 hard fork as defined by
// https://github.com/decred/dcps/blob/master/dcp-0002/dcp-0002.mediawiki.
var DCP0002 = HardcodedDeployment{
	MainNetActivationHeight:  189568,
	TestNet2ActivationHeight: 151968,
	TestNet3ActivationHeight: 0,
	SimNetActivationHeight:   0,
}

// DCP0003 specifies the activation of a CSV soft fork as defined by
// https://github.com/decred/dcps/blob/master/dcp-0003/dcp-0003.mediawiki.
var DCP0003 = HardcodedDeployment{
	MainNetActivationHeight:  189568,
	TestNet2ActivationHeight: 151968,
	TestNet3ActivationHeight: 0,
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
	case 0x48e7a065: // testnet2
		activationHeight = d.TestNet2ActivationHeight
	case wire.TestNet3:
		activationHeight = d.TestNet3ActivationHeight
	case wire.SimNet:
		activationHeight = d.SimNetActivationHeight
	}
	return activationHeight >= 0 && height >= activationHeight
}

const (
	lockedinStatus = "lockedin"
	activeStatus   = "active"
)

// DCP0010Active returns whether the consensus rules for the next block with the
// current chain tip height requires the subsidy split as specified in DCP0010.
// DCP0010 is always active on simnet, and requires the RPC syncer to detect
// activation on mainnet and testnet3.
func DCP0010Active(ctx context.Context, height int32, params *chaincfg.Params,
	syncer interface{}) (bool, error) {

	net := params.Net
	rcai := int32(params.RuleChangeActivationInterval)

	if net == wire.SimNet {
		return true, nil
	}
	if net != wire.MainNet && net != wire.TestNet3 {
		return false, nil
	}
	rpc, ok := syncer.(*dcrd.RPC)
	if !ok {
		return false, errors.E(errors.Bug, "DCP0010 activation check requires RPC syncer")
	}
	var resp dcrdtypes.GetBlockChainInfoResult
	err := rpc.Call(ctx, "getblockchaininfo", &resp)
	if err != nil {
		return false, err
	}
	d, ok := resp.Deployments[chaincfg.VoteIDChangeSubsidySplit]
	if !ok {
		return false, nil
	}
	switch {
	case d.Status == lockedinStatus && height == int32(d.Since)+rcai-1:
		return true, nil
	case d.Status == activeStatus:
		return true, nil
	default:
		return false, nil
	}
}
