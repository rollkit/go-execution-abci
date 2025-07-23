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
		blockID, err := store.GetBlockID(context.Background(), header.Height())
		if err != nil && header.Height() > 1 {
			return nil, err
		}

		vote := cmtproto.Vote{
			Type:             cmtproto.PrecommitType,
			Height:           int64(header.Height()), //nolint:gosec
			BlockID:          blockID.ToProto(),
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
