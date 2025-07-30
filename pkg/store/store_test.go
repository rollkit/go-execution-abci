package store_test

import (
	"testing"

	ds "github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/evstack/ev-abci/pkg/store"
)

func TestStateIO(t *testing.T) {
	db := ds.NewMapDatastore()
	abciStore := store.NewExecABCIStore(db)
	myState := store.TestingStateFixture()
	require.NoError(t, abciStore.SaveState(t.Context(), myState))
	gotState, gotErr := abciStore.LoadState(t.Context())
	require.NoError(t, gotErr)
	assert.Equal(t, myState, gotState)
	exists, gotErr := db.Has(t.Context(), ds.NewKey("/abci/s"))
	require.NoError(t, gotErr)
	assert.True(t, exists)
}
