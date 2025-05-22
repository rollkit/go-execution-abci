package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of sequencer MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

func (k msgServer) ChangeSequencers(ctx context.Context, msg *types.MsgChangeSequencers) (*types.MsgChangeSequencersResponse, error) {
	if k.authority != msg.Authority {
		return nil, govtypes.ErrInvalidSigner.Wrapf("invalid authority; expected %s, got %s", k.authority, msg.Authority)
	}

	if len(msg.Sequencers) == 0 {
		return nil, sdkerrors.ErrNotSupported.Wrapf("one sequencer is required.")
	}

	if len(msg.Sequencers) > 1 {
		return nil, sdkerrors.ErrNotSupported.Wrapf("currently only one sequencer can be set at a time")
	}

	newSequencer := msg.Sequencers[0]
	err := k.Sequencer.Set(ctx, newSequencer)
	if err != nil {
		return nil, err
	}

	if err = k.NextSequencerChangeHeight.Set(ctx, msg.BlockHeight); err != nil {
		return nil, err
	}

	return &types.MsgChangeSequencersResponse{}, nil
}

func (k msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if k.authority != msg.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", k.authority, msg.Authority)
	}

	if err := k.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
