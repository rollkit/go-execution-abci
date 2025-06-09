package network

import (
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/depinject"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	"github.com/rollkit/go-execution-abci/modules/network/keeper"
	modulev1 "github.com/rollkit/go-execution-abci/modules/network/module/v1"
	"github.com/rollkit/go-execution-abci/modules/network/types"
)

var _ appmodule.AppModule = AppModule{}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

func init() {
	appmodule.Register(
		&modulev1.Module{},
		appmodule.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	Config         *modulev1.Module
	Cdc            codec.Codec
	Key            *sdk.KVStoreKey
	ParamsSubspace paramtypes.Subspace
	StakingKeeper  types.StakingKeeper
	AccountKeeper  types.AccountKeeper
	BankKeeper     types.BankKeeper
}

type ModuleOutputs struct {
	depinject.Out

	NetworkKeeper keeper.Keeper
	Module        appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// default to governance authority if not provided
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
	}

	k := keeper.NewKeeper(
		in.Cdc,
		in.Key,
		in.ParamsSubspace,
		in.StakingKeeper,
		in.AccountKeeper,
		in.BankKeeper,
		authority.String(),
	)
	m := NewAppModule(
		in.Cdc,
		k,
		in.AccountKeeper,
		in.BankKeeper,
	)

	return ModuleOutputs{NetworkKeeper: k, Module: m}
}
