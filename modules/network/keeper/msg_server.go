package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdkerr "cosmossdk.io/errors"
	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/gogoproto/proto"

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

	if err := k.validateAttestation(ctx, msg); err != nil {
		return nil, err
	}
	// can vote only for the last epoch
	if delta := ctx.BlockHeight() - msg.Height; delta < 0 || delta > int64(k.GetParams(ctx).EpochLength) {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "exceeded voting window: %d blocks", delta)
	}

	valIndexPos, found := k.GetValidatorIndex(ctx, msg.Validator)
	if !found {
		return nil, sdkerr.Wrapf(sdkerrors.ErrNotFound, "validator index not found for %s", msg.Validator)
	}

	vote, err := k.verifyVote(ctx, msg)
	if err != nil {
		return nil, err
	}

	if err := k.updateAttestationBitmap(ctx, msg, valIndexPos); err != nil {
		return nil, sdkerr.Wrap(err, "update attestation bitmap")
	}

	if err := k.SetSignature(ctx, msg.Height, msg.Validator, vote.Signature); err != nil {
		return nil, sdkerr.Wrap(err, "store signature")
	}

	if err := k.updateEpochBitmap(ctx, uint64(msg.Height), valIndexPos); err != nil {
		return nil, err
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

func (k msgServer) updateEpochBitmap(ctx sdk.Context, votedEpoch uint64, index uint16) error {
	epochBitmap := k.GetEpochBitmap(ctx, votedEpoch)
	if epochBitmap == nil {
		validators, err := k.stakingKeeper.GetLastValidators(ctx)
		if err != nil {
			return err
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
	if err := k.SetEpochBitmap(ctx, votedEpoch, epochBitmap); err != nil {
		return sdkerr.Wrap(err, "set epoch bitmap")
	}
	return nil
}

// validateAttestation validates the attestation request
func (k msgServer) validateAttestation(ctx sdk.Context, msg *types.MsgAttest) error {
	if k.GetParams(ctx).SignMode == types.SignMode_SIGN_MODE_CHECKPOINT &&
		!k.IsCheckpointHeight(ctx, msg.Height) {
		return sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "height %d is not a checkpoint", msg.Height)
	}

	has, err := k.IsInAttesterSet(ctx, msg.Validator)
	if err != nil {
		return sdkerr.Wrapf(err, "in attester set")
	}
	if !has {
		return sdkerr.Wrapf(sdkerrors.ErrUnauthorized, "validator %s not in attester set", msg.Validator)
	}
	return nil
}

// updateAttestationBitmap handles bitmap operations for attestation
func (k msgServer) updateAttestationBitmap(ctx sdk.Context, msg *types.MsgAttest, index uint16) error {
	bitmap, err := k.GetAttestationBitmap(ctx, msg.Height)
	if err != nil && !sdkerr.IsOf(err, collections.ErrNotFound) {
		return err
	}

	if bitmap == nil {
		validators, err := k.stakingKeeper.GetLastValidators(ctx)
		if err != nil {
			return err
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
		return sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "validator %s already attested for height %d", msg.Validator, msg.Height)
	}

	k.bitmapHelper.SetBit(bitmap, int(index))

	if err := k.SetAttestationBitmap(ctx, msg.Height, bitmap); err != nil {
		return sdkerr.Wrap(err, "set attestation bitmap")
	}
	return nil
}

// verifyVote verifies the vote signature and block hash
func (k msgServer) verifyVote(ctx sdk.Context, msg *types.MsgAttest) (*cmtproto.Vote, error) {
	var vote cmtproto.Vote
	if err := proto.Unmarshal(msg.Vote, &vote); err != nil {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "unmarshal vote: %s", err)
	}
	if msg.Height != vote.Height {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "vote height does not match attestation height")
	}
	if senderAddr, err := sdk.ValAddressFromBech32(msg.Validator); err != nil {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "invalid validator address: %s", err)
	} else if !bytes.Equal(senderAddr, vote.ValidatorAddress) {
		return nil, sdkerr.Wrapf(sdkerrors.ErrUnauthorized, "vote validator address does not match attestation validator address")
	}
	if len(vote.Signature) == 0 {
		return nil, sdkerrors.ErrInvalidRequest.Wrap("empty signature")
	}

	// todo (Alex): validate app hash match, vote clock drift

	validator, err := k.stakingKeeper.GetValidator(ctx, vote.ValidatorAddress)
	if err != nil {
		return nil, sdkerr.Wrapf(err, "get validator")
	}
	pubKey, err := validator.ConsPubKey()
	if err != nil {
		return nil, sdkerr.Wrapf(err, "pubkey")
	}
	if !bytes.Equal(pubKey.Address(), vote.ValidatorAddress) {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "pubkey address does not match validator address")
	}
	voteSignBytes := cmttypes.VoteSignBytes(ctx.ChainID(), &vote)
	if !pubKey.VerifySignature(voteSignBytes, vote.Signature) {
		return nil, sdkerr.Wrapf(sdkerrors.ErrInvalidRequest, "invalid vote signature")
	}

	return &vote, nil
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
