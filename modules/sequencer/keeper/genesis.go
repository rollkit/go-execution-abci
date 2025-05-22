package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, data *types.GenesisState) ([]abci.ValidatorUpdate, error) {
	if len(data.Sequencers) == 0 {
		return []abci.ValidatorUpdate{}, nil
	}

	// Set the initial sequence
	if err := k.Sequencer.Set(ctx, data.Sequencers[0]); err != nil {
		return nil, err
	}

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

	return []abci.ValidatorUpdate{
		{
			PubKey: pk,
			Power:  1,
		},
	}, nil
}
