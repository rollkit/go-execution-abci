package adapter

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"cosmossdk.io/log"
	header "github.com/celestiaorg/go-header"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/proxy"
	cmtypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/rollkit/go-execution-abci/mempool"
	"github.com/rollkit/go-execution-abci/p2p"
	"github.com/rollkit/rollkit/core/execution"
	rollkitp2p "github.com/rollkit/rollkit/p2p"
	"github.com/rollkit/rollkit/store"
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
	P2PClient  *rollkitp2p.Client
	TxGossiper *p2p.Gossiper
	State      atomic.Pointer[State] // TODO: this should store data on disk
	EventBus   *cmtypes.EventBus
	CometCfg   *config.Config
	AppGenesis *genutiltypes.AppGenesis

	Logger log.Logger
}

// NewABCIExecutor creates a new Adapter instance that implements the go-execution.Executor interface.
// The Adapter wraps the provided ABCI application and delegates execution-related operations to it.
func NewABCIExecutor(
	app servertypes.ABCI,
	store store.Store,
	p2pClient *rollkitp2p.Client,
	logger log.Logger,
	cfg *config.Config,
	appGenesis *genutiltypes.AppGenesis,
) *Adapter {
	a := &Adapter{
		App:        app,
		Store:      store,
		Logger:     logger,
		P2PClient:  p2pClient,
		CometCfg:   cfg,
		AppGenesis: appGenesis,
	}

	// TODO: this looks soooo bad
	cmtApp := server.NewCometABCIWrapper(app)
	clientCreator := proxy.NewLocalClientCreator(cmtApp)

	proxyApp, err := initProxyApp(clientCreator, logger, nil)
	if err != nil {
		panic(err)
	}

	mempool := mempool.NewCListMempool(cfg.Mempool, proxyApp.Mempool(), a.Store.Height(context.Background()))
	a.Mempool = mempool

	return a
}

func initProxyApp(clientCreator proxy.ClientCreator, logger log.Logger, metrics *proxy.Metrics) (proxy.AppConns, error) {
	proxyApp := proxy.NewAppConns(clientCreator, metrics)
	// proxyApp.SetLogger(logger.With("module", "proxy"))
	if err := proxyApp.Start(); err != nil {
		return nil, fmt.Errorf("error while starting proxy app connections: %w", err)
	}
	return proxyApp, nil
}

func (a *Adapter) Start(ctx context.Context) error {
	var err error
	a.TxGossiper, err = p2p.NewGossiper(a.P2PClient.Host(), a.P2PClient.PubSub(), "TODO:-chainid+tx", a.Logger)
	if err != nil {
		return err
	}
	go a.TxGossiper.ProcessMessages(ctx)

	return nil
}

// InitChain implements execution.Executor.
func (a *Adapter) InitChain(ctx context.Context, genesisTime time.Time, initialHeight uint64, chainID string) (stateRoot []byte, maxBytes uint64, err error) {
	if a.AppGenesis == nil {
		return header.Hash{}, 0, fmt.Errorf("app genesis not loaded")
	}

	if a.AppGenesis.GenesisTime != genesisTime {
		return header.Hash{}, 0, fmt.Errorf("genesis time mismatch: expected %s, got %s", a.AppGenesis.GenesisTime, genesisTime)
	}

	if a.AppGenesis.ChainID != chainID {
		return header.Hash{}, 0, fmt.Errorf("chain ID mismatch: expected %s, got %s", a.AppGenesis.ChainID, chainID)
	}

	if initialHeight != uint64(a.AppGenesis.InitialHeight) {
		return header.Hash{}, 0, fmt.Errorf("initial height mismatch: expected %d, got %d", a.AppGenesis.InitialHeight, initialHeight)
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

	s := a.State.Load()
	if s == nil {
		s = &State{}
	}

	if res.ConsensusParams != nil {
		s.ConsensusParams = *res.ConsensusParams
	} else {
		s.ConsensusParams = consensusParams
	}

	vals, err := cmtypes.PB2TM.ValidatorUpdates(res.Validators)
	if err != nil {
		return nil, 0, err
	}

	// apply initchain valset change
	nValSet := cmtypes.NewValidatorSet(vals)

	// TODO: this should be removed, as we should not assume that there is only one validator
	if len(nValSet.Validators) != 1 {
		return nil, 0, fmt.Errorf("expected exactly one validator")
	}

	s.Validators = cmtypes.NewValidatorSet(nValSet.Validators)
	s.NextValidators = cmtypes.NewValidatorSet(nValSet.Validators).CopyIncrementProposerPriority(1)
	s.LastValidators = cmtypes.NewValidatorSet(nValSet.Validators)
	s.LastHeightConsensusParamsChanged = int64(initialHeight)
	s.LastHeightValidatorsChanged = int64(initialHeight)

	a.State.Store(s)

	return res.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), err
}

// ExecuteTxs implements execution.Executor.
func (a *Adapter) ExecuteTxs(ctx context.Context, txs [][]byte, blockHeight uint64, timestamp time.Time, prevStateRoot []byte) ([]byte, uint64, error) {
	// Process proposal first
	s := a.State.Load()

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

	// Then finalize block
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

	return fbResp.AppHash, uint64(s.ConsensusParams.Block.MaxBytes), nil
}

// GetTxs calls PrepareProposal with the next height from the store and returns the transactions from the ABCI app
func (a *Adapter) GetTxs(ctx context.Context) ([][]byte, error) {
	s := a.State.Load()

	if a.Mempool == nil {
		return nil, fmt.Errorf("mempool not initialized")
	}

	reapedTxs := a.Mempool.ReapMaxBytesMaxGas(int64(s.ConsensusParams.Block.MaxBytes), -1)
	txsBytes := make([][]byte, len(reapedTxs))
	for i, tx := range reapedTxs {
		txsBytes[i] = tx
	}

	resp, err := a.App.PrepareProposal(&abci.RequestPrepareProposal{
		MaxTxBytes:         int64(s.ConsensusParams.Block.MaxBytes),
		Txs:                txsBytes,
		LocalLastCommit:    abci.ExtendedCommitInfo{},
		Misbehavior:        []abci.Misbehavior{},
		Height:             int64(a.Store.Height(ctx) + 1),
		Time:               time.Now(), // TODO: this shouldn't cause any issues during sync, as this is only called during block creation
		NextValidatorsHash: s.NextValidators.Hash(),
		ProposerAddress:    s.Validators.Proposer.Address,
	})

	if err != nil {
		return nil, err
	}

	return resp.Txs, nil

}

// SetFinal implements execution.Executor.
func (a *Adapter) SetFinal(ctx context.Context, blockHeight uint64) error {
	_, err := a.App.Commit()
	return err
}
