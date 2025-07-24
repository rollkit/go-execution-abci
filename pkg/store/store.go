package store

import (
	"context"
	"fmt"
	"strconv"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtstateproto "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtstate "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"
	proto "github.com/cosmos/gogoproto/proto"
	ds "github.com/ipfs/go-datastore"
	kt "github.com/ipfs/go-datastore/keytransform"
)

const (
	// keyPrefix is the prefix used for all ABCI-related keys in the datastore
	keyPrefix = "abci"
	// stateKey is the key used for storing state
	stateKey = "s"
	// blockResponseKey is the key used for storing block responses
	blockResponseKey = "br"
	// blockIDKey is the key used for storing block IDs
	blockIDKey = "bid"
)

// Store wraps a datastore with ABCI-specific functionality
type Store struct {
	prefixedStore ds.Batching
}

// NewExecABCIStore creates a new Store with the ABCI prefix.
// The data is stored under rollkit database and not in the app's database.
func NewExecABCIStore(store ds.Batching) *Store {
	return &Store{
		prefixedStore: kt.Wrap(store, &kt.PrefixTransform{
			Prefix: ds.NewKey(keyPrefix),
		}),
	}
}

// LoadState loads the state from disk.
// When the state does not exist, it returns an empty state.
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

// SaveBlockID saves the block ID to disk per height.
// This is used to store the block ID for the block execution
func (s *Store) SaveBlockID(ctx context.Context, height uint64, blockID *cmttypes.BlockID) error {
	blockIDProto := blockID.ToProto()
	data, err := proto.Marshal(&blockIDProto)
	if err != nil {
		return fmt.Errorf("failed to marshal block ID: %w", err)
	}

	key := ds.NewKey(blockIDKey).ChildString(strconv.FormatUint(height, 10))
	return s.prefixedStore.Put(ctx, key, data)
}

// GetBlockID loads the block ID from disk for a specific height.
func (s *Store) GetBlockID(ctx context.Context, height uint64) (*cmttypes.BlockID, error) {
	key := ds.NewKey(blockIDKey).ChildString(strconv.FormatUint(height, 10))
	data, err := s.prefixedStore.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get block ID %d: %w", height, err)
	}

	if data == nil {
		return nil, fmt.Errorf("block ID not found for height %d", height)
	}

	protoBlockID := &cmtproto.BlockID{}
	if err := proto.Unmarshal(data, protoBlockID); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block ID: %w", err)
	}

	blockID, err := cmttypes.BlockIDFromProto(protoBlockID)
	if err != nil {
		return nil, fmt.Errorf("failed to convert block ID from proto: %w", err)
	}

	return blockID, nil
}

// SaveBlockResponse saves the block response to disk per height
// This is used to store the results of the block execution
// so that they can be retrieved later, e.g., for querying transaction results.
func (s *Store) SaveBlockResponse(ctx context.Context, height uint64, resp *abci.ResponseFinalizeBlock) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal block response: %w", err)
	}

	key := ds.NewKey(blockResponseKey).ChildString(strconv.FormatUint(height, 10))
	return s.prefixedStore.Put(ctx, key, data)
}

// GetBlockResponse loads the block response from disk for a specific height
// If the block response does not exist, it returns an error.
func (s *Store) GetBlockResponse(ctx context.Context, height uint64) (*abci.ResponseFinalizeBlock, error) {
	key := ds.NewKey(blockResponseKey).ChildString(strconv.FormatUint(height, 10))
	data, err := s.prefixedStore.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get block response: %w", err)
	}

	if data == nil {
		return nil, fmt.Errorf("block response not found for height %d", height)
	}

	resp := &abci.ResponseFinalizeBlock{}
	if err := proto.Unmarshal(data, resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block response: %w", err)
	}

	return resp, nil
}
