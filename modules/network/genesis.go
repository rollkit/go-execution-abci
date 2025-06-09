package network

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/rollkit/go-execution-abci/modules/network/keeper"
	"github.com/rollkit/go-execution-abci/modules/network/types"
)

// InitGenesis initializes the network module's state from a provided genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	// Set module params
	k.SetParams(ctx, genState.Params)

	// Set validator indices
	for _, vi := range genState.ValidatorIndices {
		k.SetValidatorIndex(ctx, vi.Address, uint16(vi.Index), vi.Power)
		// Also add to attester set
		k.SetAttesterSetMember(ctx, vi.Address)
	}

	// Set attestation bitmaps
	for _, ab := range genState.AttestationBitmaps {
		k.SetAttestationBitmap(ctx, ab.Height, ab.Bitmap)
		// Store full attestation info
		store := ctx.KVStore(k.GetStoreKey())
		key := append([]byte("attestation_info/"), sdk.Uint64ToBigEndian(uint64(ab.Height))...)
		bz := k.GetCodec().MustMarshal(&ab)
		store.Set(key, bz)
		
		if ab.SoftConfirmed {
			setSoftConfirmed(ctx, k, ab.Height)
		}
	}
}

// ExportGenesis returns the network module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesisState()
	genesis.Params = k.GetParams(ctx)

	// Export validator indices
	store := ctx.KVStore(k.GetStoreKey())
	iterator := sdk.KVStorePrefixIterator(store, types.ValidatorIndexPrefix)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		addr := string(iterator.Key()[len(types.ValidatorIndexPrefix):])
		index, _ := k.GetValidatorIndex(ctx, addr)
		power := k.GetValidatorPower(ctx, index)
		
		genesis.ValidatorIndices = append(genesis.ValidatorIndices, types.ValidatorIndex{
			Address: addr,
			Index:   uint32(index),
			Power:   power,
		})
	}

	// Export attestation bitmaps
	attIterator := sdk.KVStorePrefixIterator(store, []byte("attestation_info/"))
	defer attIterator.Close()

	for ; attIterator.Valid(); attIterator.Next() {
		var ab types.AttestationBitmap
		k.GetCodec().MustUnmarshal(attIterator.Value(), &ab)
		genesis.AttestationBitmaps = append(genesis.AttestationBitmaps, ab)
	}

	return genesis
}

// Helper functions
func (k keeper.Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}

func (k keeper.Keeper) GetCodec() codec.BinaryCodec {
	return k.cdc
}

func setSoftConfirmed(ctx sdk.Context, k keeper.Keeper, height int64) {
	store := ctx.KVStore(k.GetStoreKey())
	key := append([]byte("soft_confirmed/"), sdk.Uint64ToBigEndian(uint64(height))...)
	store.Set(key, []byte{1})
}