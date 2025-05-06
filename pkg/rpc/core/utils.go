package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	cmtypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/rollkit/types"
)

// ToABCIHeader converts Rollkit header to Header format defined in ABCI.
// Caller should fill all the fields that are not available in Rollkit header (like ChainID).
func ToABCIHeader(header *types.Header) (cmtypes.Header, error) {
	return cmtypes.Header{
		Version: cmversion.Consensus{
			Block: header.Version.Block,
			App:   header.Version.App,
		},
		Height: int64(header.Height()), //nolint:gosec
		Time:   header.Time(),
		LastBlockID: cmtypes.BlockID{
			Hash: cmbytes.HexBytes(header.LastHeaderHash[:]),
			PartSetHeader: cmtypes.PartSetHeader{
				Total: 0,
				Hash:  nil,
			},
		},
		LastCommitHash:     cmbytes.HexBytes(header.LastCommitHash),
		DataHash:           cmbytes.HexBytes(header.DataHash),
		ConsensusHash:      cmbytes.HexBytes(header.ConsensusHash),
		AppHash:            cmbytes.HexBytes(header.AppHash),
		LastResultsHash:    cmbytes.HexBytes(header.LastResultsHash),
		EvidenceHash:       new(cmtypes.EvidenceData).Hash(),
		ProposerAddress:    header.ProposerAddress,
		ChainID:            header.ChainID(),
		ValidatorsHash:     cmbytes.HexBytes(header.ValidatorHash),
		NextValidatorsHash: cmbytes.HexBytes(header.ValidatorHash),
	}, nil
}

// ToABCIBlock converts Rolkit block into block format defined by ABCI.
// Returned block should pass `ValidateBasic`.
func ToABCIBlock(header *types.SignedHeader, data *types.Data) (*cmtypes.Block, error) {
	abciHeader, err := ToABCIHeader(&header.Header)
	if err != nil {
		return nil, err
	}

	// we have one validator
	if len(header.ProposerAddress) == 0 {
		return nil, errors.New("proposer address is not set")
	}

	abciCommit := getABCICommit(header.Height(), header.Hash(), header.ProposerAddress, header.Time(), header.Signature)

	// This assumes that we have only one signature
	if len(abciCommit.Signatures) == 1 {
		abciCommit.Signatures[0].ValidatorAddress = header.ProposerAddress
	}
	abciBlock := cmtypes.Block{
		Header: abciHeader,
		Evidence: cmtypes.EvidenceData{
			Evidence: nil,
		},
		LastCommit: abciCommit,
	}
	abciBlock.Txs = make([]cmtypes.Tx, len(data.Txs))
	for i := range data.Txs {
		abciBlock.Txs[i] = cmtypes.Tx(data.Txs[i])
	}
	abciBlock.DataHash = cmbytes.HexBytes(header.DataHash)

	return &abciBlock, nil
}

// ToABCIBlockMeta converts Rollkit block into BlockMeta format defined by ABCI
func ToABCIBlockMeta(header *types.SignedHeader, data *types.Data) (*cmtypes.BlockMeta, error) {
	cmblock, err := ToABCIBlock(header, data)
	if err != nil {
		return nil, err
	}
	blockID := cmtypes.BlockID{Hash: cmblock.Hash()}

	return &cmtypes.BlockMeta{
		BlockID:   blockID,
		BlockSize: cmblock.Size(),
		Header:    cmblock.Header,
		NumTxs:    len(cmblock.Txs),
	}, nil
}

// getABCICommit returns a commit format defined by ABCI.
// Other fields (especially ValidatorAddress and Timestamp of Signature) have to be filled by caller.
func getABCICommit(height uint64, hash []byte, val cmtypes.Address, time time.Time, signature []byte) *cmtypes.Commit {
	tmCommit := cmtypes.Commit{
		Height: int64(height), //nolint:gosec
		Round:  0,
		BlockID: cmtypes.BlockID{
			Hash:          cmbytes.HexBytes(hash),
			PartSetHeader: cmtypes.PartSetHeader{},
		},
		Signatures: make([]cmtypes.CommitSig, 1),
	}
	commitSig := cmtypes.CommitSig{
		BlockIDFlag:      cmtypes.BlockIDFlagCommit,
		Signature:        signature,
		ValidatorAddress: val,
		Timestamp:        time,
	}
	tmCommit.Signatures[0] = commitSig

	return &tmCommit
}

func normalizeHeight(height *int64) uint64 {
	var heightValue uint64
	if height == nil {
		var err error
		// TODO: Decide how to handle context here. Using background for now.
		heightValue, err = env.Adapter.RollkitStore.Height(context.Background())
		if err != nil {
			// TODO: Consider logging or returning error
			env.Logger.Error("Failed to get current height in normalizeHeight", "err", err)
			return 0
		}
	} else if *height < 0 {
		// Handle negative heights if they have special meaning (e.g., -1 for latest)
		// Currently, just treat them as 0 or latest, adjust as needed.
		// For now, let's assume negative height means latest valid height.
		var err error
		heightValue, err = env.Adapter.RollkitStore.Height(context.Background())
		if err != nil {
			env.Logger.Error("Failed to get current height for negative height in normalizeHeight", "err", err)
			return 0
		}
	} else {
		heightValue = uint64(*height)
	}

	return heightValue
}

func getBlockMeta(ctx context.Context, n uint64) *cmtypes.BlockMeta {
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx, n)
	if err != nil {
		env.Logger.Error("Failed to get block data in getBlockMeta", "height", n, "err", err)
		return nil
	}
	if header == nil || data == nil {
		env.Logger.Error("Nil header or data returned from GetBlockData", "height", n)
		return nil
	}
	// Assuming ToABCIBlockMeta is now in pkg/rpc/provider/provider_utils.go
	bmeta, err := ToABCIBlockMeta(header, data) // Removed rpc. prefix
	if err != nil {
		env.Logger.Error("Failed to convert block to ABCI block meta", "height", n, "err", err)
		return nil
	}

	return bmeta
}

func filterMinMax(base, height, mini, maxi, limit int64) (int64, int64, error) {
	// filter negatives
	if mini < 0 || maxi < 0 {
		return mini, maxi, errors.New("height must be greater than zero")
	}

	// adjust for default values
	if mini == 0 {
		mini = 1
	}
	if maxi == 0 {
		maxi = height
	}

	// limit max to the height
	maxi = min(height, maxi)

	// limit min to the base
	mini = max(base, mini)

	// limit min to within `limit` of max
	// so the total number of blocks returned will be `limit`
	mini = max(mini, maxi-limit+1)

	if mini > maxi {
		return mini, maxi, fmt.Errorf("%w: min height %d can't be greater than max height %d",
			errors.New("invalid request"), mini, maxi)
	}
	return mini, maxi, nil
}
