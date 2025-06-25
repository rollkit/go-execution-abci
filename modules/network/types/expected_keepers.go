package types

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	tyrollkittypes "github.com/rollkit/rollkit/types"
)

// StakingKeeper defines the expected staking keeper interface
type StakingKeeper interface {
	GetAllValidators(ctx context.Context) (validators []stakingtypes.Validator, err error)
	GetValidator(ctx context.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, err error)
	GetLastValidators(ctx context.Context) (validators []stakingtypes.Validator, err error)
	GetLastTotalPower(ctx context.Context) (math.Int, error)
}

// AccountKeeper defines the expected account keeper interface
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// BankKeeper defines the expected bank keeper interface
type BankKeeper interface {
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

// BlockSource is the block store
type BlockSource interface {
	GetBlockData(ctx context.Context, height uint64) (*tyrollkittypes.SignedHeader, *tyrollkittypes.Data, error)
}
