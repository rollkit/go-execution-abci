package keeper

import (
	"fmt"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

// Keeper of the network store
type Keeper struct {
	cdc           codec.BinaryCodec
	stakingKeeper types.StakingKeeper
	accountKeeper types.AccountKeeper
	bankKeeper    types.BankKeeper
	authority     string
	bitmapHelper  *BitmapHelper
	blockSource   types.BlockSource

	// Collections for state management
	ValidatorIndex collections.Map[string, uint16]
	ValidatorPower collections.Map[uint16, uint64]

	//AttestationBitmap tracks attestations for a block
	AttestationBitmap collections.Map[int64, []byte]
	// EpochBitmap track tracks participation over a range of epochs
	EpochBitmap           collections.Map[uint64, []byte]
	AttesterSet           collections.KeySet[string]
	CommitSignatures      collections.Map[collections.Pair[int64, string], cmtproto.CommitSig]
	StoredAttestationInfo collections.Map[int64, types.AttestationBitmap]
	Params                collections.Item[types.Params]
}

// NewKeeper creates a new network Keeper instance
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService, // Changed from sdk.StoreKey
	sk types.StakingKeeper,
	ak types.AccountKeeper,
	bk types.BankKeeper,
	blockSource types.BlockSource,
	authority string,
) Keeper {

	sb := collections.NewSchemaBuilder(storeService)
	keeper := Keeper{
		cdc:           cdc,
		stakingKeeper: sk,
		accountKeeper: ak,
		bankKeeper:    bk,
		authority:     authority,
		bitmapHelper:  NewBitmapHelper(),
		blockSource:   blockSource,

		ValidatorIndex:        collections.NewMap(sb, types.ValidatorIndexPrefix, "validator_index", collections.StringKey, collections.Uint16Value),
		ValidatorPower:        collections.NewMap(sb, types.ValidatorPowerPrefix, "validator_power", collections.Uint16Key, collections.Uint64Value),
		AttestationBitmap:     collections.NewMap(sb, types.AttestationBitmapPrefix, "attestation_bitmap", collections.Int64Key, collections.BytesValue),
		EpochBitmap:           collections.NewMap(sb, types.EpochBitmapPrefix, "epoch_bitmap", collections.Uint64Key, collections.BytesValue),
		AttesterSet:           collections.NewKeySet(sb, types.AttesterSetPrefix, "attester_set", collections.StringKey),
		CommitSignatures:      collections.NewMap(sb, types.CommitPrefix, "commits", collections.PairKeyCodec(collections.Int64Key, collections.StringKey), codec.CollValue[cmtproto.CommitSig](cdc)),
		StoredAttestationInfo: collections.NewMap(sb, types.StoredAttestationInfoPrefix, "stored_attestation_info", collections.Int64Key, codec.CollValue[types.AttestationBitmap](cdc)), // Initialize new collection
		Params:                collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
	}

	// The schema is built implicitly when the first collection is created or can be explicitly built.
	// schema, err := sb.Build()
	// if err != nil {
	// 	panic(err)
	// }
	// keeper.schema = schema

	return keeper
}

// GetAuthority returns the module authority
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger returns a module-specific logger
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "network")
}

// GetParams get all parameters as types.Params
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	p, _ := k.Params.Get(ctx)
	return p
}

// SetParams set the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}

// SetValidatorIndex stores the validator index mapping and power
func (k Keeper) SetValidatorIndex(ctx sdk.Context, addr string, index uint16, power uint64) error {
	if err := k.ValidatorIndex.Set(ctx, addr, index); err != nil {
		return err
	}
	return k.ValidatorPower.Set(ctx, index, power)
}

// GetValidatorIndex retrieves the validator index
func (k Keeper) GetValidatorIndex(ctx sdk.Context, addr string) (uint16, bool) {
	index, err := k.ValidatorIndex.Get(ctx, addr)
	if err != nil {
		// For 'not found', collections.ErrNotFound can be checked specifically if needed.
		return 0, false
	}
	return index, true
}

// GetValidatorPower retrieves the validator power by index
func (k Keeper) GetValidatorPower(ctx sdk.Context, index uint16) (uint64, error) {
	power, err := k.ValidatorPower.Get(ctx, index)
	return power, err
}

// SetAttestationBitmap stores the attestation bitmap for a height
func (k Keeper) SetAttestationBitmap(ctx sdk.Context, height int64, bitmap []byte) error {
	return k.AttestationBitmap.Set(ctx, height, bitmap)
}

// GetAttestationBitmap retrieves the attestation bitmap for a height
func (k Keeper) GetAttestationBitmap(ctx sdk.Context, height int64) ([]byte, error) {
	bitmap, err := k.AttestationBitmap.Get(ctx, height)
	return bitmap, err
}

