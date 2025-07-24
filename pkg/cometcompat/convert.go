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

// ToABCIHeader converts rollkit header format defined by ABCI.
func ToABCIHeader(header rlktypes.Header, lastCommit *cmttypes.Commit) (cmttypes.Header, error) {
	if len(header.ProposerAddress) == 0 {
		return cmttypes.Header{}, errors.New("proposer address is not set")
	}

	return cmttypes.Header{
		Version: cmprotoversion.Consensus{
			Block: cmtversion.BlockProtocol,
			App:   header.Version.App,
		},
		Height:             int64(header.Height()), //nolint:gosec
		Time:               header.Time(),
		LastBlockID:        lastCommit.BlockID,
		LastCommitHash:     lastCommit.Hash(),
		DataHash:           cmbytes.HexBytes(header.DataHash),
		ConsensusHash:      cmbytes.HexBytes(header.ConsensusHash),
		AppHash:            cmbytes.HexBytes(header.AppHash),
		LastResultsHash:    cmbytes.HexBytes(header.LastResultsHash),
		EvidenceHash:       new(cmttypes.EvidenceData).Hash(),
		ProposerAddress:    header.ProposerAddress,
		ChainID:            header.ChainID(),
		ValidatorsHash:     cmbytes.HexBytes(header.ValidatorHash),
		NextValidatorsHash: cmbytes.HexBytes(header.ValidatorHash),
	}, nil
}

// ToABCIBlock converts rollit block into block format defined by ABCI.
func ToABCIBlock(header cmttypes.Header, lastCommit *cmttypes.Commit, data *rlktypes.Data) (*cmttypes.Block, error) {
	abciBlock := cmttypes.Block{
		Header: header,
		Evidence: cmttypes.EvidenceData{
			Evidence: nil,
		},
		LastCommit: lastCommit,
	}

	abciBlock.Txs = make([]cmttypes.Tx, len(data.Txs))
	for i := range data.Txs {
		abciBlock.Txs[i] = cmttypes.Tx(data.Txs[i])
	}

	return &abciBlock, nil
}

// ToABCIBlockMeta converts an ABCI block into a BlockMeta format.
func ToABCIBlockMeta(abciBlock *cmttypes.Block) (*cmttypes.BlockMeta, error) {
	blockParts, err := abciBlock.MakePartSet(cmttypes.BlockPartSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("make part set: %w", err)
	}

	return &cmttypes.BlockMeta{
		BlockID: cmttypes.BlockID{
			Hash:          abciBlock.Header.Hash(),
			PartSetHeader: blockParts.Header(),
		},
		BlockSize: abciBlock.Size(),
		Header:    abciBlock.Header,
		NumTxs:    len(abciBlock.Txs),
	}, nil
}
