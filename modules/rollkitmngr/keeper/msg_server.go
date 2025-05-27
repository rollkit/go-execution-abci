package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/rollkit/go-execution-abci/modules/rollkitmngr/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of sequencer MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

func (k msgServer) MigrateToRollkit(ctx context.Context, msg *types.MsgRollkitMigrate) (*types.MsgMigrateToRollkitResponse, error) {
	if k.authority != msg.Authority {
		return nil, govtypes.ErrInvalidSigner.Wrapf("invalid authority; expected %s, got %s", k.authority, msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if msg.BlockHeight < uint64(sdkCtx.BlockHeight()) {
		return nil, sdkerrors.ErrInvalidRequest.Wrapf("block height %d must be greater than current block height %d", msg.BlockHeight, sdkCtx.BlockHeight())
	}

	// TODO: set attesters

	if err := k.NextSequencers.Set(ctx, msg.BlockHeight, msg.Sequencer); err != nil {
		return nil, err
	}

	return &types.MsgMigrateToRollkitResponse{}, nil
}
