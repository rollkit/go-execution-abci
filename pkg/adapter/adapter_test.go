package adapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtpubsub "github.com/cometbft/cometbft/libs/pubsub"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/mempool"
	cmtypes "github.com/cometbft/cometbft/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	ds "github.com/ipfs/go-datastore"
	kt "github.com/ipfs/go-datastore/keytransform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rollnode "github.com/rollkit/rollkit/node"
	rstore "github.com/rollkit/rollkit/pkg/store"
)

func TestExecuteFiresEvents(t *testing.T) {
	timestamp := time.Now()
	myTxs := [][]byte{{0x01}, {0x02}}
	myExecResult := []*abci.ExecTxResult{{Code: 0, Data: []byte{0}}, {Code: 0, Data: []byte{1}}}
	specs := map[string]struct {
		txs         [][]byte
		mockMutator func(*MockABCIApp)
		expErr      bool
	}{
		"all good - events published": {
			txs:         myTxs,
			mockMutator: func(*MockABCIApp) {},
		},
		"proposal fails - no events": {
			txs: myTxs,
			mockMutator: func(m *MockABCIApp) {
				m.ProcessProposalFn = func(proposal *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
					return nil, errors.New("test")
				}
			},
			expErr: true,
		},
		"finalize fails - no events": {
			txs: myTxs,
			mockMutator: func(m *MockABCIApp) {
				m.FinalizeBlockFn = func(block *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
					return nil, errors.New("test")
				}
			},
			expErr: true,
		},
		"commit fails - no events": {
			txs: myTxs,
			mockMutator: func(m *MockABCIApp) {
				m.CommitFn = func() (*abci.ResponseCommit, error) {
					return nil, errors.New("test")
				}
			},
			expErr: true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			eventBus := cmtypes.NewEventBus()
			require.NoError(t, eventBus.Start())
			t.Cleanup(func() { _ = eventBus.Stop() })

			capturedBlockEvents, blockMx := captureEvents(ctx, eventBus, "tm.event='NewBlock'", 1)
			capturedTxEvents, txMx := captureEvents(ctx, eventBus, "tm.event='Tx'", 2)
			mockApp := &MockABCIApp{
				ProcessProposalFn: func(proposal *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
					return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
				},
				FinalizeBlockFn: func(block *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
					return &abci.ResponseFinalizeBlock{
						TxResults: myExecResult,
					}, nil
				},
				CommitFn: func() (*abci.ResponseCommit, error) {
					return &abci.ResponseCommit{}, nil
				},
			}
			spec.mockMutator(mockApp)

			originStore := ds.NewMapDatastore()
			rollkitPrefixStore := kt.Wrap(originStore, &kt.PrefixTransform{
				Prefix: ds.NewKey(rollnode.RollkitPrefix),
			})
			rollkitStore := rstore.New(rollkitPrefixStore)
			abciStore := NewStore(originStore)
			adapter := &Adapter{
				App:          mockApp,
				Store:        abciStore,
				RollkitStore: rollkitStore,
				EventBus:     eventBus,
				Metrics:      NopMetrics(),
				Logger:       log.NewTestLogger(t),
				MempoolIDs:   newMempoolIDs(),
				Mempool:      &mempool.NopMempool{},
			}
			require.NoError(t, adapter.Store.SaveState(ctx, stateFixture()))

			// when
			_, _, err := adapter.ExecuteTxs(ctx, spec.txs, 1, timestamp, bytes.Repeat([]byte{1}, 32))
			if spec.expErr {
				require.Error(t, err)
				blockMx.RLock()
				defer blockMx.RUnlock()
				require.Empty(t, *capturedBlockEvents)
				require.Empty(t, *capturedTxEvents)
				return
			}
			require.NoError(t, err)

			assert.Eventually(t, func() bool {
				blockMx.RLock()
				defer blockMx.RUnlock()
				return len(*capturedBlockEvents) == 1
			}, time.Second, 20*time.Millisecond)
			assert.Eventually(t, func() bool {
				txMx.RLock()
				defer txMx.RUnlock()
				return len(*capturedTxEvents) == 2
			}, time.Second, 20*time.Millisecond)
			cancel()

			blockMx.RLock()
			t.Cleanup(blockMx.RUnlock)
			gotMsg := (*capturedBlockEvents)[0].Data().(cmtypes.EventDataNewBlock)
			expAbciResult := abci.ResponseFinalizeBlock{
				TxResults: myExecResult,
			}
			assert.Equal(t, expAbciResult, gotMsg.ResultFinalizeBlock)

			txMx.RLock()
			t.Cleanup(txMx.RUnlock)
			for i, v := range *capturedTxEvents {
				event := v.Data().(cmtypes.EventDataTx)
				assert.Equal(t, *expAbciResult.TxResults[i], event.Result)
				assert.Equal(t, spec.txs[i], event.Tx)
				assert.Equal(t, uint32(i), event.Index)
				assert.Equal(t, int64(1), event.Height)
			}
		})
	}
}

func captureEvents(ctx context.Context, eventBus *cmtypes.EventBus, query string, numEventsExpected int) (*[]cmtpubsub.Message, *sync.RWMutex) {
	subscriber := fmt.Sprintf("test-%d", time.Now().UnixNano())
	evSub, err := eventBus.Subscribe(ctx, subscriber, cmtquery.MustCompile(query), numEventsExpected)
	if err != nil {
		panic(err)
	}
	var mx sync.RWMutex
	capturedEvents := make([]cmtpubsub.Message, 0, numEventsExpected)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case v := <-evSub.Out():
				mx.Lock()
				capturedEvents = append(capturedEvents, v)
				if len(capturedEvents) >= numEventsExpected {
					mx.Unlock()
					return
				}
				mx.Unlock()
			}
		}
	}()
	return &capturedEvents, &mx
}

type MockABCIApp struct {
	servertypes.ABCI  // satisfy the interface
	ProcessProposalFn func(*abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error)
	FinalizeBlockFn   func(*abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error)
	CommitFn          func() (*abci.ResponseCommit, error)
}

func (m *MockABCIApp) ProcessProposal(r *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	if m.ProcessProposalFn == nil {
		panic("not expected to be called")
	}
	return m.ProcessProposalFn(r)
}
func (m *MockABCIApp) FinalizeBlock(r *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	if m.FinalizeBlockFn == nil {
		panic("not expected to be called")
	}
	return m.FinalizeBlockFn(r)
}

func (m *MockABCIApp) Commit() (*abci.ResponseCommit, error) {
	if m.CommitFn == nil {
		panic("not expected to be called")
	}
	return m.CommitFn()
}
