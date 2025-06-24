package types

import (
	"fmt"
	"math"
)

// DefaultGenesisState returns the default genesis state
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:             DefaultParams(),
		ValidatorIndices:   []ValidatorIndex{},
		AttestationBitmaps: []AttestationBitmap{},
	}
}

// Validate performs basic genesis state validation
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	if len(gs.ValidatorIndices) > math.MaxUint16 {
		return fmt.Errorf("too many validator indices")
	}
	// Check for duplicate validator indices
	indexMap := make(map[string]bool)
	usedIndices := make(map[uint32]bool)

	for _, vi := range gs.ValidatorIndices {
		if indexMap[vi.Address] {
			return fmt.Errorf("duplicate validator address: %s", vi.Address)
		}
		if usedIndices[vi.Index] {
			return fmt.Errorf("duplicate index: %d", vi.Index)
		}
		indexMap[vi.Address] = true
		usedIndices[vi.Index] = true
	}

	// Validate attestation bitmaps
	for _, ab := range gs.AttestationBitmaps {
		if ab.Height <= 0 {
			return fmt.Errorf("invalid attestation height: %d", ab.Height)
		}
		if ab.VotedPower > ab.TotalPower {
			return fmt.Errorf("voted power exceeds total power at height %d", ab.Height)
		}
	}

	return nil
}
