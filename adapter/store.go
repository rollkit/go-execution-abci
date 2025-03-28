package adapter

import (
	"context"
	"fmt"

	cmtstateproto "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtstate "github.com/cometbft/cometbft/state"
	cmtypes "github.com/cometbft/cometbft/types"
	proto "github.com/cosmos/gogoproto/proto"
	"github.com/rollkit/rollkit/pkg/store"
)

const stateKey = "abci-s"

// State contains information about current state of the blockchain.
type State struct {
	ConsensusParams                  cmtypes.ConsensusParams
	LastHeightConsensusParamsChanged int64
	Validators                       *cmtypes.ValidatorSet
	NextValidators                   *cmtypes.ValidatorSet
	LastValidators                   *cmtypes.ValidatorSet
	LastHeightValidatorsChanged      int64
}

// loadState loads the state from disk
func loadState(ctx context.Context, s store.Store) (*cmtstate.State, error) {
	data, err := s.GetMetadata(ctx, stateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get state metadata: %w", err)
	}
	if data == nil {
		return &cmtstate.State{}, nil
	}

	stateProto := &cmtstateproto.State{}
	if err := proto.Unmarshal(data, stateProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return cmtstate.FromProto(stateProto)
}

// saveState saves the state to disk
func saveState(ctx context.Context, s store.Store, state *cmtstate.State) error {
	stateProto, err := state.ToProto()
	if err != nil {
		return fmt.Errorf("failed to convert state to proto: %w", err)
	}

	data, err := proto.Marshal(stateProto)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return s.SetMetadata(ctx, stateKey, data)
}
