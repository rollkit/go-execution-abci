package cometcompat

import (
	"testing"
	"time"

	ds "github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/rollkit/types"

	execstore "github.com/rollkit/go-execution-abci/pkg/store"
)

func TestSignaturePayloadProvider_MissingBlockID(t *testing.T) {
	// Create a test store
	dsStore := ds.NewMapDatastore()
	store := execstore.NewExecABCIStore(dsStore)

	// Create the SignaturePayloadProvider
	provider := SignaturePayloadProvider(store)

	// Create a test header (no BlockID exists in store yet)
	header := &types.Header{
		BaseHeader: types.BaseHeader{
			Height:  1,
			Time:    uint64(time.Now().UnixNano()),
			ChainID: "test-chain",
		},
		ProposerAddress: make([]byte, 20),
	}

	// This should not error even though BlockID doesn't exist
	signBytes, err := provider(header)
	require.NoError(t, err)
	require.NotEmpty(t, signBytes)

	// Test with height > 1 (should also work now)
	header.BaseHeader.Height = 2
	signBytes2, err := provider(header)
	require.NoError(t, err)
	require.NotEmpty(t, signBytes2)
}