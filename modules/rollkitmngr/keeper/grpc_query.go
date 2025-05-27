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
// Note it is only a wrapper around the staking module's query (iff the attesters are enabled).
func (q queryServer) Attesters(context.Context, *types.QueryAttestersRequest) (*types.QueryAttestersResponse, error) {
	// err := q.NextSequencers.Walk(ctx, nil, func(key uint64, sequencer types.Sequencer) (bool, error) {
	// 	sequencers = append(sequencers, types.SequencerChanges{
	// 		BlockHeight: key,
	// 		Sequencers:  []types.Sequencer{sequencer},
	// 	})
	// 	return false, nil
	// })
	// if err != nil && !errors.Is(err, collections.ErrNotFound) {
	// 	return nil, err
	// }

	panic("unimplemented")
}

// IsMigrating implements types.QueryServer.
func (q queryServer) IsMigrating(context.Context, *types.QueryIsMigratingRequest) (*types.QueryIsMigratingResponse, error) {
	panic("unimplemented")
}

// Attesters returns the current attesters.
// Note it is only a wrapper around the staking module's query (iff the attesters are disabled). Otherwise it fetches from the module state.
func (q queryServer) Sequencer(ctx context.Context, _ *types.QuerySequencerRequest) (*types.QuerySequencerResponse, error) {
	vals, err := q.stakingKeeper.GetLastValidators(ctx)
	if err != nil {
		return nil, sdkerrors.ErrLogic.Wrapf("failed to get last validators")
	}

	// Eventually, refactor this when rollkit suppots multiple sequencers.
	// In the meantime, this is enough to determine if the chain is using rollkit or not.
	// There is one false positive for single validators chains, but those are more often local testnets.
	if len(vals) > 1 {
		// TODO
		// chain is using attesters, fallback to something else
	}

	return &types.QuerySequencerResponse{
		Sequencer: types.Sequencer{
			Name:            vals[0].GetMoniker(),
			ConsensusPubkey: vals[0].ConsensusPubkey,
		},
	}, nil
}
