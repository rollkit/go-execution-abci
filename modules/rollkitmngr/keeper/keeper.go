package keeper

import (
	"context"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollkit/go-execution-abci/modules/rollkitmngr/types"
)

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.BinaryCodec
	authority    string

	authKeeper    types.AccountKeeper
	stakingKeeper types.StakingKeeper

	Schema    collections.Schema
	Sequencer collections.Item[types.Sequencer]
	Migration collections.Item[types.RollkitMigration]
}

// NewKeeper creates a new sequencer Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
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
		Sequencer: collections.NewItem(
			sb,
			types.SequencerKey,
			"sequencer",
			codec.CollValue[types.Sequencer](cdc),
		),
		Migration: collections.NewItem(
			sb,
			types.MigrationKey,
			"rollkit_migration",
			codec.CollValue[types.RollkitMigration](cdc),
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
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", "x/"+types.ModuleName)
}

// IsMigrating checks if the migration to Rollkit is in progress.
// It checks if the RollkitMigration item exists in the store.
// And if it does, it verifies we are past the block height that started the migration.
func (k Keeper) IsMigrating(ctx context.Context) (start, end uint64, ok bool) {
	migration, err := k.Migration.Get(ctx)
	if err == collections.ErrNotFound {
		return 0, 0, false
	} else if err != nil {
		k.Logger(ctx).Error("failed to get Rollkit migration state", "error", err)
		return 0, 0, false
	}

	// smoothen the migration over IBCSmoothingFactor blocks, in order to migrate the validator set to the sequencer or attesters network when IBC is enabled.
	migrationEndHeight := migration.BlockHeight + IBCSmoothingFactor

	// If IBC is not enabled, the migration can be done in one block.
	if !k.isIBCEnabled(ctx) {
		migrationEndHeight = migration.BlockHeight + 1
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	migrationInProgress := sdkCtx.BlockHeight() >= int64(migration.BlockHeight) && sdkCtx.BlockHeight() <= int64(migrationEndHeight)

	return migration.BlockHeight, migrationEndHeight, migrationInProgress
}

// isIBCEnabled checks if IBC is enabled on the chain.
// In order to not import the IBC module, we only check if the IBC store exists,
// but not the ibc params. This should be sufficient for our use case.
func (k Keeper) isIBCEnabled(ctx context.Context) (enabled bool) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// ref https://github.com/cosmos/ibc-go/blob/v10.2.0/modules/core/exported/module.go
	ibcStoreKey := storetypes.NewKVStoreKey("ibc")

	ms := sdkCtx.MultiStore()
	ms.GetKVStore(ibcStoreKey)
	defer func() {
		if r := recover(); r != nil {
			// If we panic, it means the store does not exist, so IBC is not enabled.
			enabled = false
		}
	}()

	enabled = true // has not panicked, so store exists

	return
}