// SetEpochBitmap stores the epoch participation bitmap
func (k Keeper) SetEpochBitmap(ctx sdk.Context, epoch uint64, bitmap []byte) error {
	return k.EpochBitmap.Set(ctx, epoch, bitmap)
}

// GetEpochBitmap retrieves the epoch participation bitmap
func (k Keeper) GetEpochBitmap(ctx sdk.Context, epoch uint64) []byte {
	bitmap, err := k.EpochBitmap.Get(ctx, epoch)
	if err != nil {
		// Consider logging err or returning (nil, error)
		return nil
	}
	return bitmap
}

// IsInAttesterSet checks if a validator is in the attester set
func (k Keeper) IsInAttesterSet(ctx sdk.Context, addr string) (bool, error) {
	has, err := k.AttesterSet.Has(ctx, addr)
	return has, err
}

// SetAttesterSetMember adds a validator to the attester set
func (k Keeper) SetAttesterSetMember(ctx sdk.Context, addr string) error {
	return k.AttesterSet.Set(ctx, addr)
}

// RemoveAttesterSetMember removes a validator from the attester set
func (k Keeper) RemoveAttesterSetMember(ctx sdk.Context, addr string) error {
	return k.AttesterSet.Remove(ctx, addr)
}

// BuildValidatorIndexMap rebuilds the validator index mapping
func (k Keeper) BuildValidatorIndexMap(ctx sdk.Context) error {
	validators, err := k.stakingKeeper.GetAllValidators(ctx)
	if err != nil {
		return err
	}

	// Clear existing indices and powers
	// The `nil` range clears all entries in the collection.
	if err := k.ValidatorIndex.Clear(ctx, nil); err != nil {
		k.Logger(ctx).Error("failed to clear validator index", "error", err)
		return err
	}
	if err := k.ValidatorPower.Clear(ctx, nil); err != nil {
		k.Logger(ctx).Error("failed to clear validator power", "error", err)
		return err
	}

	// Build new indices for bonded validators
	index := uint16(0)
	for _, val := range validators {
		if val.IsBonded() {
			power := uint64(val.GetConsensusPower(sdk.DefaultPowerReduction))
			if err := k.SetValidatorIndex(ctx, val.OperatorAddress, index, power); err != nil {
				// Consider how to handle partial failures; potentially log and continue or return error.
				k.Logger(ctx).Error("failed to set validator index during build", "validator", val.OperatorAddress, "error", err)
				return err
			}
			index++
		}
	}
	return nil
}

// GetCurrentEpoch returns the current epoch number
func (k Keeper) GetCurrentEpoch(ctx sdk.Context) uint64 {
	return k.GetEpochForHeight(ctx, ctx.BlockHeight())
}

func (k Keeper) GetEpochForHeight(ctx sdk.Context, height int64) uint64 {
	params := k.GetParams(ctx)
	return uint64(height) / params.EpochLength
}

// IsCheckpointHeight checks if a height is a checkpoint
func (k Keeper) IsCheckpointHeight(ctx sdk.Context, height int64) bool {
	p, err := k.Params.Get(ctx)
	if err != nil {
		return false
	}
	params := p
	return uint64(height)%params.EpochLength == 0
}

// CalculateVotedPower calculates the total voted power from a bitmap
func (k Keeper) CalculateVotedPower(ctx sdk.Context, bitmap []byte) (uint64, error) {
	var votedPower uint64
	for i := 0; i < len(bitmap)*8; i++ {
		if k.bitmapHelper.IsSet(bitmap, i) {
			power, err := k.GetValidatorPower(ctx, uint16(i))
			if err != nil {
				return 0, fmt.Errorf("get validator power: %w", err)
			}

			votedPower += power
		}
	}
	return votedPower, nil
}

