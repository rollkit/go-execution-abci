package keeper

import (
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

// MigrateToSequencer includes the logic that needs to execute during the process of a CometBFT chain to rollup changeover **or** a sequencer changeover.
// This method constructs validator updates that will be given to CometBFT, that will gradually (TODO) remove all the validators and add the sequencer.
func (k Keeper) MigrateToSequencer(ctx sdk.Context, nextSeq types.Sequencer, lastValidatorSet []stakingtypes.Validator) (initialValUpdates []abci.ValidatorUpdate, err error) {
	pk, err := nextSeq.TmConsPublicKey()
	if err != nil {
		return nil, err
	}
	sequencerUpdate := abci.ValidatorUpdate{
		PubKey: pk,
		Power:  1,
	}

	for _, val := range lastValidatorSet {
		powerUpdate := val.ABCIValidatorUpdateZero()
		if val.ConsensusPubkey.Equal(nextSeq.ConsensusPubkey) {
			continue
		}
		initialValUpdates = append(initialValUpdates, powerUpdate)
	}

	if len(lastValidatorSet) > 1 {
		k.Logger(ctx).Info("Migration to single sequencer complete. Chain is now using Rollkit.")
	} else {
		k.Logger(ctx).Info("Sequencer change complete. Chain is now using the new sequencer.")
	}

	// remove the sequencer from the next sequencer map
	if err = k.NextSequencers.Remove(ctx, uint64(ctx.BlockHeight())); err != nil {
		return nil, err
	}

	return append(initialValUpdates, sequencerUpdate), nil
}
