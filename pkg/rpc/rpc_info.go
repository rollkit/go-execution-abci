package rpc

import (
	"context"
	"errors"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// ABCIInfo implements client.CometRPC.
func (r *RPCServer) ABCIInfo(context.Context) (*coretypes.ResultABCIInfo, error) {
	info, err := r.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIInfo{
		Response: *info,
	}, nil
}

// ABCIQuery implements client.CometRPC.
func (r *RPCServer) ABCIQuery(ctx context.Context, path string, data cmtbytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	resp, err := r.adapter.App.Query(ctx, &abci.RequestQuery{
		Data: data,
		Path: path,
	})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{
		Response: *resp,
	}, nil
}

// ABCIQueryWithOptions implements client.CometRPC.
func (r *RPCServer) ABCIQueryWithOptions(ctx context.Context, path string, data cmtbytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	resp, err := r.adapter.App.Query(ctx, &abci.RequestQuery{
		Data:   data,
		Path:   path,
		Height: opts.Height,
		Prove:  opts.Prove,
	})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{
		Response: *resp,
	}, nil
}

// Status implements client.CometRPC.
func (r *RPCServer) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	info, err := r.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}

	s, err := r.adapter.LoadState(ctx)
	if err != nil {
		return nil, err
	}

	return &coretypes.ResultStatus{
		NodeInfo: p2p.DefaultNodeInfo{}, // TODO: fill this in
		SyncInfo: coretypes.SyncInfo{
			// LatestBlockHash:   cmtbytes.HexBytes(info.LastBlockAppHash), // TODO: fill this in  latestBlockMeta.BlockID.Hash
			LatestAppHash:     cmtbytes.HexBytes(info.LastBlockAppHash),
			LatestBlockHeight: info.LastBlockHeight,
			LatestBlockTime:   s.LastBlockTime,
		},
		ValidatorInfo: coretypes.ValidatorInfo{
			Address:     s.Validators.Proposer.Address,
			PubKey:      s.Validators.Proposer.PubKey,
			VotingPower: s.Validators.Proposer.VotingPower,
		},
	}, nil
}

// NetInfo implements client.Client.
func (r *RPCServer) NetInfo(context.Context) (*coretypes.ResultNetInfo, error) {
	res := coretypes.ResultNetInfo{
		Listening: true,
	}
	for _, ma := range r.adapter.P2PClient.Addrs() {
		res.Listeners = append(res.Listeners, ma.String())
	}
	peers := r.adapter.P2PClient.Peers()
	res.NPeers = len(peers)
	for _, peer := range peers {
		res.Peers = append(res.Peers, coretypes.Peer{
			NodeInfo: p2p.DefaultNodeInfo{
				DefaultNodeID: p2p.ID(peer.NodeInfo.NodeID),
				ListenAddr:    peer.NodeInfo.ListenAddr,
				Network:       peer.NodeInfo.Network,
			},
			IsOutbound: peer.IsOutbound,
			RemoteIP:   peer.RemoteIP,
		})
	}

	return &res, nil
}

// Health implements client.Client.
func (r *RPCServer) Health(context.Context) (*coretypes.ResultHealth, error) {
	return &coretypes.ResultHealth{}, nil
}

// ConsensusParams implements client.Client.
func (r *RPCServer) ConsensusParams(ctx context.Context, height *int64) (*coretypes.ResultConsensusParams, error) {
	state, err := r.adapter.LoadState(ctx)
	if err != nil {
		return nil, err
	}
	params := state.ConsensusParams
	// Use normalizeHeight which is now defined in utils.go (part of the same package)
	normalizedHeight := r.normalizeHeight(height)
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
func (r *RPCServer) ConsensusState(context.Context) (*coretypes.ResultConsensusState, error) {
	return nil, ErrConsensusStateNotAvailable
}

// DumpConsensusState implements client.Client.
func (r *RPCServer) DumpConsensusState(context.Context) (*coretypes.ResultDumpConsensusState, error) {
	return nil, ErrConsensusStateNotAvailable
}

// Genesis implements client.Client.
func (r *RPCServer) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	// Returning unimplemented as per the original code.
	// Consider implementing or returning a more specific error if needed.
	panic("unimplemented")
}

// GenesisChunked implements client.Client.
func (r *RPCServer) GenesisChunked(context.Context, uint) (*coretypes.ResultGenesisChunk, error) {
	return nil, errors.New("GenesisChunked RPC method is not yet implemented")
}
