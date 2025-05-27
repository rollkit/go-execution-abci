package core

import (
	"testing"
	"time"

	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/libp2p/go-libp2p/core/crypto"

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

func TestCommit_VerifyCometBFTCommit_LightClient_Compatible(t *testing.T) {
	blockHeight := uint64(1)
	now := time.Now()
	chainID := "test-chain-id"

	mockPrivKey, mockPubKey, err := crypto.GenerateEd25519Key(nil)
	require.NoError(t, err)

	testSigner, err := types.NewSigner(mockPubKey)
	require.NoError(t, err)
	proposerAddress := testSigner.Address

	blockData := &types.Data{
		Metadata: &types.Metadata{
			ChainID:      chainID,
			Height:       blockHeight,
			Time:         uint64(now.UnixNano()),
			LastDataHash: types.Hash{},
		},
		Txs: make(types.Txs, 0),
	}
	dataHash := blockData.DACommitment()

	rollkitHeader := types.Header{
		BaseHeader: types.BaseHeader{
			Height:  blockHeight,
			Time:    uint64(now.UnixNano()),
			ChainID: chainID,
		},
		Version:         types.Version{Block: 1, App: 1},
		LastHeaderHash:  types.Hash{},
		LastCommitHash:  types.Hash{},
		DataHash:        dataHash,
		ConsensusHash:   types.Hash{},
		AppHash:         types.Hash{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc, 0xd, 0xe, 0xf, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20},
		LastResultsHash: types.Hash{},
		ValidatorHash:   types.Hash{},
		ProposerAddress: proposerAddress,
	}

	rollkitHeaderHash := rollkitHeader.Hash()

	voteProto := cmtproto.Vote{
		Type:             cmtproto.PrecommitType,
		Height:           int64(rollkitHeader.Height()),
		Round:            0,
		BlockID:          cmtproto.BlockID{Hash: rollkitHeaderHash[:], PartSetHeader: cmtproto.PartSetHeader{Total: 1, Hash: dataHash[:]}},
		Timestamp:        now,
		ValidatorAddress: proposerAddress,
		ValidatorIndex:   0,
	}

	payloadBytes := cmttypes.VoteSignBytes(chainID, &voteProto)

	realSignature, err := mockPrivKey.Sign(payloadBytes)
	require.NoError(t, err)

	rollkitSignedHeader := &types.SignedHeader{
		Header:    rollkitHeader,
		Signature: types.Signature(realSignature),
		Signer:    testSigner,
	}

	mockTxIndexer := new(MockTxIndexer)
	mockBlockIndexer := new(MockBlockIndexer)
	mockApp := new(MockApp)
	mockRollkitStore := new(MockRollkitStore)

	originalEnv := env
	env = &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
		},
		TxIndexer:    mockTxIndexer,
		BlockIndexer: mockBlockIndexer,
		Logger:       cmtlog.NewNopLogger(),
	}
	t.Cleanup(func() { env = originalEnv })

	heightForRPC := int64(blockHeight)
	mockRollkitStore.On("GetBlockData", mock.Anything, blockHeight).Return(rollkitSignedHeader, blockData, nil)

	rpcCtx := newTestRPCContext()
	resultCommit, err := Commit(rpcCtx, &heightForRPC)

	require.NoError(t, err)
	require.NotNil(t, resultCommit)

	assert.Equal(t, chainID, resultCommit.ChainID)
	assert.Equal(t, heightForRPC, resultCommit.Height)
	assert.EqualValues(t, rollkitHeader.AppHash[:], resultCommit.AppHash.Bytes(), "AppHash mismatch")
	assert.NotNil(t, resultCommit.SignedHeader.Header, "CometBFT Header should not be nil")
	assert.NotNil(t, resultCommit.SignedHeader.Commit, "CometBFT Commit should not be nil")
	assert.Equal(t, heightForRPC, resultCommit.SignedHeader.Header.Height)
	assert.Equal(t, proposerAddress, resultCommit.SignedHeader.Header.ProposerAddress.Bytes())
	assert.Len(t, resultCommit.SignedHeader.Commit.Signatures, 1, "Should have one commit signature")
	if len(resultCommit.SignedHeader.Commit.Signatures) == 1 {
		commitSig := resultCommit.SignedHeader.Commit.Signatures[0]
		assert.Equal(t, cmttypes.BlockIDFlagCommit, commitSig.BlockIDFlag, "CommitSig BlockIDFlag should be BlockIDFlagCommit")
		assert.Equal(t, proposerAddress, commitSig.ValidatorAddress.Bytes())
		assert.EqualValues(t, realSignature, commitSig.Signature, "Signature mismatch")
	}

	mockRollkitStore.AssertExpectations(t)
}
