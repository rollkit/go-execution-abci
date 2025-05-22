// NewKeeper creates a new staking Keeper instance
package keeper

import (
	"context"

	addresscodec "cosmossdk.io/core/address"
	storetypes "cosmossdk.io/core/store"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/codec"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
)

// wrapper  staking keeper
type Keeper struct {
	stakingkeeper.Keeper
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
	return Keeper{*k}
}

// ApplyAndReturnValidatorSetUpdates applies state changes but does not return validator updates.
func (k Keeper) ApplyAndReturnValidatorSetUpdates(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	_, err := k.Keeper.ApplyAndReturnValidatorSetUpdates(ctx)
	return []abci.ValidatorUpdate{}, err
}
