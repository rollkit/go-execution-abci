package keeper

import (
	"bytes"
	"context"
	"cosmossdk.io/collections"
	"fmt"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/errors"
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

	if err := k.validateAttestation(ctx, msg); err != nil {
		return nil, err
	}
	votedEpoch := k.GetEpochForHeight(ctx, msg.Height)
	if k.GetCurrentEpoch(ctx) != votedEpoch+1 { // can vote only for last epoch
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "validator %s not voted for epoch %d", msg.Validator, votedEpoch)
	}

	valIndexPos, found := k.GetValidatorIndex(ctx, msg.Validator)
	if !found {
		return nil, errors.Wrapf(sdkerrors.ErrNotFound, "validator index not found for %s", msg.Validator)
	}

	if err := k.updateAttestationBitmap(ctx, msg, valIndexPos); err != nil {
		return nil, errors.Wrap(err, "update attestation bitmap")
	}

	vote, err := k.verifyVote(ctx, msg)
	if err != nil {
		return nil, err
	}

	if err := k.SetSignature(ctx, msg.Height, msg.Validator, vote.Signature); err != nil {
		return nil, errors.Wrap(err, "store signature")
	}

	if err := k.updateEpochBitmap(ctx, votedEpoch, valIndexPos); err != nil {
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
		return errors.Wrap(err, "set epoch bitmap")
	}
	return nil
}

// validateAttestation validates the attestation request
func (k msgServer) validateAttestation(ctx sdk.Context, msg *types.MsgAttest) error {
	if !k.IsCheckpointHeight(ctx, msg.Height) {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "height %d is not a checkpoint", msg.Height)
	}

	has, err := k.IsInAttesterSet(ctx, msg.Validator)
	if err != nil {
		return errors.Wrapf(err, "in attester set")
	}
	if !has {
		return errors.Wrapf(sdkerrors.ErrUnauthorized, "validator %s not in attester set", msg.Validator)
	}
	return nil
}

// updateAttestationBitmap handles bitmap operations for attestation
func (k msgServer) updateAttestationBitmap(ctx sdk.Context, msg *types.MsgAttest, index uint16) error {
	bitmap, err := k.GetAttestationBitmap(ctx, msg.Height)
	if err != nil && !errors.IsOf(err, collections.ErrNotFound) {
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
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "validator %s already attested for height %d", msg.Validator, msg.Height)
	}

	k.bitmapHelper.SetBit(bitmap, int(index))

	if err := k.SetAttestationBitmap(ctx, msg.Height, bitmap); err != nil {
		return errors.Wrap(err, "set attestation bitmap")
	}
	return nil
}

// verifyVote verifies the vote signature and block hash
func (k msgServer) verifyVote(ctx sdk.Context, msg *types.MsgAttest) (*cmtproto.Vote, error) {
	var vote cmtproto.Vote
	if err := proto.Unmarshal(msg.Vote, &vote); err != nil {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "unmarshal vote: %s", err)
	}

	header, _, err := k.blockSource.GetBlockData(ctx, uint64(msg.Height))
	if err != nil {
		return nil, errors.Wrapf(err, "block data for height %d", msg.Height)
	}
	if header == nil {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "block header not found for height %d", msg.Height)
	}

	if !bytes.Equal(vote.BlockID.Hash, header.Header.AppHash) {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "vote block ID hash does not match app hash for height %d", msg.Height)
	}

	validator, err := k.stakingKeeper.GetValidator(ctx, vote.ValidatorAddress)
	if err != nil {
		return nil, errors.Wrapf(err, "get validator")
	}

	pubKey, err := validator.ConsPubKey()
	if err != nil {
		return nil, errors.Wrapf(err, "pubkey")
	}

	voteSignBytes := cmttypes.VoteSignBytes(header.Header.ChainID(), &vote)
	if !pubKey.VerifySignature(voteSignBytes, vote.Signature) {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid vote signature")
	}

	return &vote, nil
}

// JoinAttesterSet handles MsgJoinAttesterSet
func (k msgServer) JoinAttesterSet(goCtx context.Context, msg *types.MsgJoinAttesterSet) (*types.MsgJoinAttesterSetResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	valAddr, err := sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator address: %s", err)
	}

	validator, err := k.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return nil, err
	}

	if !validator.IsBonded() {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "validator must be bonded to join attester set")
	}
	has, err := k.IsInAttesterSet(ctx, msg.Validator)
	if err != nil {
		return nil, errors.Wrapf(err, "in attester set")
	}
	if has {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "validator already in attester set")
	}

	// TODO (Alex): the valset should be updated at the end of an epoch only
	if err := k.SetAttesterSetMember(ctx, msg.Validator); err != nil {
		return nil, errors.Wrap(err, "set attester set member")
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
		return nil, errors.Wrapf(err, "in attester set")
	}
	if !has {
		return nil, errors.Wrapf(sdkerrors.ErrInvalidRequest, "validator not in attester set")
	}

	// TODO (Alex): the valset should be updated at the end of an epoch only
	if err := k.RemoveAttesterSetMember(ctx, msg.Validator); err != nil {
		return nil, errors.Wrap(err, "remove attester set member")
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
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", k.GetAuthority(), msg.Authority)
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
