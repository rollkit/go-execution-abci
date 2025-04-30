package adapter

import (
	"context"
	"fmt"

	cmtstateproto "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtstate "github.com/cometbft/cometbft/state"
	proto "github.com/cosmos/gogoproto/proto"
	ds "github.com/ipfs/go-datastore"
	kt "github.com/ipfs/go-datastore/keytransform"
)

const (
	// KeyPrefix is the prefix used for all ABCI-related keys in the datastore
	KeyPrefix = "abci"
	// stateKey is the key used for storing state
	stateKey = "s"
)

// NewPrefixedStore creates a new datastore with the ABCI prefix
func NewPrefixedStore(store ds.Batching) ds.Batching {
	return kt.Wrap(store, &kt.PrefixTransform{
		Prefix: ds.NewKey(KeyPrefix),
	})
}

// loadState loads the state from disk
func loadState(ctx context.Context, s ds.Batching) (*cmtstate.State, error) {
	data, err := s.Get(ctx, ds.NewKey(stateKey))
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
func saveState(ctx context.Context, s ds.Batching, state *cmtstate.State) error {
	stateProto, err := state.ToProto()
	if err != nil {
		return fmt.Errorf("failed to convert state to proto: %w", err)
	}

	data, err := proto.Marshal(stateProto)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return s.Put(ctx, ds.NewKey(stateKey), data)
}
