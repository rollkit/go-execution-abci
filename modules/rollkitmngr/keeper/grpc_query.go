package keeper

import (
	"context"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/rollkit/go-execution-abci/modules/rollkitmngr/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	Keeper
}

// NewQueryServer creates a new instance of the sequencer query server.
func NewQueryServer(keeper Keeper) types.QueryServer {
	return queryServer{Keeper: keeper}
}

// Attesters returns the current attesters.
// Note it is only a wrapper around the staking module's query.
// If the sequencer and attesters are equals, then the chain is not using attesters.
func (q queryServer) Attesters(ctx context.Context, _ *types.QueryAttestersRequest) (*types.QueryAttestersResponse, error) {
	vals, err := q.stakingKeeper.GetLastValidators(ctx)
	if err != nil {
		return nil, sdkerrors.ErrLogic.Wrapf("failed to get last validators: %v", err)
	}

	attesters := make([]types.Attester, len(vals))
	for i, val := range vals {
		attesters[i] = types.Attester{
			Name:            val.GetMoniker(),
			ConsensusPubkey: val.ConsensusPubkey,
		}
	}

	return &types.QueryAttestersResponse{
		Attesters: attesters,
	}, nil
}

// IsMigrating checks if the migration to Rollkit is in progress.
func (q queryServer) IsMigrating(ctx context.Context, _ *types.QueryIsMigratingRequest) (*types.QueryIsMigratingResponse, error) {
	start, end, isMigrating := q.Keeper.IsMigrating(ctx)

	return &types.QueryIsMigratingResponse{
		IsMigrating:      isMigrating,
		StartBlockHeight: start,
		EndBlockHeight:   end,
	}, nil
}

// Sequencer returns the current Sequencer.
func (q queryServer) Sequencer(ctx context.Context, _ *types.QuerySequencerRequest) (*types.QuerySequencerResponse, error) {
	_, _, migrationInProgress := q.Keeper.IsMigrating(ctx)
	if migrationInProgress {
		return nil, sdkerrors.ErrLogic.Wrap("sequencer is not set, migration is in progress or not started yet")
	}

	seq, err := q.Keeper.Sequencer.Get(ctx)
	if err != nil {
		return nil, sdkerrors.ErrLogic.Wrapf("failed to get sequencer: %v", err)
	}

	return &types.QuerySequencerResponse{
		Sequencer: seq,
	}, nil
}
