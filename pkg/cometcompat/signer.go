package cometcompat

import (
	"context"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/rollkit/types"

	abciexecstore "github.com/rollkit/go-execution-abci/pkg/store"
)

func SignaturePayloadProvider(store *abciexecstore.Store) types.SignaturePayloadProvider {
	return func(header *types.Header) ([]byte, error) {
		var blockIDProto cmtproto.BlockID
		
		// Try to get the BlockID from store first
		blockID, err := store.GetBlockID(context.Background(), header.Height())
		if err != nil {
			// BlockID is not available in storage yet (normal during block construction).
			// We need to compute the correct BlockID that corresponds to this header.
			
			// For height 1, there's no previous commit, so we use empty BlockID
			if header.Height() == 1 {
				blockIDProto = cmtproto.BlockID{}
			} else {
				// For height > 1, we need to compute the BlockID.
				// This requires simulating what getBlockMeta does.
				
				// First, get the data for this block (if available)
				// Note: In a real scenario, this might not be available yet
				
				// Get the last commit (for the previous block)
				// We can try to reconstruct this from the store
				
				// For now, let's see if we can get enough information to compute the BlockID
				// If not, we'll use empty BlockID as a fallback
				
				// TODO: Implement proper BlockID computation
				// This is a complex problem that might require architectural changes
				blockIDProto = cmtproto.BlockID{}
			}
		} else {
			blockIDProto = blockID.ToProto()
		}

		vote := cmtproto.Vote{
			Type:             cmtproto.PrecommitType,
			Height:           int64(header.Height()), //nolint:gosec
			BlockID:          blockIDProto,
			Round:            0,
			Timestamp:        header.Time(),
			ValidatorAddress: header.ProposerAddress,
			ValidatorIndex:   0,
		}

		chainID := header.ChainID()
		consensusVoteBytes := cmttypes.VoteSignBytes(chainID, &vote)

		return consensusVoteBytes, nil
	}
}
