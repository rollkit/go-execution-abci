package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/mempool"
	corep2p "github.com/cometbft/cometbft/p2p"
	cmtstate "github.com/cometbft/cometbft/state"
	cmtypes "github.com/cometbft/cometbft/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/rollkit/rollkit/core/execution"
	rollkitp2p "github.com/rollkit/rollkit/pkg/p2p"
	"github.com/rollkit/rollkit/pkg/store"

	"github.com/rollkit/go-execution-abci/pkg/p2p"
)

var _ execution.Executor = &Adapter{}

// LoadGenesisDoc returns the genesis document from the provided config file.
func LoadGenesisDoc(cfg *config.Config) (*cmtypes.GenesisDoc, error) {
	genesisFile := cfg.GenesisFile()
	doc, err := cmtypes.GenesisDocFromFile(genesisFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read genesis doc from file: %w", err)
	}
	return doc, nil
}

// Adapter is a struct that will contain an ABCI Application, and will implement the go-execution interface
type Adapter struct {
	App        servertypes.ABCI
	Store      store.Store
	Mempool    mempool.Mempool
	MempoolIDs *mempoolIDs

	P2PClient  *rollkitp2p.Client
	TxGossiper *p2p.Gossiper
	p2pMetrics *rollkitp2p.Metrics

	EventBus   *cmtypes.EventBus
	CometCfg   *config.Config
	AppGenesis *genutiltypes.AppGenesis

	Logger  log.Logger
	Metrics *Metrics
}

// NewABCIExecutor creates a new Adapter instance that implements the go-execution.Executor interface.
// The Adapter wraps the provided ABCI application and delegates execution-related operations to it.
func NewABCIExecutor(
	app servertypes.ABCI,
	store store.Store,
	p2pClient *rollkitp2p.Client,
	p2pMetrics *rollkitp2p.Metrics,
	logger log.Logger,
	cfg *config.Config,
	appGenesis *genutiltypes.AppGenesis,
	metrics *Metrics,
) *Adapter {
	if metrics == nil {
		metrics = NopMetrics()
	}
	a := &Adapter{
		App:        app,
		Store:      store,
		Logger:     logger,
		P2PClient:  p2pClient,
		p2pMetrics: p2pMetrics,
		CometCfg:   cfg,
		AppGenesis: appGenesis,
		MempoolIDs: newMempoolIDs(),
		Metrics:    metrics,
	}

	return a
}

func (a *Adapter) SetMempool(mempool mempool.Mempool) {
	a.Mempool = mempool
}

func (a *Adapter) Start(ctx context.Context) error {
	var err error
	topic := fmt.Sprintf("%s-tx", a.AppGenesis.ChainID)
	a.TxGossiper, err = p2p.NewGossiper(a.P2PClient.Host(), a.P2PClient.PubSub(), topic, a.Logger, p2p.WithValidator(
		a.newTxValidator(ctx),
	))
	if err != nil {
		return err
	}

	go a.TxGossiper.ProcessMessages(ctx)

	return nil
}

