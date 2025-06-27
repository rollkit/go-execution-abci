package types

import (
	"fmt"

	"cosmossdk.io/math"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var (
	KeyEpochLength      = []byte("EpochLength")
	KeyQuorumFraction   = []byte("QuorumFraction")
	KeyMinParticipation = []byte("MinParticipation")
	KeyPruneAfter       = []byte("PruneAfter")
	KeyEmergencyMode    = []byte("EmergencyMode")
	KeySignMode         = []byte("SignMode")
)

// Default parameter values
var (
	DefaultEpochLength      = uint64(10)                        // todo (Alex): what is a good default?
	DefaultQuorumFraction   = math.LegacyNewDecWithPrec(667, 3) // 2/3
	DefaultMinParticipation = math.LegacyNewDecWithPrec(5, 1)   // 1/2
	DefaultPruneAfter       = uint64(7)
	DefaultSignMode         = SignMode_SIGN_MODE_CHECKPOINT
)

// NewParams creates a new Params instance
func NewParams(
	epochLength uint64,
	quorumFraction math.LegacyDec,
	minParticipation math.LegacyDec,
	pruneAfter uint64,
	signMode SignMode,
) Params {
	return Params{
		EpochLength:      epochLength,
		QuorumFraction:   quorumFraction.String(),
		MinParticipation: minParticipation.String(),
		PruneAfter:       pruneAfter,
		SignMode:         signMode,
	}
}

// DefaultParams returns default parameters
func DefaultParams() Params {
	return NewParams(
		DefaultEpochLength,
		DefaultQuorumFraction,
		DefaultMinParticipation,
		DefaultPruneAfter,
		DefaultSignMode,
	)
}

// ParamSetPairs implements params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyEpochLength, &p.EpochLength, validateEpochLength),
		paramtypes.NewParamSetPair(KeyQuorumFraction, &p.QuorumFraction, validateQuorumFraction),
		paramtypes.NewParamSetPair(KeyMinParticipation, &p.MinParticipation, validateMinParticipation),
		paramtypes.NewParamSetPair(KeyPruneAfter, &p.PruneAfter, validatePruneAfter),
		paramtypes.NewParamSetPair(KeySignMode, &p.SignMode, validateSignMode),
	}
}

// Validate validates the parameter set
func (p Params) Validate() error {
	if err := validateEpochLength(p.EpochLength); err != nil {
		return err
	}
	if err := validateQuorumFraction(p.QuorumFraction); err != nil {
		return err
	}
	if err := validateMinParticipation(p.MinParticipation); err != nil {
		return err
	}
	if err := validatePruneAfter(p.PruneAfter); err != nil {
		return err
	}
	if err := validateSignMode(p.SignMode); err != nil {
		return err
	}
	return nil
}

func validateEpochLength(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v == 0 {
		return fmt.Errorf("epoch length must be positive: %d", v)
	}

	return nil
}

func validateQuorumFraction(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	dec, err := math.LegacyNewDecFromStr(v)
	if err != nil {
		return fmt.Errorf("invalid decimal string: %w", err)
	}

	if dec.LTE(math.LegacyZeroDec()) || dec.GT(math.LegacyOneDec()) {
		return fmt.Errorf("quorum fraction must be between 0 and 1: %s", v)
	}

	return nil
}

func validateMinParticipation(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	dec, err := math.LegacyNewDecFromStr(v)
	if err != nil {
		return fmt.Errorf("invalid decimal string: %w", err)
	}

	if dec.LTE(math.LegacyZeroDec()) || dec.GT(math.LegacyOneDec()) {
		return fmt.Errorf("min participation must be between 0 and 1: %s", v)
	}

	return nil
}

func validatePruneAfter(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v == 0 {
		return fmt.Errorf("prune after must be positive: %d", v)
	}

	return nil
}

func validateSignMode(i interface{}) error {
	v, ok := i.(SignMode)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v == SignMode_SIGN_MODE_UNSPECIFIED {
		return fmt.Errorf("sign mode cannot be unspecified")
	}

	if v != SignMode_SIGN_MODE_CHECKPOINT {
		return fmt.Errorf("invalid sign mode: %d", v)
	}

	return nil
}

// ParamKeyTable returns the parameter key table
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}
