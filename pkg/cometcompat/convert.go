package cometcompat

import (
	"errors"
	"fmt"

	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmprotoversion "github.com/cometbft/cometbft/proto/tendermint/version"
	cmttypes "github.com/cometbft/cometbft/types"
	cmtversion "github.com/cometbft/cometbft/version"

	rlktypes "github.com/rollkit/rollkit/types"
)

// ToABCIBlock converts Rolkit block into block format defined by ABCI.
func ToABCIBlock(header *rlktypes.SignedHeader, data *rlktypes.Data, lastCommit *cmttypes.Commit) (*cmttypes.Block, error) {
	abciHeader, err := ToABCIHeader(&header.Header)
	if err != nil {
		return nil, err
	}

	// validate have one validator
	if len(header.ProposerAddress) == 0 {
		return nil, errors.New("proposer address is not set")
	}

	// set commit hash
	abciHeader.LastCommitHash = lastCommit.Hash()

	// set validator hash
	if header.Signer.Address != nil {
		validatorHash, err := validatorHasher(header.ProposerAddress, header.Signer.PubKey)
		if err != nil {
			return nil, fmt.Errorf("failed to compute validator hash: %w", err)
		}
		abciHeader.ValidatorsHash = cmbytes.HexBytes(validatorHash)
		abciHeader.NextValidatorsHash = cmbytes.HexBytes(validatorHash)
	}

	abciBlock := cmttypes.Block{
		Header: abciHeader,
		Evidence: cmttypes.EvidenceData{
			Evidence: nil,
		},
		LastCommit: lastCommit,
	}
	abciBlock.Txs = make([]cmttypes.Tx, len(data.Txs))
	for i := range data.Txs {
		abciBlock.Txs[i] = cmttypes.Tx(data.Txs[i])
	}
	abciBlock.DataHash = cmbytes.HexBytes(header.DataHash)

	return &abciBlock, nil
}

// ToABCIBlockMeta converts Rollkit block into BlockMeta format defined by ABCI
func ToABCIBlockMeta(header *rlktypes.SignedHeader, data *rlktypes.Data, lastCommit *cmttypes.Commit) (*cmttypes.BlockMeta, error) {
	cmblock, err := ToABCIBlock(header, data, lastCommit)
	if err != nil {
		return nil, err
	}
	blockID := cmttypes.BlockID{Hash: cmblock.Hash()}

	return &cmttypes.BlockMeta{
		BlockID:   blockID,
		BlockSize: cmblock.Size(),
		Header:    cmblock.Header,
		NumTxs:    len(cmblock.Txs),
	}, nil
}

// ToABCIHeader converts Rollkit header to Header format defined in ABCI.
func ToABCIHeader(header *rlktypes.Header) (cmttypes.Header, error) {
	return cmttypes.Header{
		Version: cmprotoversion.Consensus{
			Block: cmtversion.BlockProtocol,
			App:   header.Version.App,
		},
		Height: int64(header.Height()), //nolint:gosec
		Time:   header.Time(),
		LastBlockID: cmttypes.BlockID{
			Hash: cmbytes.HexBytes(header.LastHeaderHash),
			PartSetHeader: cmttypes.PartSetHeader{
				Total: 0,
				Hash:  nil,
			},
		},
		LastCommitHash:  cmbytes.HexBytes(header.LastCommitHash),
		DataHash:        cmbytes.HexBytes(header.DataHash),
		ConsensusHash:   cmbytes.HexBytes(header.ConsensusHash),
		AppHash:         cmbytes.HexBytes(header.AppHash),
		LastResultsHash: cmbytes.HexBytes(header.LastResultsHash),
		EvidenceHash:    new(cmttypes.EvidenceData).Hash(),
		ProposerAddress: header.ProposerAddress,
		ChainID:         header.ChainID(),
		// validator hash and next validator hash are not set here
		// they are set later (in ToABCIBlock)
	}, nil
}
