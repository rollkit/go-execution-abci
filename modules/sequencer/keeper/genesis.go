package keeper

import (
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

func (k Keeper) InitGenesis(ctx sdk.Context, data *types.GenesisState) ([]abci.ValidatorUpdate, error) {
	if len(data.Sequencers) == 0 {
		return []abci.ValidatorUpdate{}, fmt.Errorf("no sequencers found in genesis state")
	}

	seq := data.Sequencers[0]
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
