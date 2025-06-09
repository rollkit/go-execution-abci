package keeper

import (
	"crypto/sha256"
	"encoding/base64"

	// For error wrapping if needed
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

// BeginBlocker handles begin block logic for the network module
func (k Keeper) BeginBlocker(ctx sdk.Context) {
	params := k.GetParams(ctx)

	// Only process if sign mode is IBC_ONLY and we have outbound IBC packets
	if params.SignMode == types.SignMode_SIGN_MODE_IBC_ONLY {
		// TODO: Check for outbound IBC packets
		// For now, this is a placeholder
	}
}

// EndBlocker handles end block logic for the network module
func (k Keeper) EndBlocker(ctx sdk.Context) {
	height := ctx.BlockHeight()
	params := k.GetParams(ctx)

	// Handle checkpoint heights
	if k.IsCheckpointHeight(ctx, height) {
		k.processCheckpoint(ctx, height)
	}

	// Handle epoch end
	epoch := k.GetCurrentEpoch(ctx)
	nextHeight := height + 1
	nextEpoch := uint64(nextHeight) / params.EpochLength

	if epoch != nextEpoch {
		k.processEpochEnd(ctx, epoch)
	}
}

// processCheckpoint handles checkpoint processing
func (k Keeper) processCheckpoint(ctx sdk.Context, height int64) {
	bitmapBytes := k.GetAttestationBitmap(ctx, height)
	if bitmapBytes == nil {
		return
	}

	votedPower := k.CalculateVotedPower(ctx, bitmapBytes)
	totalPower := k.GetTotalPower(ctx)

	validatorHash := sha256.Sum256(bitmapBytes)

	commitHash := sha256.Sum256([]byte("placeholder"))

	softConfirmed := k.CheckQuorum(ctx, votedPower, totalPower)

	attestationInfoToStore := types.AttestationBitmap{
		Height:        height,
		Bitmap:        bitmapBytes,
		VotedPower:    votedPower,
		TotalPower:    totalPower,
		SoftConfirmed: softConfirmed,
	}

	if err := k.StoredAttestationInfo.Set(ctx, height, attestationInfoToStore); err != nil {
		k.Logger(ctx).Error("failed to store attestation info", "height", height, "error", err)
	}

	// Emit hashes
	k.emitCheckpointHashes(ctx, height, validatorHash[:], commitHash[:], softConfirmed)
}

func (k Keeper) processEpochEnd(ctx sdk.Context, epoch uint64) {
	params := k.GetParams(ctx)
	epochBitmap := k.GetEpochBitmap(ctx, epoch)

	if epochBitmap != nil {
		validators := k.stakingKeeper.GetLastValidators(ctx)
		totalBondedValidators := 0
		for _, v := range validators {
			if v.IsBonded() {
				totalBondedValidators++
			}
		}

		if totalBondedValidators > 0 {
			participated := k.bitmapHelper.PopCount(epochBitmap)
			minParticipation, err := math.LegacyNewDecFromStr(params.MinParticipation)
			if err != nil {
				k.Logger(ctx).Error("failed to parse MinParticipation", "error", err)
			} else {
				participationRate := math.LegacyNewDec(int64(participated)).QuoInt64(int64(totalBondedValidators))
				if participationRate.LT(minParticipation) {
					k.ejectLowParticipants(ctx, epochBitmap)
				}
			}
		}
	}

	if !params.EmergencyMode {
		epochStartHeight := int64(epoch * params.EpochLength)
		checkpointsInEpoch := 0
		softConfirmedCheckpoints := 0

		for h := epochStartHeight; h < epochStartHeight+int64(params.EpochLength); h++ {
			if h > ctx.BlockHeight() {
				break
			}
			if k.IsCheckpointHeight(ctx, h) {
				checkpointsInEpoch++
				if k.IsSoftConfirmed(ctx, h) {
					softConfirmedCheckpoints++
				}
			}
		}

		if checkpointsInEpoch > 0 && softConfirmedCheckpoints == 0 {
			panic("Network module: No checkpoints achieved quorum in epoch")
		}
	}

	if err := k.PruneOldBitmaps(ctx, epoch); err != nil {
		k.Logger(ctx).Error("failed to prune old data at epoch end", "epoch", epoch, "error", err)
	}

	if err := k.BuildValidatorIndexMap(ctx); err != nil {
		k.Logger(ctx).Error("failed to rebuild validator index map at epoch end", "epoch", epoch, "error", err)
	}
}

// ejectLowParticipants ejects validators with low participation
func (k Keeper) ejectLowParticipants(ctx sdk.Context, epochBitmap []byte) {
	// TODO: Implement validator ejection logic
	k.Logger(ctx).Info("Low participation detected, ejection logic not yet implemented")
}

// emitCheckpointHashes emits checkpoint hashes
func (k Keeper) emitCheckpointHashes(ctx sdk.Context, height int64, validatorHash, commitHash []byte, softConfirmed bool) {
	var softConfirmedSt string

	if softConfirmed {
		softConfirmedSt = "true"
	} else {
		softConfirmedSt = "false"
	}
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"checkpoint",
			sdk.NewAttribute("height", math.NewInt(height).String()),
			sdk.NewAttribute("validator_hash", base64.StdEncoding.EncodeToString(validatorHash)),
			sdk.NewAttribute("commit_hash", base64.StdEncoding.EncodeToString(commitHash)),
			sdk.NewAttribute("soft_confirmed", softConfirmedSt),
		),
	)
}

func (k Keeper) emitZeroHashes(ctx sdk.Context, height int64) {
	zeroHash := make([]byte, 32)
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"checkpoint",
			sdk.NewAttribute("height", math.NewInt(height).String()),
			sdk.NewAttribute("validator_hash", base64.StdEncoding.EncodeToString(zeroHash)),
			sdk.NewAttribute("commit_hash", base64.StdEncoding.EncodeToString(zeroHash)),
			sdk.NewAttribute("soft_confirmed", "false"),
		),
	)
}

func (k Keeper) AfterValidatorSetUpdates(ctx sdk.Context) {
	k.BuildValidatorIndexMap(ctx)
}
