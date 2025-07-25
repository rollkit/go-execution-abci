package core

import (
	"fmt"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// Validators gets the validator set at the given block height.
//
// If no height is provided, it will fetch the latest validator set. Note the
// validators are sorted by their voting power - this is the canonical order
// for the validators in the set as used in computing their Merkle root.
//
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/validators
func Validators(ctx *rpctypes.Context, heightPtr *int64, pagePtr, perPagePtr *int) (*coretypes.ResultValidators, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize height: %w", err)
	}

	genesisValidators := env.Adapter.AppGenesis.Consensus.Validators
	if len(genesisValidators) != 1 {
		return nil, fmt.Errorf("there should be exactly one validator in genesis")
	}

	// Since it's a centralized sequencer
	// changed behavior to get this from genesis
	genesisValidator := genesisValidators[0]
	validator := cmttypes.Validator{
		Address:          genesisValidator.Address,
		PubKey:           genesisValidator.PubKey,
		VotingPower:      genesisValidator.Power,
		ProposerPriority: int64(1),
	}

	return &coretypes.ResultValidators{
		BlockHeight: int64(height), //nolint:gosec
		Validators: []*cmttypes.Validator{
			&validator,
		},
		Count: 1,
		Total: 1,
	}, nil
}

// DumpConsensusState dumps consensus state.
// UNSTABLE
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/dump_consensus_state
func DumpConsensusState(ctx *rpctypes.Context) (*coretypes.ResultDumpConsensusState, error) {
	// Evolve doesn't have CometBFT consensus state.
	return nil, ErrConsensusStateNotAvailable
}

// ConsensusState returns a concise summary of the consensus state.
// UNSTABLE
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/consensus_state
func ConsensusState(ctx *rpctypes.Context) (*coretypes.ResultConsensusState, error) {
	// Evolve doesn't have CometBFT consensus state.
	return nil, ErrConsensusStateNotAvailable
}

// ConsensusParams gets the consensus parameters at the given block height.
// If no height is provided, it will fetch the latest consensus params.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/consensus_params
func ConsensusParams(ctx *rpctypes.Context, heightPtr *int64) (*coretypes.ResultConsensusParams, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize height: %w", err)
	}

	state, err := env.Adapter.Store.LoadState(ctx.Context())
	if err != nil {
		return nil, err
	}

	params := state.ConsensusParams
	return &coretypes.ResultConsensusParams{
		BlockHeight: int64(height), //nolint:gosec
		ConsensusParams: cmttypes.ConsensusParams{
			Block: cmttypes.BlockParams{
				MaxBytes: params.Block.MaxBytes,
				MaxGas:   params.Block.MaxGas,
			},
			Evidence: cmttypes.EvidenceParams{
				MaxAgeNumBlocks: params.Evidence.MaxAgeNumBlocks,
				MaxAgeDuration:  params.Evidence.MaxAgeDuration,
				MaxBytes:        params.Evidence.MaxBytes,
			},
			Validator: cmttypes.ValidatorParams{
				PubKeyTypes: params.Validator.PubKeyTypes,
			},
			Version: cmttypes.VersionParams{
				App: params.Version.App,
			},
		},
	}, nil
}
