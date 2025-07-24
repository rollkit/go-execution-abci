package rollkitmngr

import (
	"cosmossdk.io/core/address"
	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/core/store"
	"cosmossdk.io/depinject"
	"cosmossdk.io/depinject/appconfig"
	"github.com/cosmos/cosmos-sdk/codec"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/evstack/ev-abci/modules/rollkitmngr/keeper"
	modulev1 "github.com/evstack/ev-abci/modules/rollkitmngr/module"
	"github.com/evstack/ev-abci/modules/rollkitmngr/types"
)

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}

func init() {
	appconfig.Register(
		&modulev1.Module{},
		appconfig.Provide(ProvideModule),
	)
}

type ModuleInputs struct {
	depinject.In

	Config       *modulev1.Module
	Cdc          codec.Codec
	StoreService store.KVStoreService
	AddressCodec address.Codec
	// optional, used to detect if IBC module is enabled.
	// When IBC module is present, use `depinject.Provide(IBCStoreKey(ibcStoreKey))`
	IBCStoreKey keeper.IbcKVStoreKeyAlias `optional:"true"`

	StakingKeeper types.StakingKeeper
}

// Dependency Injection Outputs
type ModuleOutputs struct {
	depinject.Out

	SequencerKeeper keeper.Keeper
	Module          appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
	// default to governance authority if not provided
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	if in.Config.Authority != "" {
		authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
	}

	k := keeper.NewKeeper(
		in.Cdc,
		in.StoreService,
		in.AddressCodec,
		in.StakingKeeper,
		in.IBCStoreKey,
		authority.String(),
	)
	m := NewAppModule(in.Cdc, k)

	return ModuleOutputs{SequencerKeeper: k, Module: m}
}
