package rollkitadapter

import (
	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtypes "github.com/cometbft/cometbft/types"
	"github.com/rollkit/rollkit/types"
)

func CreateCometBFTPayloadProvider() types.SignaturePayloadProvider {
	return func(header *types.Header, data *types.Data) ([]byte, error) {
		vote := cmtproto.Vote{
			Type:   cmtproto.PrecommitType,
			Height: int64(header.Height()), //nolint:gosec
			Round:  0,
			// Header hash = block hash in rollkit
			BlockID: cmtproto.BlockID{
				Hash:          cmbytes.HexBytes(header.Hash()),
				PartSetHeader: cmtproto.PartSetHeader{},
			},
			Timestamp: header.Time(),
			// proposerAddress = sequencer = validator
			ValidatorAddress: header.ProposerAddress,
			ValidatorIndex:   0,
		}
		chainID := header.ChainID()
		consensusVoteBytes := cmtypes.VoteSignBytes(chainID, &vote)

		return consensusVoteBytes, nil
	}
}
