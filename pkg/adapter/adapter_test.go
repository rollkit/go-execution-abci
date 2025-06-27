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
	tmcryptoed25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmtpubsub "github.com/cometbft/cometbft/libs/pubsub"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/mempool"
	cmtypes "github.com/cometbft/cometbft/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
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
			myMockApp := mockApp(myExecResult, spec.mockMutator)

			originStore := ds.NewMapDatastore()
			adapter := NewABCIExecutor(myMockApp, originStore, nil, nil, log.NewTestLogger(t), nil, nil)
			adapter.EventBus = eventBus
			adapter.MempoolIDs = newMempoolIDs()
			adapter.Mempool = &mempool.NopMempool{}

			var cometBftPubKey tmcryptoed25519.PubKey
			privKey, pubKey, err := crypto.GenerateEd25519Key(nil)
			require.NoError(t, err)

			// mimic cometbft validator set settings\
			// otherwise .Address is 32 bytes long while cometbft expects 20 bytes
			pubKeyRaw, err := pubKey.Raw()
			require.NoError(t, err)
			cometBftPubKey = pubKeyRaw
			val := cmtypes.NewValidator(cometBftPubKey, 1)

			header := types.Header{
				BaseHeader:      types.BaseHeader{Height: 2, Time: uint64(time.Now().UnixNano())},
				ProposerAddress: val.Address,
				AppHash:         []byte("apphash1"),
			}

			headerBz, err := cometcompat.PayloadProvider()(&header)
			require.NoError(t, err)

			sig, err := privKey.Sign(headerBz)
			require.NoError(t, err)
			sigT := types.Signature(sig)

			signer, err := types.NewSigner(pubKey)
			require.NoError(t, err)
			signedHeader := &types.SignedHeader{
				Header:    header,
				Signer:    signer,
				Signature: sig,
			}
			require.NoError(t, adapter.RollkitStore.SaveBlockData(ctx, signedHeader, &types.Data{Txs: make(types.Txs, 0)}, &sigT))
			require.NoError(t, adapter.Store.SaveState(ctx, stateFixture()))

			// when
			ctx = context.WithValue(ctx, types.SignedHeaderContextKey, signedHeader)
			_, _, err = adapter.ExecuteTxs(ctx, spec.txs, 1, timestamp, bytes.Repeat([]byte{1}, 32))
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

func mockApp(myExecResult []*abci.ExecTxResult, mutator ...func(*MockABCIApp)) *MockABCIApp {
	r := &MockABCIApp{
		ProcessProposalFn: func(proposal *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
		},
		FinalizeBlockFn: func(block *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
			return &abci.ResponseFinalizeBlock{TxResults: myExecResult}, nil
		},
		CommitFn: func() (*abci.ResponseCommit, error) {
			return &abci.ResponseCommit{}, nil
		},
	}
	for _, m := range mutator {
		m(r)
	}
	return r
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
