package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Optimisations

// 0.a Skip the slow migration if IBC is not enabled
// 0.b Only from migration from CometBFT to Rollkit
// 0.c BlockHeight is the height that starts the migration, as the migration can take several blocks to complete, we need to return the expected halt height.

// Two options:

// 1. Migrate to sequencer only
/*
	- Slowly migrate the validator set to the sequencer
	- We need to check if the sequencer is already in the validator set, otherwise it would need to be added to the validator set (check what are the implications of this)
	- If IBC enabled and vp of sequencer 2/3, migration can be done immediately
*/

// 2. Migrate to sequencer with attesters network
/*
	- If IBC enabled and vp of attesters network 2/3, migration can be done immediately
	- Otherwise, slowly migrate the validator set to the attesters
*/

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
