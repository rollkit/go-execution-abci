package core

import (
	"testing"
	"time"

	cmtlog "github.com/cometbft/cometbft/libs/log"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

func newTestRPCContext() *rpctypes.Context {
	return &rpctypes.Context{}
}

func TestBlockSearch_Success(t *testing.T) {
	mockTxIndexer := new(MockTxIndexer)
	mockRollkitStore := new(MockRollkitStore)
	mockApp := new(MockApp)
	mockBlockIndexer := new(MockBlockIndexer)
	env = &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
		},
		TxIndexer:    mockTxIndexer,
		BlockIndexer: mockBlockIndexer,
		Logger:       cmtlog.NewNopLogger(),
	}

	ctx := newTestRPCContext()
	query := "tx.height > 1"
	page := 1
	perPage := 10
	orderBy := "asc"

	mockedBlockHeights := []int64{2, 3}

	mockBlockIndexer.On("Search", mock.Anything, mock.AnythingOfType("*query.Query")).Return(mockedBlockHeights, nil)

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

	assert.Equal(t, header1.BaseHeader.Height, uint64(result.Blocks[0].Block.Height))
	assert.Equal(t, []byte(header1.AppHash), []byte(result.Blocks[0].Block.AppHash))
	assert.Equal(t, []byte(header1.ProposerAddress), []byte(result.Blocks[0].Block.ProposerAddress))

	assert.Equal(t, header2.BaseHeader.Height, uint64(result.Blocks[1].Block.Height))
	assert.Equal(t, []byte(header2.AppHash), []byte(result.Blocks[1].Block.AppHash))
	assert.Equal(t, []byte(header2.ProposerAddress), []byte(result.Blocks[1].Block.ProposerAddress))

	mockTxIndexer.AssertExpectations(t)
	mockBlockIndexer.AssertExpectations(t)
	mockRollkitStore.AssertExpectations(t)
	mockApp.AssertExpectations(t)
}
