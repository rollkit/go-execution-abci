package provider

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types" // Needed for Status
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p" // Used by ABCIQueryWithOptions indirectly via Status? No, remove if unused.
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// Status implements client.CometRPC.
func (p *RpcProvider) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	info, err := p.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}

	s, err := p.adapter.Store.LoadState(ctx)
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

	return &coretypes.ResultStatus{
		NodeInfo: nodeInfo, // Use the populated or default NodeInfo
		SyncInfo: coretypes.SyncInfo{
			// LatestBlockHash:   // Need block meta -> latestBlockMeta.BlockID.Hash
			LatestAppHash:     cmtbytes.HexBytes(info.LastBlockAppHash),
			LatestBlockHeight: info.LastBlockHeight,
			LatestBlockTime:   s.LastBlockTime,
			// CatchingUp: // Requires sync status logic
		},
		ValidatorInfo: coretypes.ValidatorInfo{ // Assumes single validator/sequencer model
			Address:     s.Validators.Proposer.Address,
			PubKey:      s.Validators.Proposer.PubKey,
			VotingPower: s.Validators.Proposer.VotingPower,
		},
	}, nil
}

// NetInfo implements client.Client.
func (p *RpcProvider) NetInfo(context.Context) (*coretypes.ResultNetInfo, error) {
	res := coretypes.ResultNetInfo{
		Listening: true,       // Assuming node is always listening if RPC is up
		Listeners: []string{}, // TODO: Populate with actual listener addresses if available
	}

	// Access P2P client from adapter to get peer info
	if p.adapter.P2PClient != nil {
		for _, ma := range p.adapter.P2PClient.Addrs() {
			res.Listeners = append(res.Listeners, ma.String())
		}
		peers := p.adapter.P2PClient.Peers()
		res.NPeers = len(peers)
		for _, peer := range peers {
			// Convert peer info to coretypes.Peer
			// Ensure p2p.DefaultNodeInfo is correctly populated from peer.NodeInfo
			res.Peers = append(res.Peers, coretypes.Peer{
				NodeInfo: p2p.DefaultNodeInfo{ // Adapt this based on actual available PeerInfo structure
					// Access fields via peer.NodeInfo
					DefaultNodeID: p2p.ID(peer.NodeInfo.NodeID),
					ListenAddr:    peer.NodeInfo.ListenAddr,
					Network:       peer.NodeInfo.Network,
					// Other fields like Moniker, Version might be available too
				},
				IsOutbound: peer.IsOutbound,
				RemoteIP:   peer.RemoteIP,
			})
		}
	} else {
		// Handle case where P2P client is not available or initialized
		res.NPeers = 0
		res.Peers = []coretypes.Peer{}
	}

	return &res, nil
}

// Health implements client.Client.
func (p *RpcProvider) Health(context.Context) (*coretypes.ResultHealth, error) {
	// Basic health check, always returns OK for now.
	// Could be extended to check DB connection, P2P status, etc.
	return &coretypes.ResultHealth{}, nil
}

// ConsensusParams implements client.Client.
func (p *RpcProvider) ConsensusParams(ctx context.Context, height *int64) (*coretypes.ResultConsensusParams, error) {
	state, err := p.adapter.Store.LoadState(ctx)
	if err != nil {
		return nil, err
	}
	params := state.ConsensusParams
	// Use normalizeHeight which should be moved to rpcProvider as well
	normalizedHeight := p.normalizeHeight(height) // Changed r.normalizeHeight to p.normalizeHeight
	return &coretypes.ResultConsensusParams{
		BlockHeight: int64(normalizedHeight), //nolint:gosec
		ConsensusParams: cmttypes.ConsensusParams{
			Block: cmttypes.BlockParams{
				MaxBytes: params.Block.MaxBytes,
				MaxGas:   params.Block.MaxGas,
			},
			Evidence: cmttypes.EvidenceParams{
				MaxAgeNumBlocks: params.Evidence.MaxAgeNumBlocks,
				MaxAgeDuration:  params.Evidence.MaxAgeDuration,
				MaxBytes:        params.Evidence.MaxBytes,
			},
			Validator: cmttypes.ValidatorParams{
				PubKeyTypes: params.Validator.PubKeyTypes,
			},
			Version: cmttypes.VersionParams{
				App: params.Version.App,
			},
		},
	}, nil
}

// ConsensusState implements client.Client.
func (p *RpcProvider) ConsensusState(context.Context) (*coretypes.ResultConsensusState, error) {
	// Rollkit doesn't have Tendermint consensus state.
	return nil, ErrConsensusStateNotAvailable
}

// DumpConsensusState implements client.Client.
func (p *RpcProvider) DumpConsensusState(context.Context) (*coretypes.ResultDumpConsensusState, error) {
	// Rollkit doesn't have Tendermint consensus state.
	return nil, ErrConsensusStateNotAvailable
}
