package keeper

import (
	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

type Keeper struct {
	storeService storetypes.KVStoreService
	cdc          codec.BinaryCodec
	authority    string

	authKeeper    types.AccountKeeper
	stakingKeeper types.StakingKeeper

	Schema         collections.Schema
	NextSequencers collections.Map[uint64, types.Sequencer]
}

// NewKeeper creates a new sequencer Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	ak types.AccountKeeper,
	stakingKeeper types.StakingKeeper,
	authority string,
) Keeper {
	// ensure that authority is a valid account address
	if _, err := ak.AddressCodec().StringToBytes(authority); err != nil {
		panic("authority is not a valid acc address")
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		storeService:  storeService,
		cdc:           cdc,
		authority:     authority,
		authKeeper:    ak,
		stakingKeeper: stakingKeeper,
		NextSequencers: collections.NewMap(
			sb,
			types.NextSequencersKey,
			"next_sequencers",
			collections.Uint64Key,
			codec.CollValue[types.Sequencer](cdc),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}
