// NewKeeper creates a new staking Keeper instance
package keeper

import (
	"context"

	addresscodec "cosmossdk.io/core/address"
	storetypes "cosmossdk.io/core/store"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// wrapper  staking keeper
type Keeper struct {
	*stakingkeeper.Keeper
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	authority string,
	validatorAddressCodec addresscodec.Codec,
	consensusAddressCodec addresscodec.Codec,
) Keeper {
	k := stakingkeeper.NewKeeper(cdc, storeService, ak, bk, authority, validatorAddressCodec, consensusAddressCodec)
	return Keeper{k}
}

// ApplyAndReturnValidatorSetUpdates applies state changes but does not return validator updates (except on genesis, in order to bootstrap the chain).
func (k Keeper) ApplyAndReturnValidatorSetUpdates(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	valUpdates, err := k.Keeper.ApplyAndReturnValidatorSetUpdates(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if sdkCtx.BlockHeight() == 0 {
		// genesis block, return validator updates from gentxs.
		return valUpdates, err
	}

	return []abci.ValidatorUpdate{}, err
}
