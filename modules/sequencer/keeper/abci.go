package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO:
// We should slowly migrate the validator set to the sequencer if validator set is greater than 1
// We need to check if the sequencer is already in the validator set, otherwise it would need to be added to the validator set (check what are the implications of this)
// Make sure logic works when going from CometBFT validator set to Rollkit
// AND Rollkit sequencer to another
// Clarify is multiple sequencers are supported in the future
// Skip the slow migration if IBC is not enabled

// EndBlocker is called at the end of every block and returns sequencer updates.
func (k Keeper) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	nextSequencer, err := k.NextSequencers.Get(sdkCtx, uint64(sdkCtx.BlockHeight()))
	if errors.Is(err, collections.ErrNotFound) {
		// no sequencer change scheduled for this block
		return []abci.ValidatorUpdate{}, nil
	} else if err != nil {
		return nil, err
	}

	validatorSet, err := k.stakingKeeper.GetLastValidators(sdkCtx)
	if err != nil {
		return nil, err
	}

	return k.MigrateToSequencer(sdkCtx, nextSequencer, validatorSet)
}
