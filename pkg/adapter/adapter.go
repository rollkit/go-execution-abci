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
	types1 "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtstate "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	ds "github.com/ipfs/go-datastore"
	kt "github.com/ipfs/go-datastore/keytransform"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	host "github.com/libp2p/go-libp2p/core/host"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/rollkit/rollkit/core/execution"
	rollnode "github.com/rollkit/rollkit/node"
	rollkitp2p "github.com/rollkit/rollkit/pkg/p2p"
	rstore "github.com/rollkit/rollkit/pkg/store"

	"github.com/rollkit/go-execution-abci/pkg/p2p"
)

var _ execution.Executor = &Adapter{}

type P2PClientInfo interface {
	Info() (string, string, string, error)
	Host() host.Host
	PubSub() *pubsub.PubSub
	Addrs() []ma.Multiaddr
	Peers() []rollkitp2p.PeerConnection
}

// LoadGenesisDoc returns the genesis document from the provided config file.
func LoadGenesisDoc(cfg *config.Config) (*cmttypes.GenesisDoc, error) {
	genesisFile := cfg.GenesisFile()
	doc, err := cmttypes.GenesisDocFromFile(genesisFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read genesis doc from file: %w", err)
	}
	return doc, nil
}

// Adapter is a struct that will contain an ABCI Application, and will implement the go-execution interface
type Adapter struct {
	App          servertypes.ABCI
	Store        *Store
	RollkitStore rstore.Store
	Mempool      mempool.Mempool
	MempoolIDs   *mempoolIDs

	P2PClient  P2PClientInfo
	TxGossiper *p2p.Gossiper
	p2pMetrics *rollkitp2p.Metrics

	EventBus   *cmttypes.EventBus
	AppGenesis *genutiltypes.AppGenesis

	Logger  log.Logger
	Metrics *Metrics
}

// NewABCIExecutor creates a new Adapter instance that implements the go-execution.Executor interface.
// The Adapter wraps the provided ABCI application and delegates execution-related operations to it.
func NewABCIExecutor(
	app servertypes.ABCI,
	store ds.Batching,
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

	// create rollkit prefix
	rollkitPrefixStore := kt.Wrap(store, &kt.PrefixTransform{
		Prefix: ds.NewKey(rollnode.RollkitPrefix),
	})
	rollkitStore := rstore.New(rollkitPrefixStore)
	// Create a new Store with ABCI prefix
	abciStore := NewStore(store)

	a := &Adapter{
		App:          app,
		Store:        abciStore,
		RollkitStore: rollkitStore,
		Logger:       logger,
		P2PClient:    p2pClient,
		p2pMetrics:   p2pMetrics,
		AppGenesis:   appGenesis,
		MempoolIDs:   newMempoolIDs(),
		Metrics:      metrics,
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

	validators := make([]*cmttypes.Validator, len(a.AppGenesis.Consensus.Validators))
	for i, v := range a.AppGenesis.Consensus.Validators {
		validators[i] = cmttypes.NewValidator(v.PubKey, v.Power)
	}

	consensusParams := a.AppGenesis.Consensus.Params.ToProto()

	res, err := a.App.InitChain(&abci.RequestInitChain{
		Time:            genesisTime,
		ChainId:         chainID,
		ConsensusParams: &consensusParams,
		Validators:      cmttypes.TM2PB.ValidatorUpdates(cmttypes.NewValidatorSet(validators)),
		AppStateBytes:   a.AppGenesis.AppState,
		InitialHeight:   int64(initialHeight),
	})
	if err != nil {
		return nil, 0, err
	}

	s := &cmtstate.State{}
	if res.ConsensusParams != nil {
		s.ConsensusParams = cmttypes.ConsensusParamsFromProto(*res.ConsensusParams)
	} else {
		s.ConsensusParams = cmttypes.ConsensusParamsFromProto(consensusParams)
	}
	s.ChainID = chainID

	vals, err := cmttypes.PB2TM.ValidatorUpdates(res.Validators)
	if err != nil {
		return nil, 0, err
	}

	nValSet := cmttypes.NewValidatorSet(vals)

	if len(nValSet.Validators) != 1 {
		err := fmt.Errorf("expected exactly one validator")
		return nil, 0, err
	}

	s.Validators = cmttypes.NewValidatorSet(nValSet.Validators)
	s.NextValidators = cmttypes.NewValidatorSet(nValSet.Validators).CopyIncrementProposerPriority(1)
	s.LastValidators = cmttypes.NewValidatorSet(nValSet.Validators)
	s.LastHeightConsensusParamsChanged = int64(initialHeight)
	s.LastHeightValidatorsChanged = int64(initialHeight)
	s.AppHash = res.AppHash

	if err := a.Store.SaveState(ctx, s); err != nil {
		return nil, 0, fmt.Errorf("failed to save initial state: %w", err)
	}

	a.Logger.Info("Chain initialized successfully", "appHash", fmt.Sprintf("%X", res.AppHash))
	return res.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), nil
}

