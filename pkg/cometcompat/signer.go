package cometcompat

import (
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/rollkit/types"
)

func SignaturePayloadProvider() types.SignaturePayloadProvider {
	return func(header *types.Header, data *types.Data) ([]byte, error) {
		vote := cmtproto.Vote{
			Type:   cmtproto.PrecommitType,
			Height: int64(header.Height()), //nolint:gosec
			Round:  0,
			BlockID: cmtproto.BlockID{
				Hash: cmtbytes.HexBytes(data.Hash()),
				PartSetHeader: cmtproto.PartSetHeader{
					Total: 1,
					Hash:  cmtbytes.HexBytes(data.Hash()),
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
