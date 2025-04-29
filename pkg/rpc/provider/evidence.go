package provider

import (
	"context"

	// Check if logger is used
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// BroadcastEvidence implements client.Client but is essentially a no-op in this context,
// as Rollkit doesn't handle evidence in the same way as Tendermint.
// It returns a successful response with the evidence hash, mimicking Tendermint's behaviour
// without actually processing or storing the evidence.
func (p *RpcProvider) BroadcastEvidence(_ context.Context, evidence cmttypes.Evidence) (*coretypes.ResultBroadcastEvidence, error) {
	// Log that evidence broadcasting is not supported or is a no-op?
	p.logger.Debug("BroadcastEvidence called, but evidence handling is not implemented in Rollkit RPC.")
	return &coretypes.ResultBroadcastEvidence{
		Hash: evidence.Hash(),
	}, nil
}
