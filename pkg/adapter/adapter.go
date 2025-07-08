package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/mempool"
	corep2p "github.com/cometbft/cometbft/p2p"
	cmtprototypes "github.com/cometbft/cometbft/proto/tendermint/types"
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
	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
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

// BlockFilter decides if a block commit is build and published
type BlockFilter interface {
	IsPublishable(ctx context.Context, height int64) bool
}

var _ BlockFilter = BlockFilterFn(nil)

type BlockFilterFn func(ctx context.Context, height int64) bool

func (b BlockFilterFn) IsPublishable(ctx context.Context, height int64) bool {
	return b(ctx, height)
}

// LoadGenesisDoc returns the genesis document from the provided config file.
func LoadGenesisDoc(cfg *cmtcfg.Config) (*cmttypes.GenesisDoc, error) {
	genesisFile := cfg.GenesisFile()
	doc, err := cmttypes.GenesisDocFromFile(genesisFile)
	if err != nil {
		return nil, fmt.Errorf("read genesis doc from file: %w", err)
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
	metrics *Metrics

	blockFilter   BlockFilter
	stackedEvents []StackedEvent
}

// NewABCIExecutor creates a new Adapter instance that implements the go-execution.Executor interface.
// The Adapter wraps the provided ABCI application and delegates execution-related operations to it.
func NewABCIExecutor(
	app servertypes.ABCI,
	store ds.Batching,
	p2pClient *rollkitp2p.Client,
	p2pMetrics *rollkitp2p.Metrics,
	logger log.Logger,
	cfg *cmtcfg.Config,
	appGenesis *genutiltypes.AppGenesis,
	opts ...Option,
) *Adapter {
	rollkitPrefixStore := kt.Wrap(store, &kt.PrefixTransform{
		Prefix: ds.NewKey(rollnode.RollkitPrefix),
	})
	rollkitStore := rstore.New(rollkitPrefixStore)

	abciStore := NewExecABCIStore(store)

	a := &Adapter{
		App:          app,
		Store:        abciStore,
		RollkitStore: rollkitStore,
		Logger:       logger,
		P2PClient:    p2pClient,
		p2pMetrics:   p2pMetrics,
		AppGenesis:   appGenesis,
		MempoolIDs:   newMempoolIDs(),
		metrics:      NopMetrics(),
		blockFilter:  BlockFilterFn(func(ctx context.Context, height int64) bool { return true }),
	}

	for _, opt := range opts {
		opt(a)
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
		a.metrics.TxValidationTotal.Add(1)
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
		a.metrics.CheckTxDurationSeconds.Observe(checkTxDuration)

		switch {
		case errors.Is(err, mempool.ErrTxInCache):
			a.metrics.TxValidationResultTotal.With("result", "rejected_in_cache").Add(1)
			return true
		case errors.Is(err, mempool.ErrMempoolIsFull{}):
			a.metrics.TxValidationResultTotal.With("result", "rejected_mempool_full").Add(1)
			return true
		case errors.Is(err, mempool.ErrTxTooLarge{}):
			a.metrics.TxValidationResultTotal.With("result", "rejected_too_large").Add(1)
			return false
		case errors.Is(err, mempool.ErrPreCheck{}):
			a.metrics.TxValidationResultTotal.With("result", "rejected_precheck").Add(1)
			return false
		case err != nil:
			a.metrics.TxValidationResultTotal.With("result", "rejected_checktx_error").Add(1)
			a.Logger.Error("CheckTx failed with unexpected error", "err", err)
			return false
		default:
		}

		select {
		case <-ctx.Done():
			a.metrics.TxValidationResultTotal.With("result", "rejected_context_canceled").Add(1)
			return false
		case checkTxResp := <-checkTxResCh:
			if checkTxResp.Code == abci.CodeTypeOK {
				a.metrics.TxValidationResultTotal.With("result", "accepted").Add(1)
				return true
			}
			a.metrics.TxValidationResultTotal.With("result", "rejected_checktx").Add(1)
			return false
		case <-time.After(5 * time.Second):
			a.metrics.TxValidationResultTotal.With("result", "rejected_timeout").Add(1)
			a.Logger.Error("CheckTx response timed out")
			return false
		}
	}
}

// InitChain implements execution.Executor.
func (a *Adapter) InitChain(ctx context.Context, genesisTime time.Time, initialHeight uint64, chainID string) ([]byte, uint64, error) {
	initChainStart := time.Now()
	defer func() {
		a.metrics.InitChainDurationSeconds.Observe(time.Since(initChainStart).Seconds())
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
		return nil, 0, fmt.Errorf("app initialize chain: %w", err)
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
		return nil, 0, fmt.Errorf("validator updates: %w", err)
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
		return nil, 0, fmt.Errorf("save initial state: %w", err)
	}

	a.Logger.Info("chain initialized successfully", "appHash", fmt.Sprintf("%X", res.AppHash))
	return res.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), nil
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
		a.metrics.BlockExecutionDurationSeconds.Observe(time.Since(execStart).Seconds())
	}()
	a.Logger.Info("Executing block", "height", blockHeight, "num_txs", len(txs), "timestamp", timestamp)
	a.metrics.TxsExecutedPerBlock.Observe(float64(len(txs)))

	s, err := a.Store.LoadState(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("load state: %w", err)
	}

	header, ok := types.SignedHeaderFromContext(ctx)
	if !ok {
		return nil, 0, fmt.Errorf("rollkit header not found in context")
	}

	lastCommit, err := a.getLastCommit(ctx, blockHeight)
	if err != nil {
		return nil, 0, fmt.Errorf("get last commit: %w", err)
	}

	emptyBlock, err := cometcompat.ToABCIBlock(header, &types.Data{}, lastCommit)
	if err != nil {
		return nil, 0, fmt.Errorf("compute header hash: %w", err)
	}

	ppResp, err := a.App.ProcessProposal(&abci.RequestProcessProposal{
		Hash:               emptyBlock.Header.Hash(),
		Height:             int64(blockHeight),
		Time:               timestamp,
		Txs:                txs,
		ProposedLastCommit: cometCommitToABCICommitInfo(lastCommit),
		Misbehavior:        []abci.Misbehavior{},
		ProposerAddress:    s.Validators.Proposer.Address,
		NextValidatorsHash: s.NextValidators.Hash(),
	})
	if err != nil {
		return nil, 0, err
	}

	if ppResp.Status != abci.ResponseProcessProposal_ACCEPT {
		return nil, 0, fmt.Errorf("proposal rejected by app")
	}

	fbResp, err := a.App.FinalizeBlock(&abci.RequestFinalizeBlock{
		Hash:               emptyBlock.Header.Hash(),
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
		Height:             int64(blockHeight),
		Time:               timestamp,
		DecidedLastCommit: abci.CommitInfo{
			Round: 0,
			Votes: nil,
		},
		Txs: txs,
	})
	if err != nil {
		return nil, 0, err
	}

	for i, tx := range txs {
		sum256 := sha256.Sum256(tx)
		a.Logger.Debug("Processed TX", "hash", strings.ToUpper(hex.EncodeToString(sum256[:])), "result", fbResp.TxResults[i].Code, "log", fbResp.TxResults[i].Log, "height", blockHeight)
	}

	s.AppHash = fbResp.AppHash
	s.LastBlockHeight = int64(blockHeight)

	nValSet := s.NextValidators.Copy()

	validatorUpdates, err := cmttypes.PB2TM.ValidatorUpdates(fbResp.ValidatorUpdates)
	if err != nil {
		return nil, 0, err
	}

	lastHeightValsChanged := s.LastHeightValidatorsChanged
	if len(validatorUpdates) > 0 {
		a.metrics.ValidatorUpdatesTotal.Add(float64(len(validatorUpdates)))
		err := nValSet.UpdateWithChangeSet(validatorUpdates)
		if err != nil {
			return nil, 0, fmt.Errorf("changing validator set: %w", err)
		}
		lastHeightValsChanged = int64(blockHeight) + 2
	}

	nValSet.IncrementProposerPriority(1)

	if fbResp.ConsensusParamUpdates != nil {
		a.metrics.ConsensusParamUpdatesTotal.Add(1)
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
		return nil, 0, fmt.Errorf("save state: %w", err)
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

	if blockHeight == 0 {
		lastCommit.Signatures = []cmttypes.CommitSig{
			{
				BlockIDFlag:      cmttypes.BlockIDFlagCommit,
				ValidatorAddress: s.Validators.Proposer.Address,
				Timestamp:        time.Now().UTC(),
				Signature:        []byte{},
			},
		}
	}

	block := s.MakeBlock(int64(blockHeight), cmtTxs, lastCommit, nil, s.Validators.Proposer.Address)

	currentBlockID := cmttypes.BlockID{
		Hash:          block.Hash(),
		PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: block.DataHash},
	}

	if a.blockFilter.IsPublishable(ctx, int64(header.Height())) {
		// save response before the events are fired (current behaviour in CometBFT)
		if err := a.Store.SaveBlockResponse(ctx, blockHeight, fbResp); err != nil {
			return nil, 0, fmt.Errorf("save block response: %w", err)
		}
		if err := fireEvents(a.EventBus, block, currentBlockID, fbResp, validatorUpdates); err != nil {
			return nil, 0, fmt.Errorf("fire events: %w", err)
		}
	} else {
		a.stackBlockCommitEvents(currentBlockID, block, fbResp, validatorUpdates)
		// clear events so that they are not stored with the block data at this stage.
		fbResp.Events = nil
		if err := a.Store.SaveBlockResponse(ctx, blockHeight, fbResp); err != nil {
			return nil, 0, fmt.Errorf("save block response: %w", err)
		}
	}

	a.Logger.Info("block executed successfully", "height", blockHeight, "appHash", fmt.Sprintf("%X", fbResp.AppHash))
	return fbResp.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), nil
}

