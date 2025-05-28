package keeper

import (
	"context"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/rollkit/go-execution-abci/modules/rollkitmngr/types"
)

var _ types.MsgServer = &msgServer{}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of sequencer MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

func (m *msgServer) MigrateToRollkit(ctx context.Context, msg *types.MsgMigrateToRollkit) (*types.MsgMigrateToRollkitResponse, error) {
	if m.authority != msg.Authority {
		return nil, govtypes.ErrInvalidSigner.Wrapf("invalid authority; expected %s, got %s", m.authority, msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if msg.BlockHeight < uint64(sdkCtx.BlockHeight()) {
		return nil, sdkerrors.ErrInvalidRequest.Wrapf("block height %d must be greater than current block height %d", msg.BlockHeight, sdkCtx.BlockHeight())
	}

	s := types.RollkitMigration{
		BlockHeight: msg.BlockHeight,
		Sequencer:   msg.Sequencer,
		Attesters:   msg.Attesters,
	}

	if err := m.Migration.Set(ctx, s); err != nil {
		return nil, sdkerrors.ErrInvalidRequest.Wrapf("failed to set migration state: %v", err)
	}

	return &types.MsgMigrateToRollkitResponse{}, nil
}

// EditAttesters is a gov gated method that allows the authority to edit the list of attesters.
func (m *msgServer) EditAttesters(ctx context.Context, msg *types.MsgEditAttesters) (*types.MsgEditAttestersResponse, error) {
	if m.authority != msg.Authority {
		return nil, govtypes.ErrInvalidSigner.Wrapf("invalid authority; expected %s, got %s", m.authority, msg.Authority)
	}

	// TODO support adding attesters after the fact.

	attesters := make(map[string]struct{})
	for _, attester := range msg.Attesters {
		if attester.Name == "" || attester.ConsensusPubkey == nil {
			return nil, sdkerrors.ErrInvalidRequest.Wrap("attester name and consensus public key must not be empty")
		}

		if _, exists := attesters[attester.ConsensusPubkey.String()]; exists {
			return nil, sdkerrors.ErrInvalidRequest.Wrapf("duplicate attester consensus public key: %s", attester.ConsensusPubkey.String())
		}

		attesters[attester.ConsensusPubkey.String()] = struct{}{}

		consPub := attester.ConsensusPubkey.GetCachedValue().(cryptotypes.PubKey)
		val, err := m.stakingKeeper.GetValidatorByConsAddr(ctx, sdk.ConsAddress(consPub.Address()))
		if err != nil {
			return nil, sdkerrors.ErrInvalidRequest.Wrapf("failed to get validator by consensus address %s: %v", consPub.Address(), err)
		}
		if val.GetMoniker() == "" {
			return nil, sdkerrors.ErrInvalidRequest.Wrapf("no validator found for consensus public key %s", attester.ConsensusPubkey.String())
		}
	}

	// update the validators

	return &types.MsgEditAttestersResponse{}, nil
}
