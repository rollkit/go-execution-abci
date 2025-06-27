package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdkerr "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

// Attest handles MsgAttest
func (k msgServer) Attest(goCtx context.Context, msg *types.MsgAttest) (*types.MsgAttestResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.GetParams(ctx).SignMode == types.SignMode_SIGN_MODE_CHECKPOINT &&
		!k.IsCheckpointHeight(ctx, msg.Height) {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "height %d is not a checkpoint", msg.Height)
	}
	has, err := k.IsInAttesterSet(ctx, msg.Validator)
	if err != nil {
		return nil, sdkerr.Wrapf(err, "in attester set")
	}
	if !has {
		return nil, sdkerr.Wrapf(sdkerrors.ErrUnauthorized, "validator %s not in attester set", msg.Validator)
	}

	index, found := k.GetValidatorIndex(ctx, msg.Validator)
	if !found {
		return nil, sdkerr.Wrapf(sdkerrors.ErrNotFound, "validator index not found for %s", msg.Validator)
	}

	// todo (Alex): we need to set a limit to not have validators attest old blocks. Also make sure that this relates with
	// the retention period for pruning
	bitmap, err := k.GetAttestationBitmap(ctx, msg.Height)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, sdkerr.Wrap(err, "get attestation bitmap")
	}
	if bitmap == nil {
		validators, err := k.stakingKeeper.GetLastValidators(ctx)
		if err != nil {
			return nil, err
		}
		numValidators := 0
		for _, v := range validators {
			if v.IsBonded() {
				numValidators++
			}
		}
		bitmap = k.bitmapHelper.NewBitmap(numValidators)
	}

	if k.bitmapHelper.IsSet(bitmap, int(index)) {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "validator %s already attested for height %d", msg.Validator, msg.Height)
	}

	// TODO: Verify the vote signature here once we implement vote parsing

	// Set the bit
	k.bitmapHelper.SetBit(bitmap, int(index))
	if err := k.SetAttestationBitmap(ctx, msg.Height, bitmap); err != nil {
		return nil, sdkerr.Wrap(err, "set attestation bitmap")
	}

	// Store signature using the new collection method
	if err := k.SetSignature(ctx, msg.Height, msg.Validator, msg.Vote); err != nil {
		return nil, sdkerr.Wrap(err, "store signature")
	}

	epoch := k.GetCurrentEpoch(ctx)
	epochBitmap := k.GetEpochBitmap(ctx, epoch)
	if epochBitmap == nil {
		validators, err := k.stakingKeeper.GetLastValidators(ctx)
		if err != nil {
			return nil, err
		}
		numValidators := 0
		for _, v := range validators {
			if v.IsBonded() {
				numValidators++
			}
		}
		epochBitmap = k.bitmapHelper.NewBitmap(numValidators)
	}
	k.bitmapHelper.SetBit(epochBitmap, int(index))
	if err := k.SetEpochBitmap(ctx, epoch, epochBitmap); err != nil {
		return nil, sdkerr.Wrap(err, "set epoch bitmap")
	}

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.TypeMsgAttest,
			sdk.NewAttribute("validator", msg.Validator),
			sdk.NewAttribute("height", math.NewInt(msg.Height).String()),
		),
	)

	return &types.MsgAttestResponse{}, nil
}

// JoinAttesterSet handles MsgJoinAttesterSet
func (k msgServer) JoinAttesterSet(goCtx context.Context, msg *types.MsgJoinAttesterSet) (*types.MsgJoinAttesterSetResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	valAddr, err := sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator address: %s", err)
	}

	validator, err := k.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return nil, err
	}

	if !validator.IsBonded() {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "validator must be bonded to join attester set")
	}
	has, err := k.IsInAttesterSet(ctx, msg.Validator)
	if err != nil {
		return nil, sdkerr.Wrapf(err, "in attester set")
	}
	if has {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "validator already in attester set")
	}

	// TODO (Alex): the valset should be updated at the end of an epoch only
	if err := k.SetAttesterSetMember(ctx, msg.Validator); err != nil {
		return nil, sdkerr.Wrap(err, "set attester set member")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.TypeMsgJoinAttesterSet,
			sdk.NewAttribute("validator", msg.Validator),
		),
	)
	k.Logger(ctx).Info("+++ joined attester set", "validator", msg.Validator)
	return &types.MsgJoinAttesterSetResponse{}, nil
}

// LeaveAttesterSet handles MsgLeaveAttesterSet
func (k msgServer) LeaveAttesterSet(goCtx context.Context, msg *types.MsgLeaveAttesterSet) (*types.MsgLeaveAttesterSetResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	has, err := k.IsInAttesterSet(ctx, msg.Validator)
	if err != nil {
		return nil, sdkerr.Wrapf(err, "in attester set")
	}
	if !has {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "validator not in attester set")
	}

	// TODO (Alex): the valset should be updated at the end of an epoch only
	if err := k.RemoveAttesterSetMember(ctx, msg.Validator); err != nil {
		return nil, sdkerr.Wrap(err, "remove attester set member")
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.TypeMsgLeaveAttesterSet,
			sdk.NewAttribute("validator", msg.Validator),
		),
	)

	return &types.MsgLeaveAttesterSetResponse{}, nil
}

// UpdateParams handles MsgUpdateParams
func (k msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if k.GetAuthority() != msg.Authority {
		return nil, sdkerr.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", k.GetAuthority(), msg.Authority)
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	if err := k.SetParams(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("set params: %w", err)
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.TypeMsgUpdateParams,
			sdk.NewAttribute("authority", msg.Authority),
		),
	)

	return &types.MsgUpdateParamsResponse{}, nil
}