func fireEvents(
	eventBus cmttypes.BlockEventPublisher,
	block *cmttypes.Block,
	blockID cmttypes.BlockID,
	abciResponse *abci.ResponseFinalizeBlock,
	validatorUpdates []*cmttypes.Validator,
) error {
	if err := eventBus.PublishEventNewBlock(cmttypes.EventDataNewBlock{
		Block:               block,
		BlockID:             blockID,
		ResultFinalizeBlock: *abciResponse,
	}); err != nil {
		return fmt.Errorf("publish new block event: %w", err)
	}

	if err := eventBus.PublishEventNewBlockHeader(cmttypes.EventDataNewBlockHeader{
		Header: block.Header,
	}); err != nil {
		return fmt.Errorf("publish new block header event: %w", err)
	}

	if err := eventBus.PublishEventNewBlockEvents(cmttypes.EventDataNewBlockEvents{
		Height: block.Height,
		Events: abciResponse.Events,
		NumTxs: int64(len(block.Txs)),
	}); err != nil {
		return fmt.Errorf("publish block events: %w", err)
	}

	if len(block.Evidence.Evidence) != 0 {
		for _, ev := range block.Evidence.Evidence {
			if err := eventBus.PublishEventNewEvidence(cmttypes.EventDataNewEvidence{
				Evidence: ev,
				Height:   block.Height,
			}); err != nil {
				return fmt.Errorf("publish new evidence event: %w", err)
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
			return fmt.Errorf("publish event TX: %w", err)
		}
	}

	if len(validatorUpdates) > 0 {
		if err := eventBus.PublishEventValidatorSetUpdates(
			cmttypes.EventDataValidatorSetUpdates{ValidatorUpdates: validatorUpdates}); err != nil {
			return fmt.Errorf("publish valset update event: %w", err)
		}
	}

	return nil
}

func (a *Adapter) getLastCommit(ctx context.Context, blockHeight uint64) (*cmttypes.Commit, error) {
	if blockHeight > 1 {
		header, data, err := a.RollkitStore.GetBlockData(ctx, blockHeight-1)
		if err != nil {
			return nil, fmt.Errorf("get previous block data: %w", err)
		}

		commitForPrevBlock := &cmttypes.Commit{
			Height:  int64(header.Height()),
			Round:   0,
			BlockID: cmttypes.BlockID{Hash: bytes.HexBytes(header.Hash()), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: bytes.HexBytes(data.Hash())}},
			Signatures: []cmttypes.CommitSig{
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: cmttypes.Address(header.ProposerAddress),
					Timestamp:        header.Time(),
					Signature:        header.Signature,
				},
			},
		}

		return commitForPrevBlock, nil
	}

	return &cmttypes.Commit{
		Height:     int64(blockHeight),
		Round:      0,
		BlockID:    cmttypes.BlockID{},
		Signatures: []cmttypes.CommitSig{},
	}, nil
}