// getCommit retrieves the commit for the previous block height.
// If blockHeight is the initial height, it returns an empty commit.
func (a *Adapter) getCommit(ctx context.Context, blockHeight uint64) (*cmttypes.Commit, error) {
	lastAttestation, err := a.RollkitStore.GetSequencerAttestation(ctx, blockHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to get sequencer attestation for height %d: %w", blockHeight, err)
	}

	cmtBlockID_H_minus_1 := cmttypes.BlockID{
		Hash: lastAttestation.BlockHeaderHash,
		PartSetHeader: cmttypes.PartSetHeader{
			Total: 1,
			Hash:  lastAttestation.BlockDataHash,
		},
	}
	cmtSig_H_minus_1 := cmttypes.CommitSig{
		BlockIDFlag:      cmttypes.BlockIDFlagCommit,
		ValidatorAddress: cmttypes.Address(lastAttestation.SequencerAddress),
		Timestamp:        lastAttestation.Timestamp,
		Signature:        lastAttestation.Signature,
	}
	return &cmttypes.Commit{
		Height:     int64(lastAttestation.Height),
		Round:      lastAttestation.Round,
		BlockID:    cmtBlockID_H_minus_1,
		Signatures: []cmttypes.CommitSig{cmtSig_H_minus_1},
	}, nil
}

