package rpc

import (
	"context"
	"fmt"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
)

// Validators implements client.CometRPC.
func (r *RPCServer) Validators(ctx context.Context, heightPtr *int64, pagePtr *int, perPagePtr *int) (*coretypes.ResultValidators, error) {
	// Determine the height to query validators for.
	// If height is nil or latest, use the current block height.
	// Otherwise, use the specified height (if state for that height is available).
	// Note: Loading state for arbitrary past heights might not be supported
	// depending on state pruning. The current implementation implicitly loads latest state.
	height := r.normalizeHeight(heightPtr)

	s, err := r.adapter.LoadState(ctx) // Loads the *latest* state
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
	perPage := validatePerPage(perPagePtr)
	page, err := validatePage(pagePtr, perPage, totalCount)
	if err != nil {
		return nil, err
	}

	start := validateSkipCount(page, perPage)
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
