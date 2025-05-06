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
	// Determine the height to query validators for.
	// If height is nil or latest, use the current block height.
	// Otherwise, use the specified height (if state for that height is available).
	// Note: Loading state for arbitrary past heights might not be supported
	// depending on state pruning. The current implementation implicitly loads latest state.
	height := normalizeHeight(heightPtr)

	s, err := env.Adapter.Store.LoadState(ctx.Context()) // Loads the *latest* state
	if err != nil {
		return nil, fmt.Errorf("failed to load current state: %w", err)
	}

	// Check if the requested height matches the loaded state height if a specific height was requested.
	// If state history is not kept, this check might be necessary or always fail for past heights.
	if heightPtr != nil && int64(height) != s.LastBlockHeight {
		// This implies state for the requested height is not available with the current LoadState method.
		// Adjust implementation if historical state access is possible and needed.
		return nil, fmt.Errorf("validator set for height %d is not available, latest height is %d", height, s.LastBlockHeight)
	}

	validators := s.Validators.Validators
	totalCount := len(validators)

	// Handle pagination
	perPage := validatePerPage(perPagePtr)                  // Use local function
	page, err := validatePage(pagePtr, perPage, totalCount) // Use local function
	if err != nil {
		return nil, err
	}

	start := validateSkipCount(page, perPage) // Use local function
	end := start + perPage
	if end > totalCount {
		end = totalCount
	}

	// Ensure start index is not out of bounds, can happen if page * perPage > totalCount
	if start >= totalCount {
		validators = []*cmttypes.Validator{} // Return empty slice if page is out of range
	} else {
		validators = validators[start:end]
	}

	return &coretypes.ResultValidators{
		BlockHeight: s.LastBlockHeight, // Return the height for which the validator set is valid (latest)
		Validators:  validators,
		Total:       totalCount, // Total number of validators *before* pagination
	}, nil
}

// DumpConsensusState dumps consensus state.
// UNSTABLE
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/dump_consensus_state
func DumpConsensusState(ctx *rpctypes.Context) (*coretypes.ResultDumpConsensusState, error) {
	// Rollkit doesn't have Tendermint consensus state.
	return nil, ErrConsensusStateNotAvailable
}

// ConsensusState returns a concise summary of the consensus state.
// UNSTABLE
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/consensus_state
func ConsensusState(ctx *rpctypes.Context) (*coretypes.ResultConsensusState, error) {
	// Rollkit doesn't have Tendermint consensus state.
	return nil, ErrConsensusStateNotAvailable
}

// ConsensusParams gets the consensus parameters at the given block height.
// If no height is provided, it will fetch the latest consensus params.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/consensus_params
func ConsensusParams(ctx *rpctypes.Context, heightPtr *int64) (*coretypes.ResultConsensusParams, error) {
	state, err := env.Adapter.Store.LoadState(ctx.Context())
	if err != nil {
		return nil, err
	}
	params := state.ConsensusParams
	// Use normalizeHeight which should be moved to rpcProvider as well
	normalizedHeight := normalizeHeight(heightPtr) // Changed r.normalizeHeight to p.normalizeHeight
	return &coretypes.ResultConsensusParams{
		BlockHeight: int64(normalizedHeight), //nolint:gosec
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