func (a *Adapter) newTxValidator(ctx context.Context) p2p.GossipValidator {
	return func(m *p2p.GossipMessage) bool {
		a.Metrics.TxValidationTotal.Add(1)
		a.Logger.Debug("transaction received", "bytes", len(m.Data))

		msgBytes := m.Data
		labels := []string{
			"peer_id", m.From.String(),
		}
		a.p2pMetrics.PeerReceiveBytesTotal.With(labels...).Add(float64(len(msgBytes)))
		a.p2pMetrics.MessageReceiveBytesTotal.With("message_type", "tx").Add(float64(len(msgBytes)))
		checkTxResCh := make(chan *abci.ResponseCheckTx, 2)
		checkTxStart := time.Now()
		err := a.Mempool.CheckTx(m.Data, func(resp *abci.ResponseCheckTx) {
			select {
			case <-ctx.Done():
				return
			case checkTxResCh <- resp:
			}
		}, mempool.TxInfo{
			SenderID:    a.MempoolIDs.GetForPeer(m.From),
			SenderP2PID: corep2p.ID(m.From),
		})
		checkTxDuration := time.Since(checkTxStart).Seconds()
		a.Metrics.CheckTxDurationSeconds.Observe(checkTxDuration)

		switch {
		case errors.Is(err, mempool.ErrTxInCache):
			a.Metrics.TxValidationResultTotal.With("result", "rejected_in_cache").Add(1)
			return true
		case errors.Is(err, mempool.ErrMempoolIsFull{}):
			a.Metrics.TxValidationResultTotal.With("result", "rejected_mempool_full").Add(1)
			return true
		case errors.Is(err, mempool.ErrTxTooLarge{}):
			a.Metrics.TxValidationResultTotal.With("result", "rejected_too_large").Add(1)
			return false
		case errors.Is(err, mempool.ErrPreCheck{}):
			a.Metrics.TxValidationResultTotal.With("result", "rejected_precheck").Add(1)
			return false
		case err != nil:
			a.Metrics.TxValidationResultTotal.With("result", "rejected_checktx_error").Add(1)
			a.Logger.Error("CheckTx failed with unexpected error", "err", err)
			return false
		default:
		}

		select {
		case <-ctx.Done():
			a.Metrics.TxValidationResultTotal.With("result", "rejected_context_canceled").Add(1)
			return false
		case checkTxResp := <-checkTxResCh:
			if checkTxResp.Code == abci.CodeTypeOK {
				a.Metrics.TxValidationResultTotal.With("result", "accepted").Add(1)
				return true
			}
			a.Metrics.TxValidationResultTotal.With("result", "rejected_checktx").Add(1)
			return false
		case <-time.After(5 * time.Second):
			a.Metrics.TxValidationResultTotal.With("result", "rejected_timeout").Add(1)
			a.Logger.Error("CheckTx response timed out")
			return false
		}
	}
}

// InitChain implements execution.Executor.
func (a *Adapter) InitChain(ctx context.Context, genesisTime time.Time, initialHeight uint64, chainID string) ([]byte, uint64, error) {
	initChainStart := time.Now()
	defer func() {
		a.Metrics.InitChainDurationSeconds.Observe(time.Since(initChainStart).Seconds())
	}()
	a.Logger.Info("Initializing chain", "chainID", chainID, "initialHeight", initialHeight, "genesisTime", genesisTime)

	if a.AppGenesis == nil {
		err := fmt.Errorf("app genesis not loaded")
		return nil, 0, err
	}

	if a.AppGenesis.GenesisTime != genesisTime {
		err := fmt.Errorf("genesis time mismatch: expected %s, got %s", a.AppGenesis.GenesisTime, genesisTime)
		return nil, 0, err
	}

	if a.AppGenesis.ChainID != chainID {
		err := fmt.Errorf("chain ID mismatch: expected %s, got %s", a.AppGenesis.ChainID, chainID)
		return nil, 0, err
	}

	if initialHeight != uint64(a.AppGenesis.InitialHeight) {
		err := fmt.Errorf("initial height mismatch: expected %d, got %d", a.AppGenesis.InitialHeight, initialHeight)
		return nil, 0, err
	}

	validators := make([]*cmtypes.Validator, len(a.AppGenesis.Consensus.Validators))
	for i, v := range a.AppGenesis.Consensus.Validators {
		validators[i] = cmtypes.NewValidator(v.PubKey, v.Power)
	}

	consensusParams := a.AppGenesis.Consensus.Params.ToProto()

	res, err := a.App.InitChain(&abci.RequestInitChain{
		Time:            genesisTime,
		ChainId:         chainID,
		ConsensusParams: &consensusParams,
		Validators:      cmtypes.TM2PB.ValidatorUpdates(cmtypes.NewValidatorSet(validators)),
		AppStateBytes:   a.AppGenesis.AppState,
		InitialHeight:   int64(initialHeight),
	})
	if err != nil {
		return nil, 0, err
	}

	s := &cmtstate.State{}
	if res.ConsensusParams != nil {
		s.ConsensusParams = cmtypes.ConsensusParamsFromProto(*res.ConsensusParams)
	} else {
		s.ConsensusParams = cmtypes.ConsensusParamsFromProto(consensusParams)
	}

	vals, err := cmtypes.PB2TM.ValidatorUpdates(res.Validators)
	if err != nil {
		return nil, 0, err
	}

	nValSet := cmtypes.NewValidatorSet(vals)

	if len(nValSet.Validators) != 1 {
		err := fmt.Errorf("expected exactly one validator")
		return nil, 0, err
	}

	s.Validators = cmtypes.NewValidatorSet(nValSet.Validators)
	s.NextValidators = cmtypes.NewValidatorSet(nValSet.Validators).CopyIncrementProposerPriority(1)
	s.LastValidators = cmtypes.NewValidatorSet(nValSet.Validators)
	s.LastHeightConsensusParamsChanged = int64(initialHeight)
	s.LastHeightValidatorsChanged = int64(initialHeight)

	if err := a.SaveState(ctx, s); err != nil {
		return nil, 0, fmt.Errorf("failed to save initial state: %w", err)
	}

	a.Logger.Info("Chain initialized successfully", "appHash", fmt.Sprintf("%X", res.AppHash))
	return res.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), nil
}

