package cometcompat

import (
	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/rollkit/types"
)

func SignaturePayloadProvider() types.SignaturePayloadProvider {
	return func(header *types.Header) ([]byte, error) {
		vote := cmtproto.Vote{
			Type:   cmtproto.PrecommitType,
			Height: int64(header.Height()), //nolint:gosec
			Round:  0,
			BlockID: cmtproto.BlockID{ // TODO: this isn't the right hash, we need to calculate it from the block data
				Hash: cmbytes.HexBytes(header.Hash()),
				PartSetHeader: cmtproto.PartSetHeader{
					Total: 1,
					Hash:  cmbytes.HexBytes(header.Hash()),
				},
			},
			Timestamp:        header.Time(),
			ValidatorAddress: header.ProposerAddress,
			ValidatorIndex:   0,
		}

		chainID := header.ChainID()
		consensusVoteBytes := cmttypes.VoteSignBytes(chainID, &vote)

		return consensusVoteBytes, nil
	}
}
