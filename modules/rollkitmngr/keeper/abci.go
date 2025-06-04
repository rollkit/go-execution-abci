package keeper

import (
	"context"
	"errors"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

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

// PreBlocker makes sure the chain halts at block height + 1 after the migration end.
// This is to ensure that the migration is completed with a binary switch.
func (k Keeper) PreBlocker(ctx context.Context) error {
	_, end, _ := k.IsMigrating(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// one block after the migration end, we halt forcing a binary switch
	if end > 0 && end+1 == uint64(sdkCtx.BlockHeight()) {
		// remove the migration state from the store
		// this is to ensure at restart we won't halt the chain again
		if err := k.Migration.Remove(ctx); err != nil {
			return sdkerrors.ErrLogic.Wrapf("failed to delete migration state: %v", err)
		}

		return errors.New("app migration to rollkit is in progress, switch to the new binary and run the rollkit migration command to complete the migration")
	}

	return nil
}

// EndBlocker is called at the end of every block and returns sequencer updates.
func (k Keeper) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	start, _, shouldBeMigrating := k.IsMigrating(ctx)
	if !shouldBeMigrating || start > uint64(sdkCtx.BlockHeight()) {
		// no migration in progress, return empty updates
		return []abci.ValidatorUpdate{}, nil
	}

	migration, err := k.Migration.Get(ctx)
	if err != nil {
		return nil, sdkerrors.ErrLogic.Wrapf("failed to get migration state: %v", err)
	}

	validatorSet, err := k.stakingKeeper.GetLastValidators(sdkCtx)
	if err != nil {
		return nil, err
	}

	if !k.isIBCEnabled(ctx) {
		// if IBC is not enabled, we can migrate immediately
		return k.migrateNow(ctx, migration, validatorSet)
	}

	return k.migrateOver(sdkCtx, migration, validatorSet)
}
