package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/log"
	rpc "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	tmtypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/sync"

	"github.com/rollkit/go-execution-abci/modules/network/types"
	"github.com/rollkit/go-execution-abci/pkg/adapter"
	rollkittypes "github.com/rollkit/rollkit/types"

	rollkitstore "github.com/rollkit/rollkit/pkg/store"
)

// setupTestEnv initializes the test environment.
func setupTestEnv(t *testing.T, numValidators, totalBlocks int) (*MockApp, []*rollkittypes.SignedHeader) {
	t.Helper()

	// Create a validator set
	privValidators := make([]tmtypes.PrivValidator, numValidators)
	validators := make([]*tmtypes.Validator, numValidators)
	for i := 0; i < numValidators; i++ {
		privValidators[i] = tmtypes.NewMockPV()
		pubKey, err := privValidators[i].GetPubKey()
		require.NoError(t, err)
		validators[i] = tmtypes.NewValidator(pubKey, 100)
	}
	validatorSet := tmtypes.NewValidatorSet(validators)
	validatorSetHash := validatorSet.Hash()

	db := datastore.NewMapDatastore()
	realStore := rollkitstore.New(sync.MutexWrap(db))
	mockApp := &MockApp{}

	// Setup mock response for soft confirmation check
	resp := &types.QuerySoftConfirmationStatusResponse{IsSoftConfirmed: false}
	bz, err := resp.Marshal()
	require.NoError(t, err)

	mockApp.On("Query",
		mock.Anything, // context can be anything
		mock.MatchedBy(func(req *abci.RequestQuery) bool {
			return req.Path == "/rollkitsdk.network.v1.Query/SoftConfirmationStatus"
		}),
	).Return(&abci.ResponseQuery{Code: 0, Value: bz}, nil)

	genesisValidators := make([]tmtypes.GenesisValidator, len(validators))
	for i, v := range validators {
		genesisValidators[i] = tmtypes.GenesisValidator{
			Address: v.Address,
			PubKey:  v.PubKey,
			Power:   v.VotingPower,
			Name:    fmt.Sprintf("validator-%d", i),
		}
	}

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

	// Create and store blocks
	signedHeaders := make([]*rollkittypes.SignedHeader, totalBlocks)
	ctx := context.Background()

	for i := 0; i < totalBlocks; i++ {
		height := uint64(i + 1)
		header := rollkittypes.Header{
			BaseHeader: rollkittypes.BaseHeader{
				Height:  height,
				ChainID: "test-chain",
			},
			ProposerAddress: validators[i%numValidators].Address,
			ValidatorHash:   validatorSetHash,
		}
		signedHeader := &rollkittypes.SignedHeader{
			Header: header,
		}
		data := &rollkittypes.Data{}
		signedHeaders[i] = signedHeader

		err := realStore.SaveBlockData(ctx, signedHeader, data, &rollkittypes.Signature{})
		require.NoError(t, err)
	}

	err = realStore.SetHeight(ctx, uint64(totalBlocks))
	require.NoError(t, err)

	return mockApp, signedHeaders
}

func TestCommitAndBlockRPC(t *testing.T) {
	const numValidators = 4
	const numBlocks = 4
	mockApp, signedHeaders := setupTestEnv(t, numValidators, numBlocks)

	t.Run("All blocks are retrievable", func(t *testing.T) {
		for i := 1; i <= numBlocks; i++ {
			h := int64(i)
			blockResult, err := Block(&rpc.Context{}, &h)
			require.NoError(t, err, "expected no error when retrieving block %d", h)
			require.NotNil(t, blockResult, "block result for height %d should not be nil", h)
			t.Logf("Successfully retrieved block at height %d", h)
		}
	})

	t.Run("Specific block and commit actions", func(t *testing.T) {
		rpcCtx := &rpc.Context{}
		height := int64(1)

		t.Run("Block", func(t *testing.T) {
			blockResult, err := Block(rpcCtx, &height)
			require.NoError(t, err)
			require.NotNil(t, blockResult)
			assert.Equal(t, height, blockResult.Block.Height, "block height should match requested height")
			assert.Equal(t, signedHeaders[0].Header.ProposerAddress, []byte(blockResult.Block.ProposerAddress), "proposer address should match")
		})

		t.Run("Commit", func(t *testing.T) {
			commitResult, err := Commit(rpcCtx, &height)
			require.NoError(t, err)
			require.NotNil(t, commitResult)
			assert.Equal(t, height, commitResult.SignedHeader.Header.Height, "commit header height should match requested height")
			assert.Equal(t, commitResult.SignedHeader.Header.Hash(), commitResult.SignedHeader.Commit.BlockID.Hash, "commit block hash should match header hash")
		})
	})

	mockApp.AssertExpectations(t)
}
