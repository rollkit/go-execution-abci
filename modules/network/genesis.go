package network

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollkit/go-execution-abci/modules/network/keeper"
	"github.com/rollkit/go-execution-abci/modules/network/types"
)

// InitGenesis initializes the network module's state from a provided genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) error {
	// Set module params
	if err := k.SetParams(ctx, genState.Params); err != nil {
		return fmt.Errorf("set params: %s", err)
	}

	// Set validator indices
	for _, vi := range genState.ValidatorIndices {
		if err := k.SetValidatorIndex(ctx, vi.Address, uint16(vi.Index), vi.Power); err != nil {
			return err
		}
		// Also add to attester set
		if err := k.SetAttesterSetMember(ctx, vi.Address); err != nil {
			return err
		}
	}

	// Set attestation bitmaps
	for _, ab := range genState.AttestationBitmaps {
		if err := k.SetAttestationBitmap(ctx, ab.Height, ab.Bitmap); err != nil {
			return err
		}
		// Store full attestation info using collections API
		if err := k.StoredAttestationInfo.Set(ctx, ab.Height, ab); err != nil {
			return err
		}

		if ab.SoftConfirmed {
			if err := setSoftConfirmed(ctx, k, ab.Height); err != nil {
				return err
			}
		}
	}
	return nil
}

// ExportGenesis returns the network module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesisState()
	genesis.Params = k.GetParams(ctx)

	// Export validator indices using collections API
	var validatorIndices []types.ValidatorIndex
	// Iterate through all validator indices
	if err := k.ValidatorIndex.Walk(ctx, nil, func(addr string, index uint16) (bool, error) {
		power, err := k.GetValidatorPower(ctx, index)
		if err != nil {
			return false, fmt.Errorf("get validator power: %w", err)
		}
		validatorIndices = append(validatorIndices, types.ValidatorIndex{
			Address: addr,
			Index:   uint32(index),
			Power:   power,
		})
		return false, nil
	}); err != nil {
		panic(err)
	}
	genesis.ValidatorIndices = validatorIndices

	// Export attestation bitmaps using collections API
	var attestationBitmaps []types.AttestationBitmap
	// Iterate through all stored attestation info
	if err := k.StoredAttestationInfo.Walk(ctx, nil, func(height int64, ab types.AttestationBitmap) (bool, error) {
		attestationBitmaps = append(attestationBitmaps, ab)
		return false, nil
	}); err != nil {
		panic(err)
	}
	genesis.AttestationBitmaps = attestationBitmaps

	return genesis
}

// Helper function to set soft confirmed status
func setSoftConfirmed(ctx sdk.Context, k keeper.Keeper, height int64) error {
	// Get the existing attestation bitmap
	ab, err := k.StoredAttestationInfo.Get(ctx, height)
	if err != nil {
		// If there's no existing attestation bitmap, we can't set it as soft confirmed
		return err
	}

	// Set the SoftConfirmed field to true
	ab.SoftConfirmed = true

	// Update the attestation bitmap in the collection
	if err := k.StoredAttestationInfo.Set(ctx, height, ab); err != nil {
		return err
	}
	return nil
}
