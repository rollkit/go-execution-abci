package core

import (
	"errors"

	"github.com/cometbft/cometbft/p2p"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
)

func NetInfo(ctx *rpctypes.Context) (*coretypes.ResultNetInfo, error) {
	res := coretypes.ResultNetInfo{
		Listening: true,       // Assuming node is always listening if RPC is up
		Listeners: []string{}, // TODO: Populate with actual listener addresses if available
	}

	// Access P2P client from adapter to get peer info
	if env.Adapter.P2PClient != nil {
		for _, ma := range env.Adapter.P2PClient.Addrs() {
			res.Listeners = append(res.Listeners, ma.String())
		}
		peers := env.Adapter.P2PClient.Peers()
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

func Genesis(ctx *rpctypes.Context) (*coretypes.ResultGenesis, error) {
	// Returning unimplemented as per the original code.
	// Consider implementing or returning a more specific error if needed.
	panic("unimplemented")
}

func GenesisChunked(ctx *rpctypes.Context, chunk uint) (*coretypes.ResultGenesisChunk, error) {
	return nil, errors.New("GenesisChunked RPC method is not yet implemented")
}
