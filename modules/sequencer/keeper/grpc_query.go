package keeper

import (
	"context"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	Keeper
}

// NewQueryServer creates a new instance of the sequencer query server.
func NewQueryServer(keeper Keeper) types.QueryServer {
	return queryServer{Keeper: keeper}
}

// Sequencers returns the current sequencers.
// Note it is only a wrapper around the staking module's query.
func (q queryServer) Sequencers(ctx context.Context, _ *types.QuerySequencersRequest) (*types.QuerySequencersResponse, error) {
	vals, err := q.stakingKeeper.GetLastValidators(ctx)
	if err != nil {
		return nil, sdkerrors.ErrLogic.Wrapf("failed to get last validators")
	}

	// Eventually, refactor this when rollkit suppots multiple sequencers.
	// In the meantime, this is enough to determine if the chain is using rollkit or not.
	// There is one false positive for single validators chains, but those are more often local testnets.
	if len(vals) > 1 {
		return nil, sdkerrors.ErrLogic.Wrapf("chain is currently not using Rollkit")
	}

	sequencers := make([]types.Sequencer, len(vals))
	for i, val := range vals {
		sequencers[i] = types.Sequencer{
			Name:            val.GetMoniker(),
			ConsensusPubkey: val.ConsensusPubkey,
		}
	}

	return &types.QuerySequencersResponse{
		Sequencers: sequencers,
	}, nil
}

// SequencersChanges returns the planned sequencers changes.
func (q queryServer) SequencersChanges(ctx context.Context, _ *types.QuerySequencersChangesRequest) (*types.QuerySequencersChangesResponse, error) {
	var sequencers []types.SequencerChanges
	err := q.NextSequencers.Walk(ctx, nil, func(key uint64, sequencer types.Sequencer) (bool, error) {
		sequencers = append(sequencers, types.SequencerChanges{
			BlockHeight: key,
			Sequencers:  []types.Sequencer{sequencer},
		})
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QuerySequencersChangesResponse{
		SequencerChanges: sequencers,
	}, nil
}
