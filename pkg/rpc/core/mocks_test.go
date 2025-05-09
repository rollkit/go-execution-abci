package core

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmquery "github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/state/txindex"
	"github.com/stretchr/testify/mock"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	rstore "github.com/rollkit/rollkit/pkg/store"
	"github.com/rollkit/rollkit/types"
)

var _ txindex.TxIndexer = (*MockTxIndexer)(nil)
var _ rstore.Store = (*MockRollkitStore)(nil)
var _ servertypes.ABCI = (*MockApp)(nil)

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

// MockRollkitStore is a mock for rstore.Store
type MockRollkitStore struct {
	mock.Mock
}

func (m *MockRollkitStore) Height(ctx context.Context) (uint64, error) {
	args := m.Called(ctx)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *MockRollkitStore) GetBlockByHash(ctx context.Context, hash []byte) (*types.SignedHeader, *types.Data, error) {
	args := m.Called(ctx, types.Hash(hash))
	var h *types.SignedHeader
	if args.Get(0) != nil {
		h = args.Get(0).(*types.SignedHeader)
	}
	var d *types.Data
	if args.Get(1) != nil {
		d = args.Get(1).(*types.Data)
	}
	return h, d, args.Error(2)
}

func (m *MockRollkitStore) GetBlockData(ctx context.Context, height uint64) (*types.SignedHeader, *types.Data, error) {
	args := m.Called(ctx, height)
	var h *types.SignedHeader
	if args.Get(0) != nil {
		h = args.Get(0).(*types.SignedHeader)
	}
	var d *types.Data
	if args.Get(1) != nil {
		d = args.Get(1).(*types.Data)
	}
	return h, d, args.Error(2)
}

func (m *MockRollkitStore) GetState(ctx context.Context) (types.State, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return types.State{}, args.Error(1)
	}
	return args.Get(0).(types.State), args.Error(1)
}

func (m *MockRollkitStore) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockRollkitStore) GetMetadata(ctx context.Context, key string) ([]byte, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockRollkitStore) SetMetadata(ctx context.Context, key string, value []byte) error {
	args := m.Called(ctx, key, value)
	return args.Error(0)
}

func (m *MockRollkitStore) GetSignature(ctx context.Context, height uint64) (*types.Signature, error) {
	args := m.Called(ctx, height)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Signature), args.Error(1)
}

func (m *MockRollkitStore) GetSignatureByHash(ctx context.Context, hash []byte) (*types.Signature, error) {
	args := m.Called(ctx, hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.Signature), args.Error(1)
}

func (m *MockRollkitStore) SaveBlockData(ctx context.Context, header *types.SignedHeader, data *types.Data, signature *types.Signature) error {
	args := m.Called(ctx, header, data, signature)
	return args.Error(0)
}

func (m *MockRollkitStore) SetHeight(ctx context.Context, height uint64) error {
	args := m.Called(ctx, height)
	return args.Error(0)
}

func (m *MockRollkitStore) UpdateState(ctx context.Context, state types.State) error {
	args := m.Called(ctx, state)
	return args.Error(0)
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
