package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/evstack/ev-abci/modules/rollkitmngr/types"
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
