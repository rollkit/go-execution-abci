package keeper

import (
	"context"

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

// SequencersChanges returns the planned sequencers changes.
func (q queryServer) SequencersChanges(ctx context.Context, _ *types.QuerySequencerChangesRequest) (*types.QuerySequencerChangesResponse, error) {
	var sequencers []types.SequencerChanges
	err := q.NextSequencer.Walk(ctx, nil, func(key uint64, sequencer types.Sequencer) (bool, error) {
		sequencers = append(sequencers, types.SequencerChanges{
			BlockHeight: key,
			Sequencers:  []types.Sequencer{sequencer},
		})
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QuerySequencerChangesResponse{
		SequencerChanges: sequencers,
	}, nil
}