// ExecuteTxs implements execution.Executor.
func (a *Adapter) ExecuteTxs(ctx context.Context, txs [][]byte, blockHeight uint64, timestamp time.Time, prevStateRoot []byte) ([]byte, uint64, error) {
	execStart := time.Now()
	defer func() {
		a.Metrics.BlockExecutionDurationSeconds.Observe(time.Since(execStart).Seconds())
	}()
	a.Logger.Info("Executing block", "height", blockHeight, "num_txs", len(txs), "timestamp", timestamp)
	a.Metrics.TxsExecutedPerBlock.Observe(float64(len(txs)))

	s, err := a.LoadState(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to load state: %w", err)
	}

	ppResp, err := a.App.ProcessProposal(&abci.RequestProcessProposal{
		Txs:                txs,
		ProposedLastCommit: abci.CommitInfo{},
		Hash:               prevStateRoot,
		Height:             int64(blockHeight),
		Time:               timestamp,
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})
	if err != nil {
		return nil, 0, err
	}

	if ppResp.Status != abci.ResponseProcessProposal_ACCEPT {
		return nil, 0, fmt.Errorf("proposal rejected by app")
	}

	fbResp, err := a.App.FinalizeBlock(&abci.RequestFinalizeBlock{
		Txs:                txs,
		Hash:               prevStateRoot,
		Height:             int64(blockHeight),
		Time:               timestamp,
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})
	if err != nil {
		return nil, 0, err
	}

	nValSet := s.NextValidators.Copy()

	validatorUpdates, err := cmtypes.PB2TM.ValidatorUpdates(fbResp.ValidatorUpdates)
	if err != nil {
		return nil, 0, err
	}

	lastHeightValsChanged := s.LastHeightValidatorsChanged
	if len(validatorUpdates) > 0 {
		a.Metrics.ValidatorUpdatesTotal.Add(float64(len(validatorUpdates)))
		err := nValSet.UpdateWithChangeSet(validatorUpdates)
		if err != nil {
			return nil, 0, fmt.Errorf("changing validator set: %w", err)
		}
		lastHeightValsChanged = int64(blockHeight) + 2
	}

	nValSet.IncrementProposerPriority(1)

	if fbResp.ConsensusParamUpdates != nil {
		a.Metrics.ConsensusParamUpdatesTotal.Add(1)
		nextParams := s.ConsensusParams.Update(fbResp.ConsensusParamUpdates)
		err := nextParams.ValidateBasic()
		if err != nil {
			return nil, 0, fmt.Errorf("validating new consensus params: %w", err)
		}

		err = s.ConsensusParams.ValidateUpdate(fbResp.ConsensusParamUpdates, int64(blockHeight))
		if err != nil {
			return nil, 0, fmt.Errorf("updating consensus params: %w", err)
		}

		s.ConsensusParams = nextParams
		s.LastHeightConsensusParamsChanged = int64(blockHeight) + 1
	}

	s.LastValidators = s.Validators.Copy()
	s.Validators = nValSet.Copy()
	s.NextValidators = nValSet.CopyIncrementProposerPriority(1)
	s.LastHeightValidatorsChanged = lastHeightValsChanged

	if err := a.SaveState(ctx, s); err != nil {
		return nil, 0, fmt.Errorf("failed to save state: %w", err)
	}

	a.Mempool.Lock()
	defer a.Mempool.Unlock()

	err = a.Mempool.FlushAppConn()
	if err != nil {
		a.Logger.Error("client error during mempool.FlushAppConn", "err", err)
		return nil, 0, err
	}

	_, err = a.App.Commit()
	if err != nil {
		a.Logger.Error("client error during proxyAppConn.CommitSync", "err", err)
		return nil, 0, err
	}

	err = a.Mempool.Update(
		int64(blockHeight),
		cmtypes.ToTxs(txs),
		fbResp.TxResults,
		cmtstate.TxPreCheck(*s),
		cmtstate.TxPostCheck(*s),
	)
	if err != nil {
		a.Logger.Error("client error during mempool.Update", "err", err)
		return nil, 0, err
	}

	a.Logger.Info("Block executed successfully", "height", blockHeight, "appHash", fmt.Sprintf("%X", fbResp.AppHash))
	return fbResp.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), nil
}

