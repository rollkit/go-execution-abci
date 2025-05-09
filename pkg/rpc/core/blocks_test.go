package core

import (
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/rollkit/types"
)

// MockTxIndexer and MockRollkitStore definitions and their methods have been moved to mocks_test.go
// Interface satisfaction checks have also been moved.

// Helper to create a basic rpctypes.Context for tests
func newTestRPCContext() *rpctypes.Context {
	return &rpctypes.Context{}
}

func TestBlockSearch_Success(t *testing.T) {
	mockTxIndexer := new(MockTxIndexer)
	mockRollkitStore := new(MockRollkitStore)
	mockApp := new(MockApp)
	env = &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
		},
		TxIndexer: mockTxIndexer,
		Logger:    cmtlog.NewNopLogger(),
	}

	ctx := newTestRPCContext()
	query := "tx.height > 1"
	page := 1
	perPage := 10
	orderBy := "asc"

	txResult1 := &abci.TxResult{Height: 2, Index: 0}
	txResult2 := &abci.TxResult{Height: 3, Index: 0}
	mockedSearchResults := []*abci.TxResult{txResult1, txResult2}

	mockTxIndexer.On("Search", mock.Anything, mock.AnythingOfType("*query.Query")).Return(mockedSearchResults, nil)

	header1 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader:      types.BaseHeader{Height: 2, Time: uint64(time.Now().UnixNano())},
			ProposerAddress: []byte("proposer1"),
			AppHash:         []byte("apphash1"),
		},
	}
	data1 := &types.Data{
		Txs: make(types.Txs, 0),
	}
	header2 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader:      types.BaseHeader{Height: 3, Time: uint64(time.Now().UnixNano())},
			ProposerAddress: []byte("proposer2"),
			AppHash:         []byte("apphash2"),
		},
	}
	data2 := &types.Data{
		Txs: make(types.Txs, 0),
	}

	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(2)).Return(header1, data1, nil)
	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(3)).Return(header2, data2, nil)

	result, err := BlockSearch(ctx, query, &page, &perPage, orderBy)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Blocks, 2)
	assert.Equal(t, 2, result.TotalCount)

	// Compare specific header fields
	assert.Equal(t, header1.Header.BaseHeader.Height, uint64(result.Blocks[0].Block.Header.Height))
	assert.Equal(t, []byte(header1.Header.AppHash), []byte(result.Blocks[0].Block.Header.AppHash))                 // Convert both to []byte
	assert.Equal(t, []byte(header1.Header.ProposerAddress), []byte(result.Blocks[0].Block.Header.ProposerAddress)) // Convert both to []byte

	assert.Equal(t, header2.Header.BaseHeader.Height, uint64(result.Blocks[1].Block.Header.Height))
	assert.Equal(t, []byte(header2.Header.AppHash), []byte(result.Blocks[1].Block.Header.AppHash))                 // Convert both to []byte
	assert.Equal(t, []byte(header2.Header.ProposerAddress), []byte(result.Blocks[1].Block.Header.ProposerAddress)) // Convert both to []byte

	mockTxIndexer.AssertExpectations(t)
	mockRollkitStore.AssertExpectations(t)
	mockApp.AssertExpectations(t)
}

// TODO: Start with TestBlockSearch tests
