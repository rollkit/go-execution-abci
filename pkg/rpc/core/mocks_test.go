package core

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmquery "github.com/cometbft/cometbft/libs/pubsub/query"
	cmtstate "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/state/indexer"
	"github.com/cometbft/cometbft/state/txindex"
	cmttypes "github.com/cometbft/cometbft/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/mock"

	rlkp2p "github.com/rollkit/rollkit/pkg/p2p"
	rstore "github.com/rollkit/rollkit/pkg/store"
	rollkitmocks "github.com/rollkit/rollkit/test/mocks"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

var (
	_ txindex.TxIndexer     = (*MockTxIndexer)(nil)
	_ rstore.Store          = (*rollkitmocks.MockStore)(nil)
	_ servertypes.ABCI      = (*MockApp)(nil)
	_ adapter.P2PClientInfo = (*MockP2PClient)(nil)
	_ indexer.BlockIndexer  = (*MockBlockIndexer)(nil)
)

// MockTxIndexer is a mock for txindex.TxIndexer
type MockTxIndexer struct {
	mock.Mock
}

func (m *MockTxIndexer) AddBatch(batch *txindex.Batch) error {
	args := m.Called(batch)
	return args.Error(0)
}

func (m *MockTxIndexer) Index(result *abci.TxResult) error {
	args := m.Called(result)
	return args.Error(0)
}

func (m *MockTxIndexer) Get(hash []byte) (*abci.TxResult, error) {
	args := m.Called(hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.TxResult), args.Error(1)
}

func (m *MockTxIndexer) Search(ctx context.Context, query *cmquery.Query) ([]*abci.TxResult, error) {
	args := m.Called(ctx, query)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*abci.TxResult), args.Error(1)
}

func (m *MockTxIndexer) SetLogger(logger cmtlog.Logger) {
	m.Called(logger)
}

// MockBlockIndexer is a mock for indexer.BlockIndexer
type MockBlockIndexer struct {
	mock.Mock
}

// Has implements indexer.BlockIndexer.
func (m *MockBlockIndexer) Has(height int64) (bool, error) {
	args := m.Called(height)
	return args.Bool(0), args.Error(1)
}

// Index implements indexer.BlockIndexer.
func (m *MockBlockIndexer) Index(events cmttypes.EventDataNewBlockEvents) error {
	args := m.Called(events)
	return args.Error(0)
}

// Search implements indexer.BlockIndexer.
func (m *MockBlockIndexer) Search(ctx context.Context, q *cmquery.Query) ([]int64, error) {
	args := m.Called(ctx, q)
	return args.Get(0).([]int64), args.Error(1)
}

// SetLogger implements indexer.BlockIndexer.
func (m *MockBlockIndexer) SetLogger(l cmtlog.Logger) {
	m.Called(l)
}

// Prune implements indexer.BlockIndexer.
func (m *MockBlockIndexer) Prune(retainHeight int64) (int64, int64, error) {
	args := m.Called(retainHeight)
	return args.Get(0).(int64), args.Get(1).(int64), args.Error(2)
}

// SetRetainHeight implements indexer.BlockIndexer.
func (m *MockBlockIndexer) SetRetainHeight(retainHeight int64) error {
	args := m.Called(retainHeight)
	return args.Error(0)
}

// GetRetainHeight implements indexer.BlockIndexer.
func (m *MockBlockIndexer) GetRetainHeight() (int64, error) {
	args := m.Called()
	return args.Get(0).(int64), args.Error(1)
}

// MockApp is a mock of the ABCI application.
// It implements servertypes.ABCI.
type MockApp struct {
	mock.Mock
}

// Info implements servertypes.ABCI.
func (m *MockApp) Info(req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseInfo), args.Error(1)
}

// Query implements servertypes.ABCI.
// This Query method includes context.Context, which matches the usage in env.Adapter.App.Query
// but differs from the standard abci.Application.Query.
func (m *MockApp) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseQuery), args.Error(1)
}

// CheckTx implements servertypes.ABCI.
func (m *MockApp) CheckTx(req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseCheckTx), args.Error(1)
}