// GetTxs calls PrepareProposal with the next height from the store and returns the transactions from the ABCI app
func (a *Adapter) GetTxs(ctx context.Context) ([][]byte, error) {
	getTxsStart := time.Now()
	defer func() {
		a.Metrics.GetTxsDurationSeconds.Observe(time.Since(getTxsStart).Seconds())
	}()
	a.Logger.Debug("Getting transactions for proposal")

	s, err := a.LoadState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load state for GetTxs: %w", err)
	}

	if a.Mempool == nil {
		err := fmt.Errorf("mempool not initialized")
		return nil, err
	}

	reapedTxs := a.Mempool.ReapMaxBytesMaxGas(int64(s.ConsensusParams.Block.MaxBytes), -1)
	txsBytes := make([][]byte, len(reapedTxs))
	for i, tx := range reapedTxs {
		txsBytes[i] = tx
	}

	currentHeight, err := a.Store.Height(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := a.App.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:                txsBytes,
		MaxTxBytes:         int64(s.ConsensusParams.Block.MaxBytes),
		Height:             int64(currentHeight + 1),
		Time:               time.Now(),
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})
	if err != nil {
		return nil, err
	}

	a.Metrics.TxsProposedTotal.Add(float64(len(resp.Txs)))
	return resp.Txs, nil
}

// SetFinal implements execution.Executor.
func (a *Adapter) SetFinal(ctx context.Context, blockHeight uint64) error {
	return nil
}

func NewAdapter(store store.Store) *Adapter {
	return &Adapter{
		Store: store,
	}
}

// LoadState loads the state from disk
func (a *Adapter) LoadState(ctx context.Context) (*cmtstate.State, error) {
	return loadState(ctx, a.Store)
}

// SaveState saves the state to disk
func (a *Adapter) SaveState(ctx context.Context, state *cmtstate.State) error {
	return saveState(ctx, a.Store, state)
}