// ExecuteTxs implements execution.Executor.
func (a *Adapter) ExecuteTxs(
	ctx context.Context,
	txs [][]byte,
	blockHeight uint64,
	timestamp time.Time,
	prevStateRoot []byte,
) ([]byte, uint64, error) {
	execStart := time.Now()
	defer func() {
		a.Metrics.BlockExecutionDurationSeconds.Observe(time.Since(execStart).Seconds())
	}()
	a.Logger.Info("Executing block", "height", blockHeight, "num_txs", len(txs), "timestamp", timestamp)
	a.Metrics.TxsExecutedPerBlock.Observe(float64(len(txs)))

	s, err := a.Store.LoadState(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to load state: %w", err)
	}

	cometCommit, err := a.getCommit(ctx, blockHeight)
	if err != nil {
		return nil, 0, err
	}
	calculatedPrevBlockCommitHash := cometCommit.Hash()

	if err := a.RollkitStore.SaveCommitHash(ctx, blockHeight, calculatedPrevBlockCommitHash); err != nil {
		return nil, 0, fmt.Errorf("failed to save calculated commit hash for height %d: %w", blockHeight, err)
	}
	a.Logger.Info("Saved commit hash for height", "height", blockHeight, "commitHash", calculatedPrevBlockCommitHash)

	// The loaded state 's' (which is for H-1) is used to provide context (e.g. NextValidatorsHash).
	// s.LastBlockID will be updated after FinalizeBlock for block H to reflect H-1's BlockID.
	// s.AppHash at this point is AppHash_H-1 from store.

	// Convert the commit for H-1 to ABCI type for ProcessProposal and FinalizeBlock
	var abciCommitInfo_H_minus_1 abci.CommitInfo = abci.CommitInfo{}
	var cometCommit_H_minus_1 *cmttypes.Commit = &cmttypes.Commit{}
	if blockHeight > uint64(a.AppGenesis.InitialHeight) {
		cometCommit_H_minus_1, err := a.getCommit(ctx, blockHeight-1)
		if err != nil {
			return nil, 0, err
		}
		abciCommitInfo_H_minus_1 = cometCommitToABCICommitInfo(cometCommit_H_minus_1)
	}

	ppResp, err := a.App.ProcessProposal(&abci.RequestProcessProposal{
		Txs:                txs,
		ProposedLastCommit: abciCommitInfo_H_minus_1, // Pass commit for H-1
		Hash:               prevStateRoot,            // This is the provisional RollkitHeader_H.Hash()
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
		DecidedLastCommit:  abciCommitInfo_H_minus_1, // Pass commit for H-1
		Hash:               prevStateRoot,            // This is the provisional RollkitHeader_H.Hash()
		Height:             int64(blockHeight),
		Time:               timestamp,
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})
	if err != nil {
		return nil, 0, err
	}

	// Update s to reflect state H.
	s.AppHash = fbResp.AppHash
	s.LastBlockHeight = int64(blockHeight) // Height is now H
	// Set LastBlockID to the ID of the *previous* block (H-1), whose commit was just processed.
	s.LastBlockID = cometCommit_H_minus_1.BlockID

	nValSet := s.NextValidators.Copy()

	validatorUpdates, err := cmttypes.PB2TM.ValidatorUpdates(fbResp.ValidatorUpdates)
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
	s.AppHash = fbResp.AppHash

	if err := a.Store.SaveState(ctx, s); err != nil {
		return nil, 0, fmt.Errorf("failed to save state: %w", err)
	}

	err = func() error { // Lock mempool and commit
		a.Mempool.Lock()
		defer a.Mempool.Unlock()

		err = a.Mempool.FlushAppConn()
		if err != nil {
			a.Logger.Error("client error during mempool.FlushAppConn", "err", err)
			return err
		}

		_, err = a.App.Commit()
		if err != nil {
			a.Logger.Error("client error during proxyAppConn.CommitSync", "err", err)
			return err
		}

		err = a.Mempool.Update(
			int64(blockHeight),
			cmttypes.ToTxs(txs),
			fbResp.TxResults,
			cmtstate.TxPreCheck(*s),
			cmtstate.TxPostCheck(*s),
		)
		if err != nil {
			a.Logger.Error("client error during mempool.Update", "err", err)
			return err
		}
		return nil
	}()
	if err != nil {
		return nil, 0, err
	}

	cmtTxs := make(cmttypes.Txs, len(txs))
	for i := range txs {
		cmtTxs[i] = txs[i]
	}
	// s is now state H. The third argument to MakeBlock is `lastCommit`, which is the commit for H-1.
	block := s.MakeBlock(int64(blockHeight), cmtTxs, cometCommit_H_minus_1, nil, s.Validators.Proposer.Address)
	// The ADR implies Rollkit BlockManager retrieves `calculatedPrevBlockCommitHash` and sets it on RollkitHeader_H.LastCommitHash.
	// The `fireEvents` function takes blockID cmttypes.BlockID as its 4th argument.
	// We need the BlockID for the current block H.
	// Constructing BlockID_H for fireEvents using Header_H.Hash and PartsHeader_H.Hash from the newly created block.
	currentBlockID := cmttypes.BlockID{Hash: block.Header.Hash(), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: block.Header.DataHash}}

	fireEvents(a.Logger, a.EventBus, block, currentBlockID, fbResp, validatorUpdates)

	a.Logger.Info("Block executed successfully", "height", blockHeight, "appHash", fmt.Sprintf("%X", fbResp.AppHash))
	return fbResp.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), nil
}

