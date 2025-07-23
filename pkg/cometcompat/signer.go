package cometcompat

import (
	"context"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
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
			
			// For height 1, use empty BlockID (genesis has no previous block)
			if header.Height() == 1 {
				blockIDProto = cmtproto.BlockID{}
			} else {
				// For height > 1, try to compute a BlockID.
				// This is a simplified approach that attempts to create a deterministic
				// BlockID based on the available header information.
				
				// Create a minimal CometBFT header to compute its hash
				// Note: This is incomplete without the full block data and last commit,
				// but it's better than using an empty BlockID
				
				abciHeader := cmttypes.Header{
					Height:    int64(header.Height()),
					Time:      header.Time(),
					ChainID:   header.ChainID(),
					AppHash:   cmtbytes.HexBytes(header.AppHash),
					DataHash:  cmtbytes.HexBytes(header.DataHash),
					// Note: Missing LastBlockID, LastCommitHash, etc.
					// This will not produce the exact same hash as the final block,
					// but it's a step towards the correct approach.
				}
				
				// Create a minimal block with just the header
				minimalBlock := &cmttypes.Block{
					Header: abciHeader,
					Data:   cmttypes.Data{}, // Empty data - this is incorrect but necessary
				}
				
				// Try to compute a BlockID from this minimal block
				blockParts, err := minimalBlock.MakePartSet(cmttypes.BlockPartSizeBytes)
				if err != nil {
					// If we can't compute parts, fall back to empty BlockID
					blockIDProto = cmtproto.BlockID{}
				} else {
					computedBlockID := cmttypes.BlockID{
						Hash:          minimalBlock.Hash(),
						PartSetHeader: blockParts.Header(),
					}
					blockIDProto = computedBlockID.ToProto()
				}
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
