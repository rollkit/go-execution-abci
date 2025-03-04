package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	header "github.com/celestiaorg/go-header"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtypes "github.com/cometbft/cometbft/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/rollkit/go-execution"
	"github.com/rollkit/go-execution-abci/mempool"
	"github.com/rollkit/go-execution-abci/p2p"
	"github.com/rollkit/go-execution/types"
	"github.com/rollkit/rollkit/store"
	"github.com/rollkit/rollkit/third_party/log"
)

var _ execution.Executor = &Adapter{}

var genesis *cmtypes.GenesisDoc

// Adapter is a struct that will contain an ABCI Application, and will implement the go-execution interface
type Adapter struct {
	app        servertypes.ABCI
	store      store.Store
	mempool    mempool.Mempool
	txGossiper *p2p.Gossiper
	logger     log.Logger

	state atomic.Pointer[State] // TODO: this should store data on disk
}

// NewABCIExecutor creates a new Adapter instance that implements the go-execution.Executor interface.
// The Adapter wraps the provided ABCI application and delegates execution-related operations to it.
func NewABCIExecutor(
	app servertypes.ABCI,
	store store.Store,
	ps *pubsub.PubSub,
	host host.Host,
	logger log.Logger,
) execution.Executor {
	a := &Adapter{app: app, store: store, logger: logger}
	err := a.setupGossiping(context.Background(), ps, host)
	if err != nil {
		panic(err)
	}

	return a
}

func (a *Adapter) setupGossiping(ctx context.Context, ps *pubsub.PubSub, host host.Host) error {
	var err error
	a.txGossiper, err = p2p.NewGossiper(host, ps, "TODO:-chainid+tx", a.logger)
	if err != nil {
		return err
	}
	go a.txGossiper.ProcessMessages(ctx)

	return nil
}

// InitChain implements execution.Executor.
func (a *Adapter) InitChain(ctx context.Context, genesisTime time.Time, initialHeight uint64, chainID string) (stateRoot header.Hash, maxBytes uint64, err error) {
	// TODO: add genesis file support, check if the genesis data provided by rollkit matches the provided for the ABCI app
	if genesis.GenesisTime != genesisTime {
		return header.Hash{}, 0, fmt.Errorf("genesis time mismatch: expected %s, got %s", genesis.GenesisTime, genesisTime)
	}

	if genesis.ChainID != chainID {
		return header.Hash{}, 0, fmt.Errorf("chain ID mismatch: expected %s, got %s", genesis.ChainID, chainID)
	}

	if initialHeight != uint64(genesis.InitialHeight) {
		return header.Hash{}, 0, fmt.Errorf("initial height mismatch: expected %d, got %d", genesis.InitialHeight, initialHeight)
	}

	validators := make([]*cmtypes.Validator, len(genesis.Validators))
	for i, v := range genesis.Validators {
		validators[i] = cmtypes.NewValidator(v.PubKey, v.Power)
	}

	consensusParams := genesis.ConsensusParams.ToProto()

	res, err := a.app.InitChain(&abci.RequestInitChain{
		Time:            genesisTime,
		ChainId:         chainID,
		ConsensusParams: &consensusParams,
		Validators:      cmtypes.TM2PB.ValidatorUpdates(cmtypes.NewValidatorSet(validators)),
		AppStateBytes:   genesis.AppState,
		InitialHeight:   int64(initialHeight),
	})

	if err != nil {
		return nil, 0, err
	}

	s := a.state.Load()
	s.ConsensusParams = *res.ConsensusParams

	vals, err := cmtypes.PB2TM.ValidatorUpdates(res.Validators)
	if err != nil {
		return nil, 0, err
	}

	// apply initchain valset change
	nValSet := s.Validators.Copy()
	err = nValSet.UpdateWithChangeSet(vals)
	if err != nil {
		return nil, 0, err
	}

	// TODO: this should be removed, as we should not assume that there is only one validator
	if len(nValSet.Validators) != 1 {
		return nil, 0, fmt.Errorf("expected exactly one validator")
	}

	s.Validators = cmtypes.NewValidatorSet(nValSet.Validators)
	s.NextValidators = cmtypes.NewValidatorSet(nValSet.Validators).CopyIncrementProposerPriority(1)
	s.LastValidators = cmtypes.NewValidatorSet(nValSet.Validators)
	s.LastHeightConsensusParamsChanged = int64(initialHeight)
	s.LastHeightValidatorsChanged = int64(initialHeight)

	a.state.Store(s)

	return res.AppHash, uint64(res.ConsensusParams.Block.MaxBytes), err
}

// ExecuteTxs implements execution.Executor.
func (a *Adapter) ExecuteTxs(ctx context.Context, txs []types.Tx, blockHeight uint64, timestamp time.Time, prevStateRoot header.Hash) (updatedStateRoot header.Hash, maxBytes uint64, err error) {
	// run processProposal, then run finalizeblock
	s := a.state.Load()

	// Process proposal first
	ppResp, err := a.app.ProcessProposal(&abci.RequestProcessProposal{
		Txs:                byteTxs(txs),
		ProposedLastCommit: abci.CommitInfo{},
		Hash:               prevStateRoot[:],
		Height:             int64(blockHeight),
		Time:               timestamp,
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})
	if err != nil {
		return header.Hash{}, 0, err
	}

	if ppResp.Status != abci.ResponseProcessProposal_ACCEPT {
		return header.Hash{}, 0, fmt.Errorf("proposal rejected by app")
	}

	// Then finalize block
	fbResp, err := a.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Txs:                byteTxs(txs),
		Hash:               prevStateRoot[:],
		Height:             int64(blockHeight),
		Time:               timestamp,
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})
	if err != nil {
		return header.Hash{}, 0, err
	}

	return header.Hash(fbResp.AppHash), uint64(s.ConsensusParams.Block.MaxBytes), nil

}

// GetTxs calls PrepareProposal with the next height from the store and returns the transactions from the ABCI app
func (a *Adapter) GetTxs(ctx context.Context) ([]types.Tx, error) {
	s := a.state.Load()

	reapedTxs := a.mempool.ReapMaxBytesMaxGas(int64(s.ConsensusParams.Block.MaxBytes), -1)
	txsBytes := make([][]byte, len(reapedTxs))
	for i, tx := range reapedTxs {
		txsBytes[i] = tx
	}

	resp, err := a.app.PrepareProposal(&abci.RequestPrepareProposal{
		MaxTxBytes:         int64(s.ConsensusParams.Block.MaxBytes),
		Txs:                txsBytes,
		LocalLastCommit:    abci.ExtendedCommitInfo{},
		Misbehavior:        []abci.Misbehavior{},
		Height:             int64(a.store.Height() + 1),
		Time:               time.Now(), // TODO: this shouldn't cause any issues during sync, as this is only called during block creation
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})

	if err != nil {
		return nil, err
	}

	var txs = make([]types.Tx, len(resp.Txs))
	for i, tx := range resp.Txs {
		txs[i] = tx
	}

	return txs, nil

}

// SetFinal implements execution.Executor.
func (a *Adapter) SetFinal(ctx context.Context, blockHeight uint64) error {
	// call commit
	_, err := a.app.Commit()
	return err
}

func byteTxs(txs []types.Tx) [][]byte {
	result := make([][]byte, len(txs))
	for i, tx := range txs {
		result[i] = tx
	}
	return result
}
