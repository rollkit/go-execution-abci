package core

import (
	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
)

// Status returns CometBFT status including node info, pubkey, latest block
// hash, app hash, block height and time.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/status
func Status(ctx *rpctypes.Context) (*ctypes.ResultStatus, error) {
	info, err := env.Adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}

	s, err := env.Adapter.Store.LoadState(ctx.Context())
	if err != nil {
		return nil, err
	}

	// TODO: Populate NodeInfo properly
	nodeInfo := p2p.DefaultNodeInfo{
		// ProtocolVersion: p2p.NewProtocolVersion(
		// 	// Assuming these are accessible or definable constants/config values
		// 	version.P2PProtocol, // Assuming 'version' package or similar imported
		// 	version.BlockProtocol,
		// 	version.AppProtocol,
		// ),
		// DefaultNodeID: // Needs node key -> p.nodeKey.ID() ? Requires passing nodeKey to RpcProvider
		// ListenAddr: // Needs listener address -> listener.Addr().String() ? Requires access to listener
		// Network:    // Needs network/chain ID -> p.config.ChainID ? Requires access to config
		// Version:    // Needs application version -> version.TMCoreSemVer ?
		// Channels:   // Needs channel info -> p.channels // Requires access to p2p channels
		// Moniker:    // Needs moniker -> p.config.Moniker ?
		// Other fields like Moniker, Version might be available too
	}
	// Check if the node key info is readily available in adapter or needs to be passed separately.
	// If p.adapter.P2PClient is accessible and has NodeInfo:
	// nodeKey := p.adapter.P2PClient.NodeInfo() // Hypothetical method
	// if nodeKey != nil {
	//    nodeInfo = *nodeKey
	// }

	return &ctypes.ResultStatus{
		NodeInfo: nodeInfo, // Use the populated or default NodeInfo
		SyncInfo: ctypes.SyncInfo{
			// LatestBlockHash:   // Need block meta -> latestBlockMeta.BlockID.Hash
			LatestAppHash:     cmtbytes.HexBytes(info.LastBlockAppHash),
			LatestBlockHeight: info.LastBlockHeight,
			LatestBlockTime:   s.LastBlockTime,
			// CatchingUp: // Requires sync status logic
		},
		ValidatorInfo: ctypes.ValidatorInfo{ // Assumes single validator/sequencer model
			Address:     s.Validators.Proposer.Address,
			PubKey:      s.Validators.Proposer.PubKey,
			VotingPower: s.Validators.Proposer.VotingPower,
		},
	}, nil
}
