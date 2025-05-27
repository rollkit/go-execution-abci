package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

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
