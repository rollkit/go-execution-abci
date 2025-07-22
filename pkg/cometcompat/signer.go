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
		
		// Try to get the BlockID from store, but handle cases where it doesn't exist yet
		blockID, err := store.GetBlockID(context.Background(), header.Height())
		if err != nil {
			// For any height where BlockID is not available yet (either genesis or during signing),
			// use empty BlockID. This maintains compatibility with the existing flow.
			blockIDProto = cmtproto.BlockID{}
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
