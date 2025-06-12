package cometcompat

import (
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/rollkit/types"
)

func PayloadProvider() types.SignaturePayloadProvider {
	return func(header *types.Header) ([]byte, error) {
		abciHeaderForSigning, err := ToABCIHeader(header)
		if err != nil {
			return nil, err
		}
		vote := cmtproto.Vote{
			Type:   cmtproto.PrecommitType,
			Height: int64(header.Height()), //nolint:gosec
			Round:  0,
			BlockID: cmtproto.BlockID{
				Hash:          abciHeaderForSigning.Hash(),
				PartSetHeader: cmtproto.PartSetHeader{},
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
