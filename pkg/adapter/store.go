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
	// keyPrefix is the prefix used for all ABCI-related keys in the datastore
	keyPrefix = "abci"
	// stateKey is the key used for storing state
	stateKey = "s"
)

// Store wraps a datastore with ABCI-specific functionality
type Store struct {
	prefixedStore ds.Batching
}

// NewStore creates a new Store with the ABCI prefix
func NewStore(store ds.Batching) *Store {
	return &Store{
		prefixedStore: kt.Wrap(store, &kt.PrefixTransform{
			Prefix: ds.NewKey(keyPrefix),
		}),
	}
}

// LoadState loads the state from disk
func (s *Store) LoadState(ctx context.Context) (*cmtstate.State, error) {
	data, err := s.prefixedStore.Get(ctx, ds.NewKey(stateKey))
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

// SaveState saves the state to disk
func (s *Store) SaveState(ctx context.Context, state *cmtstate.State) error {
	stateProto, err := state.ToProto()
	if err != nil {
		return fmt.Errorf("failed to convert state to proto: %w", err)
	}

	data, err := proto.Marshal(stateProto)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return s.prefixedStore.Put(ctx, ds.NewKey(stateKey), data)
}
