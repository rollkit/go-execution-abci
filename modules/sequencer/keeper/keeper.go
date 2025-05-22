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

	authKeeper types.AccountKeeper
	authority  string

	Schema        collections.Schema
	Sequencer     collections.Item[types.Sequencer]
	NextSequencer collections.Map[uint64, types.Sequencer]
}

// NewKeeper creates a new sequencer Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	ak types.AccountKeeper,
	authority string,
) Keeper {
	// ensure that authority is a valid account address
	if _, err := ak.AddressCodec().StringToBytes(authority); err != nil {
		panic("authority is not a valid acc address")
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		authKeeper:   ak,
		authority:    authority,
		Sequencer: collections.NewItem(
			sb,
			types.SequencerConsAddrKey,
			"sequencer",
			codec.CollValue[types.Sequencer](cdc),
		),
		NextSequencer: collections.NewMap(
			sb,
			types.NextSequencerKey,
			"next_sequencer",
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