func cometCommitToABCICommitInfo(commit *cmttypes.Commit) abci.CommitInfo {
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
				Power:   0,
			},
			BlockIdFlag: cmtprototypes.BlockIDFlag(sig.BlockIDFlag),
		}
	}

	return abci.CommitInfo{
		Round: commit.Round,
		Votes: votes,
	}
}

type StackedEvent struct {
	blockID          cmttypes.BlockID
	block            *cmttypes.Block
	abciResponse     *abci.ResponseFinalizeBlock
	validatorUpdates []*cmttypes.Validator
}

func (a *Adapter) stackBlockCommitEvents(
	blockID cmttypes.BlockID,
	block *cmttypes.Block,
	abciResponse *abci.ResponseFinalizeBlock,
	validatorUpdates []*cmttypes.Validator,
) {
	// todo (Alex): we need this persisted to recover from restart
	a.stackedEvents = append(a.stackedEvents, StackedEvent{
		blockID:          blockID,
		block:            block,
		abciResponse:     abciResponse,
		validatorUpdates: validatorUpdates,
	})
}

// GetTxs calls PrepareProposal with the next height from the store and returns the transactions from the ABCI app
func (a *Adapter) GetTxs(ctx context.Context) ([][]byte, error) {
	getTxsStart := time.Now()
	defer func() {
		a.metrics.GetTxsDurationSeconds.Observe(time.Since(getTxsStart).Seconds())
	}()
	a.Logger.Debug("Getting transactions for proposal")

	s, err := a.Store.LoadState(ctx)
	if err != nil {
		return nil, fmt.Errorf("load state for GetTxs: %w", err)
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

	a.metrics.TxsProposedTotal.Add(float64(len(resp.Txs)))
	return resp.Txs, nil
}

// SetFinal handles extra logic once the block has been finalized (posted to DA).
// It publishes all queued events up to this height in the correct sequence, ensuring
// that events are only emitted after consensus is achieved.
func (a *Adapter) SetFinal(ctx context.Context, blockHeight uint64) error {
	return a.publishQueuedBlockEvents(ctx, int64(blockHeight))
}

func (a *Adapter) publishQueuedBlockEvents(ctx context.Context, persistedHeight int64) error {
	maxPosReleased := -1
	for i, v := range a.stackedEvents {
		h := v.block.Height
		if h <= persistedHeight && a.blockFilter.IsPublishable(ctx, h) {
			maxPosReleased = i
		}
	}
	a.Logger.Info("processing stack for soft consensus", "count", len(a.stackedEvents), "soft_consensus", maxPosReleased != -1)

	if maxPosReleased == -1 {
		return nil
	}
	softCommitHeight := a.stackedEvents[maxPosReleased].block.Height
outerLoop:
	for i := 0; i <= maxPosReleased; i++ {
		// todo (Alex): exit loop when ctx cancelled
		select {
		case <-ctx.Done():
			maxPosReleased = i
			break outerLoop
		default:
		}
		evnt := a.stackedEvents[i]
		// store events with block response
		blockRsp, err := a.Store.GetBlockResponse(ctx, uint64(evnt.block.Height))
		if err != nil {
			return fmt.Errorf("get block response: %w", err)
		}
		blockRsp.Events = evnt.abciResponse.Events
		if err := a.Store.SaveBlockResponse(ctx, uint64(evnt.block.Height), blockRsp); err != nil {
			return fmt.Errorf("save block response: %w", err)
		}

		if err := fireEvents(a.EventBus, evnt.block, evnt.blockID, evnt.abciResponse, evnt.validatorUpdates); err != nil {
			return fmt.Errorf("fire events: %w", err)
		}
		a.Logger.Debug("releasing block events with soft consensus", "height", evnt.block.Height, "soft_consensus", softCommitHeight)
	}
	a.stackedEvents = a.stackedEvents[maxPosReleased+1:]
	a.Logger.Debug("remaining stack after soft consensus", "count", len(a.stackedEvents), "soft_consensus", softCommitHeight)
	return nil
}
