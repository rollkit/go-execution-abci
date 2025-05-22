package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

var LastValidatorSet []stakingtypes.Validator

// MigrateFromSovereign is called when the chain is starting up and needs to migrate from a sovereign chain.
func (k Keeper) MigrateFromSovereign(ctx sdk.Context, sequencer types.Sequencer) error {
	return k.Sequencer.Set(ctx, sequencer)
}

// ChangeoverToConsumer includes the logic that needs to execute during the process of a CometBFT chain to rollup changeover.
// This method constructs validator updates that will be given to tendermint, which allows the consumer chain to start using the provider valset, while the standalone valset is given zero voting power where appropriate.
func (k Keeper) ChangeoverToRollup(ctx sdk.Context, lastValidatorSet []stakingtypes.Validator) (initialValUpdates []abci.ValidatorUpdate, err error) {
	seq, err := k.Sequencer.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return nil, errors.New("sequencer not found")
	} else if err != nil {
		return nil, fmt.Errorf("failed to get sequencer: %w", err)
	}

	pk, err := seq.TmConsPublicKey()
	if err != nil {
		return nil, err
	}
	sequencersUpdate := abci.ValidatorUpdate{
		PubKey: pk,
		Power:  1,
	}

	for _, val := range lastValidatorSet {
		powerUpdate := val.ABCIValidatorUpdateZero()
		if val.ConsensusPubkey.Equal(seq.ConsensusPubkey) {
			continue
		}
		initialValUpdates = append(initialValUpdates, powerUpdate)
	}

	LastValidatorSet = nil
	k.Logger(ctx).Info("Rollup changeover complete - you are now a rollup chain!")

	err = k.NextSequencerChangeHeight.Remove(ctx)
	if err != nil {
		return nil, err
	}

	return append(initialValUpdates, sequencersUpdate), nil
}