// InitChain implements servertypes.ABCI.
func (m *MockApp) InitChain(req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseInitChain), args.Error(1)
}

// PrepareProposal implements servertypes.ABCI.
// The actual servertypes.ABCI interface for PrepareProposal does not take context.
// However, to align with potential usage patterns or future interface changes,
// this mock might be called with context. Test your specific scenario.
// For strict servertypes.ABCI compliance, context should be removed.
// Based on previous linter errors, servertypes.ABCI.PrepareProposal does NOT take context.
func (m *MockApp) PrepareProposal(req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponsePrepareProposal), args.Error(1)
}

// ProcessProposal implements servertypes.ABCI.
// Similar to PrepareProposal, servertypes.ABCI.ProcessProposal does NOT take context.
func (m *MockApp) ProcessProposal(req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseProcessProposal), args.Error(1)
}

// FinalizeBlock implements servertypes.ABCI.
func (m *MockApp) FinalizeBlock(req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseFinalizeBlock), args.Error(1)
}

// ExtendVote implements servertypes.ABCI.
// This method DOES take context according to the provided servertypes.ABCI definition.
func (m *MockApp) ExtendVote(ctx context.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseExtendVote), args.Error(1)
}

// VerifyVoteExtension implements servertypes.ABCI.
// servertypes.ABCI.VerifyVoteExtension does NOT take context.
func (m *MockApp) VerifyVoteExtension(req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseVerifyVoteExtension), args.Error(1)
}

// Commit implements servertypes.ABCI.
func (m *MockApp) Commit() (*abci.ResponseCommit, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseCommit), args.Error(1)
}

// ListSnapshots implements servertypes.ABCI.
func (m *MockApp) ListSnapshots(req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseListSnapshots), args.Error(1)
}

// OfferSnapshot implements servertypes.ABCI.
func (m *MockApp) OfferSnapshot(req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseOfferSnapshot), args.Error(1)
}

// LoadSnapshotChunk implements servertypes.ABCI.
func (m *MockApp) LoadSnapshotChunk(req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseLoadSnapshotChunk), args.Error(1)
}

// ApplySnapshotChunk implements servertypes.ABCI.
func (m *MockApp) ApplySnapshotChunk(req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseApplySnapshotChunk), args.Error(1)
}

// MockP2PClient is a mock for adapter.P2PClientInfo
type MockP2PClient struct {
	mock.Mock
}

func (m *MockP2PClient) Info() (string, string, string, error) {
	args := m.Called()
	return args.String(0), args.String(1), args.String(2), args.Error(3)
}

func (m *MockP2PClient) Host() host.Host {
	return nil // Stub implementation
}

func (m *MockP2PClient) PubSub() *pubsub.PubSub {
	return nil // Stub implementation
}

func (m *MockP2PClient) Addrs() []ma.Multiaddr {
	addr, _ := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/1234") // Example stub
	return []ma.Multiaddr{addr}
}

func (m *MockP2PClient) Peers() []rlkp2p.PeerConnection {
	return []rlkp2p.PeerConnection{} // Stub implementation
}

// MockStore is a mock for adapter.Store
type MockStore struct {
	mock.Mock
}

func (m *MockStore) LoadState(ctx context.Context) (*cmtstate.State, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cmtstate.State), args.Error(1)
}

func (m *MockStore) SaveState(ctx context.Context, state *cmtstate.State) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockStore) SaveFinalizeBlockResponse(ctx context.Context, height uint64, response *abci.ResponseFinalizeBlock) error {
	args := m.Called(ctx, height, response)
	return args.Error(0)
}

func (m *MockStore) GetFinalizeBlockResponse(ctx context.Context, height uint64) (*abci.ResponseFinalizeBlock, error) {
	args := m.Called(ctx, height)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*abci.ResponseFinalizeBlock), args.Error(1)
}

func (m *MockStore) GetBlockID(ctx context.Context, height uint64) (*cmttypes.BlockID, error) {
	args := m.Called(ctx, height)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cmttypes.BlockID), args.Error(1)
}