func fireEvents(
	logger log.Logger,
	eventBus cmttypes.BlockEventPublisher,
	block *cmttypes.Block,
	blockID cmttypes.BlockID,
	abciResponse *abci.ResponseFinalizeBlock,
	validatorUpdates []*cmttypes.Validator,
) {
	if err := eventBus.PublishEventNewBlock(cmttypes.EventDataNewBlock{
		Block:               block,
		BlockID:             blockID,
		ResultFinalizeBlock: *abciResponse,
	}); err != nil {
		logger.Error("failed publishing new block", "err", err)
	}

	if err := eventBus.PublishEventNewBlockHeader(cmttypes.EventDataNewBlockHeader{
		Header: block.Header,
	}); err != nil {
		logger.Error("failed publishing new block header", "err", err)
	}

	if err := eventBus.PublishEventNewBlockEvents(cmttypes.EventDataNewBlockEvents{
		Height: block.Height,
		Events: abciResponse.Events,
		NumTxs: int64(len(block.Txs)),
	}); err != nil {
		logger.Error("failed publishing new block events", "err", err)
	}

	if len(block.Evidence.Evidence) != 0 {
		for _, ev := range block.Evidence.Evidence {
			if err := eventBus.PublishEventNewEvidence(cmttypes.EventDataNewEvidence{
				Evidence: ev,
				Height:   block.Height,
			}); err != nil {
				logger.Error("failed publishing new evidence", "err", err)
			}
		}
	}

	for i, tx := range block.Txs {
		if err := eventBus.PublishEventTx(cmttypes.EventDataTx{TxResult: abci.TxResult{
			Height: block.Height,
			Index:  uint32(i),
			Tx:     tx,
			Result: *(abciResponse.TxResults[i]),
		}}); err != nil {
			logger.Error("failed publishing event TX", "err", err)
		}
	}

	if len(validatorUpdates) > 0 {
		if err := eventBus.PublishEventValidatorSetUpdates(
			cmttypes.EventDataValidatorSetUpdates{ValidatorUpdates: validatorUpdates}); err != nil {
			logger.Error("failed publishing event", "err", err)
		}
	}
}

// GetTxs calls PrepareProposal with the next height from the store and returns the transactions from the ABCI app
func (a *Adapter) GetTxs(ctx context.Context) ([][]byte, error) {
	getTxsStart := time.Now()
	defer func() {
		a.Metrics.GetTxsDurationSeconds.Observe(time.Since(getTxsStart).Seconds())
	}()
	a.Logger.Debug("Getting transactions for proposal")

	s, err := a.Store.LoadState(ctx)
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

	currentHeight, err := a.RollkitStore.Height(ctx)
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

// SetFinal handles extra logic once the block has been finalized (posted to DA).
// For a Cosmos SDK app, this is a no-op we do not need to do anything to mark the block as finalized.
func (a *Adapter) SetFinal(ctx context.Context, blockHeight uint64) error {
	return nil
}

// cometCommitToABCICommitInfo converts a CometBFT Commit to an ABCI CommitInfo.
// This helper function is based on ADR Phase 2, step 7.
func cometCommitToABCICommitInfo(commit *cmttypes.Commit) abci.CommitInfo {
	if commit == nil {
		return abci.CommitInfo{
			Round: 0,
			Votes: []abci.VoteInfo{},
		}
	}

	if len(commit.Signatures) == 0 {
		return abci.CommitInfo{
			Round: commit.Round,
			Votes: []abci.VoteInfo{},
		}
	}

	votes := make([]abci.VoteInfo, len(commit.Signatures))
	for i, sig := range commit.Signatures {
		votes[i] = abci.VoteInfo{
			Validator: abci.Validator{
				Address: sig.ValidatorAddress,
				Power:   0, // Power is not in CommitSig; set to 0 as per ADR context.
			},
			BlockIdFlag: types1.BlockIDFlag(sig.BlockIDFlag),
		}
	}
	return abci.CommitInfo{
		Round: commit.Round,
		Votes: votes,
	}
}
