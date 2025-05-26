package rollkitadapter

import (
	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	cmtypes "github.com/cometbft/cometbft/types"
	"github.com/rollkit/rollkit/types"
)

var (
	// EmptyEvidenceHash is the hash of an empty EvidenceData
	EmptyEvidenceHash = new(cmtypes.EvidenceData).Hash()
)

func CreateCometBFTHeaderHasher() types.HeaderHasher {
	return func(header *types.Header) (types.Hash, error) {
		abciHeader := cmtypes.Header{
			Version: cmversion.Consensus{
				Block: header.Version.Block,
				App:   header.Version.App,
			},
			Height: int64(header.Height()), //nolint:gosec
			Time:   header.Time(),
			LastBlockID: cmtypes.BlockID{
				Hash: cmbytes.HexBytes(header.LastHeaderHash),
				PartSetHeader: cmtypes.PartSetHeader{
					Total: 0,
					Hash:  nil,
				},
			},
			LastCommitHash:  cmbytes.HexBytes(header.LastCommitHash),
			DataHash:        cmbytes.HexBytes(header.DataHash),
			ConsensusHash:   cmbytes.HexBytes(header.ConsensusHash),
			AppHash:         cmbytes.HexBytes(header.AppHash),
			LastResultsHash: cmbytes.HexBytes(header.LastResultsHash),
			EvidenceHash:    EmptyEvidenceHash,
			ProposerAddress: header.ProposerAddress,
			// Backward compatibility
			ValidatorsHash:     cmbytes.HexBytes(header.ValidatorHash),
			NextValidatorsHash: cmbytes.HexBytes(header.ValidatorHash),
			ChainID:            header.ChainID(),
		}
		return types.Hash(abciHeader.Hash()), nil
	}
}