// GetTotalPower returns the total staking power
func (k Keeper) GetTotalPower(ctx sdk.Context) (uint64, error) {
	n, err := k.stakingKeeper.GetLastTotalPower(ctx)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// CheckQuorum checks if the voted power meets quorum
func (k Keeper) CheckQuorum(ctx sdk.Context, votedPower, totalPower uint64) (bool, error) {
	params := k.GetParams(ctx)
	quorumFrac, err := math.LegacyNewDecFromStr(params.QuorumFraction)
	if err != nil {
		return false, fmt.Errorf("invalid quorum fraction: %w", err)
	}

	requiredPower := math.LegacyNewDec(int64(totalPower)).Mul(quorumFrac).TruncateInt().Uint64()
	return votedPower >= requiredPower, nil
}

// IsSoftConfirmed checks if a block at a given height is soft-confirmed
// based on the attestation bitmap and quorum rules.
func (k Keeper) IsSoftConfirmed(ctx sdk.Context, height int64) (bool, error) {
	bitmap, err := k.GetAttestationBitmap(ctx, height)
	if err != nil {
		return false, err
	}
	if bitmap == nil {
		return false, nil // No bitmap, so cannot be soft-confirmed
	}
	votedPower, err := k.CalculateVotedPower(ctx, bitmap)
	if err != nil {
		return false, err
	}
	totalPower, err := k.GetTotalPower(ctx) // Assuming this gets the relevant total power for the height
	if err != nil {
		return false, err
	}

	return k.CheckQuorum(ctx, votedPower, totalPower)
}

// PruneOldBitmaps removes bitmaps older than PruneAfter epochs
func (k Keeper) PruneOldBitmaps(ctx sdk.Context, currentEpoch uint64) error {
	params := k.GetParams(ctx)
	if params.PruneAfter == 0 { // Avoid pruning if PruneAfter is zero or not set
		return nil
	}
	if currentEpoch <= params.PruneAfter {
		return nil
	}

	pruneBeforeEpoch := currentEpoch - params.PruneAfter
	pruneHeight := int64(pruneBeforeEpoch * params.EpochLength) // Assuming EpochLength defines blocks per epoch

	// Prune attestation bitmaps (raw bitmaps)
	attestationRange := new(collections.Range[int64]).StartInclusive(0).EndExclusive(pruneHeight)
	if err := k.AttestationBitmap.Clear(ctx, attestationRange); err != nil {
		return fmt.Errorf("clearing attestation bitmaps before height %d: %w", pruneHeight, err)
	}
	// Prune stored attestation info (full AttestationBitmap objects)
	storedAttestationInfoRange := new(collections.Range[int64]).StartInclusive(0).EndExclusive(pruneHeight)
	if err := k.StoredAttestationInfo.Clear(ctx, storedAttestationInfoRange); err != nil {
		return fmt.Errorf("clearing stored attestation info before height %d: %w", pruneHeight, err)
	}

	// Prune epoch bitmaps
	epochRange := new(collections.Range[uint64]).StartInclusive(0).EndExclusive(pruneBeforeEpoch)
	if err := k.EpochBitmap.Clear(ctx, epochRange); err != nil {
		return fmt.Errorf("clearing epoch bitmaps before epoch %d: %w", pruneBeforeEpoch, err)
	}

	// TODO: Consider pruning signatures associated with pruned heights.
	// This would involve iterating k.CommitsByHeight and removing entries where height < pruneHeight.

	k.Logger(ctx).Info("Pruned old bitmaps and attestation info", "prunedBeforeEpoch", pruneBeforeEpoch, "prunedBeforeHeight", pruneHeight)
	return nil
}

// SetSignature stores the vote signature for a given height and validator
func (k Keeper) SetSignature(ctx sdk.Context, height int64, validatorAddr string, consAddr []byte, timestamp time.Time, signature []byte) error {
	cmtSig := cmtproto.CommitSig{
		BlockIdFlag:      cmtproto.BlockIDFlagCommit,
		ValidatorAddress: consAddr,
		Timestamp:        timestamp,
		Signature:        signature,
	}
	return k.CommitSignatures.Set(ctx, collections.Join(height, validatorAddr), cmtSig)
}

// GetSignature retrieves the vote signature for a given height and validator
func (k Keeper) GetSignature(ctx sdk.Context, height int64, validatorAddr string) (cmtproto.CommitSig, error) {
	rsp, err := k.CommitSignatures.Get(ctx, collections.Join(height, validatorAddr))
	return rsp, err
}

// HasSignature checks if a signature exists for a given height and validator
func (k Keeper) HasSignature(ctx sdk.Context, height int64, validatorAddr string) (bool, error) {
	return k.CommitSignatures.Has(ctx, collections.Join(height, validatorAddr))
}

func (k Keeper) GetCommits(ctx sdk.Context, height int64) (*cmttypes.Commit, error) {
	header, data, err := k.blockSource.GetBlockData(ctx, uint64(height))
	if err != nil {
		return nil, fmt.Errorf("failed to get block at height %d: %w", height, err)
	}
	_ = data

	commit := &cmttypes.Commit{
		Height: height,
		Round:  0,
		BlockID: cmttypes.BlockID{
			Hash:          cmbytes.HexBytes(header.LastHeaderHash),
			PartSetHeader: cmttypes.PartSetHeader{Total: 0, Hash: nil},
		},
		Signatures: make([]cmttypes.CommitSig, 0),
	}
	// add the signatures (unordered) // todo (Alex): do we need them ordered?
	rng := collections.NewPrefixedPairRange[int64, string](height)
	err = k.CommitSignatures.Walk(ctx, rng, func(key collections.Pair[int64, string], value cmtproto.CommitSig) (bool, error) {
		var sig cmttypes.CommitSig
		if err := sig.FromProto(value); err != nil {
			return true, err
		}
		commit.Signatures = append(commit.Signatures, sig)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return commit, nil
}
