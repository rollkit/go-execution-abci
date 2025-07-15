package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/libs/log"
	rpc "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/sync"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/rollkit/pkg/store"
	"github.com/rollkit/rollkit/types"

	abci "github.com/cometbft/cometbft/abci/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	networktypes "github.com/rollkit/go-execution-abci/modules/network/types"
)

func TestCommitAndBlock_test(t *testing.T) {
	// Create a validator set with 4 validators
	privValidators := make([]tmtypes.PrivValidator, 4)
	validators := make([]*tmtypes.Validator, 4)
	for i := 0; i < 4; i++ {
		privValidators[i] = tmtypes.NewMockPV()
		pubKey, err := privValidators[i].GetPubKey()
		require.NoError(t, err)
		validators[i] = tmtypes.NewValidator(pubKey, 100)
	}
	validatorSet := tmtypes.NewValidatorSet(validators)
	_ = privValidators // to be used in subsequent steps
	validatorSetHash := validatorSet.Hash()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Setup the environment
	db := datastore.NewMapDatastore()
	realStore := store.New(sync.MutexWrap(db))
	mockApp := &MockApp{}

	// Setup mock response for soft confirmation check
	resp := &networktypes.QuerySoftConfirmationStatusResponse{IsSoftConfirmed: false}
	bz, err := resp.Marshal()
	require.NoError(t, err)

	genesisValidators := make([]tmtypes.GenesisValidator, len(validators))
	for i, v := range validators {
		genesisValidators[i] = tmtypes.GenesisValidator{
			Address: v.Address,
			PubKey:  v.PubKey,
			Power:   v.VotingPower,
			Name:    "validator",
		}
	}

	mockApp.On("Query",
		mock.Anything, // context can be anything
		mock.MatchedBy(func(req *abci.RequestQuery) bool {
			return req.Path == "/rollkitsdk.network.v1.Query/SoftConfirmationStatus"
		}),
	).Return(&abci.ResponseQuery{Code: 0, Value: bz}, nil)

	env = &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: realStore,
			App:          mockApp,
			AppGenesis: &genutiltypes.AppGenesis{
				Consensus: &genutiltypes.ConsensusGenesis{
					Validators: genesisValidators,
				},
			},
		},
		Logger: log.TestingLogger(),
	}
	t.Cleanup(func() {
		env = nil
	})

	// 2. Create a genesis block and 3 more blocks, proposed by the sequencer and stored in memory
	totalBlocks := 4
	signedHeaders := make([]*types.SignedHeader, totalBlocks)
	datas := make([]*types.Data, totalBlocks)
	for i := 0; i < totalBlocks; i++ {
		height := uint64(i + 1)
		header := types.Header{
			BaseHeader: types.BaseHeader{
				Height:  height,
				ChainID: "test-chain",
			},
			ProposerAddress: validators[i%4].Address,
			ValidatorHash:   validatorSetHash,
		}
		signedHeader := &types.SignedHeader{
			Header: header,
		}
		data := &types.Data{}
		signedHeaders[i] = signedHeader
		datas[i] = data

		err := realStore.SaveBlockData(ctx, signedHeader, data, &types.Signature{})
		require.NoError(t, err)

	}

	// 3. Set the store height
	err = realStore.SetHeight(ctx, uint64(totalBlocks))
	require.NoError(t, err)

	// 4. Call Block for each block and log the output
	for i := 1; i <= totalBlocks; i++ {
		h := int64(i)
		blockResult, err := Block(&rpc.Context{}, &h)
		require.NoError(t, err)
		require.NotNil(t, blockResult)
		t.Logf("Block at height %d: %+v\n", h, blockResult.Block)
	}

	// Verify that the LastCommitHash of a block is the hash of the previous block's commit
	block2Result, err := Block(&rpc.Context{}, &[]int64{2}[0])
	require.NoError(t, err)
	commit2Hash := block2Result.Block.LastCommit.Hash()

	block3Result, err := Block(&rpc.Context{}, &[]int64{3}[0])
	require.NoError(t, err)
	assert.Equal(t, commit2Hash, block3Result.Block.Header.LastCommitHash)

	// 5. Call Commit and Block and validate the results
	rpcCtx := &rpc.Context{}

	height := int64(1)

	// Block
	blockResult, err := Block(rpcCtx, &height)
	require.NoError(t, err)
	require.NotNil(t, blockResult)
	assert.Equal(t, height, blockResult.Block.Height)
	assert.Equal(t, signedHeaders[0].Header.ProposerAddress, []byte(blockResult.Block.ProposerAddress))

	// Commit
	commitResult, err := Commit(rpcCtx, &height)
	require.NoError(t, err)
	require.NotNil(t, commitResult)
	assert.Equal(t, height, commitResult.SignedHeader.Header.Height)

	mockApp.AssertExpectations(t)
}
