package types

import (
	"encoding/binary"

	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name
	ModuleName = "network"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName
)

// KVStore key prefixes
var (
	// todo (Alex): use numbers instaed of verbose keyse?
	ValidatorIndexPrefix        = collections.NewPrefix("validator_index")
	ValidatorPowerPrefix        = collections.NewPrefix("validator_power")
	AttestationBitmapPrefix     = collections.NewPrefix("attestation_bitmap")
	EpochBitmapPrefix           = collections.NewPrefix("epoch_bitmap")
	AttesterSetPrefix           = collections.NewPrefix("attester_set")
	SignaturePrefix             = collections.NewPrefix("signature")
	StoredAttestationInfoPrefix = collections.NewPrefix("stored_attestation_info")
	ParamsKey                   = collections.NewPrefix("params")
)

// GetValidatorIndexKey returns the key for validator index mapping
func GetValidatorIndexKey(addr string) []byte {
	return append(ValidatorIndexPrefix, []byte(addr)...)
}

// GetValidatorPowerKey returns the key for validator power by index
func GetValidatorPowerKey(index uint16) []byte {
	bz := make([]byte, 2)
	binary.BigEndian.PutUint16(bz, index)
	return append(ValidatorPowerPrefix, bz...)
}

// GetAttestationKey returns the key for attestation bitmap at height
func GetAttestationKey(height int64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(height))
	return append(AttestationBitmapPrefix, bz...)
}

// GetSignatureKey returns the key for validator signature at height
func GetSignatureKey(height int64, addr string) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, uint64(height))
	return append(append(SignaturePrefix, bz...), []byte(addr)...)
}

// GetEpochBitmapKey returns the key for epoch participation bitmap
func GetEpochBitmapKey(epoch uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, epoch)
	return append(EpochBitmapPrefix, bz...)
}

// GetAttesterSetKey returns the key for attester set membership
func GetAttesterSetKey(addr string) []byte {
	return append(AttesterSetPrefix, []byte(addr)...)
}
