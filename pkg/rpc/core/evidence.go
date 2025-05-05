package core

import (
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// BroadcastEvidence implements client.Client but is essentially a no-op in this context,
// as Rollkit doesn't handle evidence in the same way as Tendermint.
// It returns a successful response with the evidence hash, mimicking Tendermint's behaviour
// without actually processing or storing the evidence.
func BroadcastEvidence(ctx *rpctypes.Context, ev cmttypes.Evidence) (*ctypes.ResultBroadcastEvidence, error) {
	// Log that evidence broadcasting is not supported or is a no-op?
	env.Logger.Debug("BroadcastEvidence called, but evidence handling is not implemented in Rollkit RPC.")
	return &ctypes.ResultBroadcastEvidence{
		Hash: ev.Hash(),
	}, nil
}
