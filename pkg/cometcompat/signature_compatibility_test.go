package cometcompat

import (
	"context"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmttypes "github.com/cometbft/cometbft/types"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/rollkit/types"

	execstore "github.com/rollkit/go-execution-abci/pkg/store"
)

func TestSignatureCompatibility_HeaderAndCommit(t *testing.T) {
	// Create test key pair
	cmtPrivKey := ed25519.GenPrivKey()
	p2pPrivKey, err := crypto.UnmarshalEd25519PrivateKey(cmtPrivKey.Bytes())
	require.NoError(t, err)

	// Create a test store
	dsStore := ds.NewMapDatastore()
	store := execstore.NewExecABCIStore(dsStore)

	validatorAddress := make([]byte, 20)
	chainID := "test-chain"

	// Create a test header
	header := &types.Header{
		BaseHeader: types.BaseHeader{
			Height:  1,
			Time:    uint64(time.Now().UnixNano()),
			ChainID: chainID,
		},
		ProposerAddress: validatorAddress,
	}

	// Test 1: SignaturePayloadProvider should work without BlockID in store
	provider := SignaturePayloadProvider(store)
	signBytes, err := provider(header)
	require.NoError(t, err)
	require.NotEmpty(t, signBytes)

	// Test 2: Should be able to sign the payload
	signature, err := p2pPrivKey.Sign(signBytes)
	require.NoError(t, err)
	require.NotEmpty(t, signature)
	require.Equal(t, 64, len(signature)) // Ed25519 signature length

	// Test 3: After saving BlockID, should still work
	blockID := &cmttypes.BlockID{
		Hash: make([]byte, 32), // dummy hash for test
	}
	err = store.SaveBlockID(context.Background(), header.Height(), blockID)
	require.NoError(t, err)

	signBytes2, err := provider(header)
	require.NoError(t, err)
	require.NotEmpty(t, signBytes2)

	// The sign bytes should be different now that we have a real BlockID
	require.NotEqual(t, signBytes, signBytes2)
}
