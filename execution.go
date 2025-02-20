package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proxy"
	cmtypes "github.com/cometbft/cometbft/types"
	execution "github.com/rollkit/go-execution"
	proxy_grpc "github.com/rollkit/go-execution/proxy/grpc"
	execution_types "github.com/rollkit/go-execution/types"
	"github.com/rollkit/rollkit/mempool"
	"github.com/rollkit/rollkit/state"
	"github.com/rollkit/rollkit/store"
	"github.com/rollkit/rollkit/third_party/log"
	"github.com/rollkit/rollkit/types"
	abciconv "github.com/rollkit/rollkit/types/abci"
)

// ErrEmptyValSetGenerated is returned when applying the validator changes would result in empty set.
var ErrEmptyValSetGenerated = errors.New("applying the validator changes would result in empty set")

// ErrAddingValidatorToBased is returned when trying to add a validator to an empty validator set.
var ErrAddingValidatorToBased = errors.New("cannot add validators to empty validator set")

// ABCIExecutionClient implements the Execute interface for ABCI applications
type ABCIExecutionClient struct {
	client *proxy_grpc.Client
	// abci specific
	proxyApp        proxy.AppConnConsensus
	eventBus        *cmtypes.EventBus
	genesis         *cmtypes.GenesisDoc
	maxBytes        uint64
	proposerAddress []byte
	chainID         string

	// rollkit specific
	mempool       mempool.Mempool
	mempoolReaper *mempool.CListMempoolReaper
	logger        log.Logger
	metrics       *state.Metrics
	state         *types.State
	store         store.Store
}

// Ensure ABCIExecutionClient implements the execution.Execute interface
var _ execution.Executor = (*ABCIExecutionClient)(nil)

// NewABCIExecutionClient creates a new ABCIExecutionClient
func NewABCIExecutionClient(
	client *proxy_grpc.Client,
	proxyApp proxy.AppConnConsensus,
	eventBus *cmtypes.EventBus,
	genesis *cmtypes.GenesisDoc,
	maxBytes uint64,
	proposerAddress []byte,
	chainID string,
	mempool mempool.Mempool,
	mempoolReaper *mempool.CListMempoolReaper,
	logger log.Logger,
	metrics *state.Metrics,
	state *types.State,
	store store.Store,
) *ABCIExecutionClient {
	return &ABCIExecutionClient{
		client:          client,
		proxyApp:        proxyApp,
		eventBus:        eventBus,
		genesis:         genesis,
		maxBytes:        maxBytes,
		proposerAddress: proposerAddress,
		chainID:         chainID,
		mempool:         mempool,
		mempoolReaper:   mempoolReaper,
		logger:          logger,
		metrics:         metrics,
		state:           state,
		store:           store,
	}
}

// InitChain initializes the blockchain with genesis information.
func (c *ABCIExecutionClient) InitChain(
	ctx context.Context,
	genesisTime time.Time,
	initialHeight uint64,
	chainID string,
) (types.Hash, uint64, error) {
	genesis := &cmtypes.GenesisDoc{
		GenesisTime:     genesisTime,
		ChainID:         chainID,
		ConsensusParams: c.genesis.ConsensusParams,
		Validators:      c.genesis.Validators,
		AppState:        c.genesis.AppState,
		InitialHeight:   int64(initialHeight),
	}

	response, err := c.initChain(genesis)
	if err != nil {
		return types.Hash{}, 0, err
	}

	stateRoot := types.Hash(response.AppHash)
	maxBytes := response.ConsensusParams.Block.MaxBytes

	return stateRoot, uint64(maxBytes), nil
}

// initChain calls InitChainSync using consensus connection to app.
func (c *ABCIExecutionClient) initChain(genesis *cmtypes.GenesisDoc) (*abci.ResponseInitChain, error) {
	params := genesis.ConsensusParams

	validators := make([]*cmtypes.Validator, len(genesis.Validators))
	for i, v := range genesis.Validators {
		validators[i] = cmtypes.NewValidator(v.PubKey, v.Power)
	}

	return c.proxyApp.InitChain(context.Background(), &abci.RequestInitChain{
		Time:    genesis.GenesisTime,
		ChainId: genesis.ChainID,
		ConsensusParams: &cmproto.ConsensusParams{
			Block: &cmproto.BlockParams{
				MaxBytes: params.Block.MaxBytes,
				MaxGas:   params.Block.MaxGas,
			},
			Evidence: &cmproto.EvidenceParams{
				MaxAgeNumBlocks: params.Evidence.MaxAgeNumBlocks,
				MaxAgeDuration:  params.Evidence.MaxAgeDuration,
				MaxBytes:        params.Evidence.MaxBytes,
			},
			Validator: &cmproto.ValidatorParams{
				PubKeyTypes: params.Validator.PubKeyTypes,
			},
			Version: &cmproto.VersionParams{
				App: params.Version.App,
			},
			Abci: &cmproto.ABCIParams{
				VoteExtensionsEnableHeight: params.ABCI.VoteExtensionsEnableHeight,
			},
		},
		Validators:    cmtypes.TM2PB.ValidatorUpdates(cmtypes.NewValidatorSet(validators)),
		AppStateBytes: genesis.AppState,
		InitialHeight: genesis.InitialHeight,
	})
}

// GetTxs retrieves all available transactions from the mempool.
func (c *ABCIExecutionClient) GetTxs(ctx context.Context) ([]execution_types.Tx, error) {
	state, err := c.store.GetState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current state: %w", err)
	}

	maxBytes := state.ConsensusParams.Block.MaxBytes
	if maxBytes == -1 {
		maxBytes = int64(cmtypes.MaxBlockSizeBytes)
	}
	if maxBytes > int64(c.maxBytes) {
		c.logger.Debug("limiting maxBytes to", "maxBytes", c.maxBytes)
		maxBytes = int64(c.maxBytes)
	}

	cmTxs := c.mempool.ReapMaxTxs(int(maxBytes))

	txs := make([]execution_types.Tx, len(cmTxs))
	for i, tx := range cmTxs {
		txs[i] = execution_types.Tx(tx)
	}

	return txs, nil
}

// ExecuteTxs executes the given transactions and returns the new state root and gas used.
func (c *ABCIExecutionClient) ExecuteTxs(
	ctx context.Context,
	txs []execution_types.Tx,
	blockHeight uint64,
	timestamp time.Time,
	prevStateRoot types.Hash,
) (types.Hash, uint64, error) {
	state, err := c.store.GetState(ctx)
	if err != nil {
		return types.Hash{}, 0, wrapStateError(err, "get current state")
	}

	cmTxs := fromExecutionTxs(txs)

	var lastSignature *types.Signature
	var lastHeaderHash types.Hash
	var lastExtendedCommit abci.ExtendedCommitInfo

	if blockHeight == uint64(c.genesis.InitialHeight) {
		lastSignature = &types.Signature{}
		lastHeaderHash = types.Hash{}
		lastExtendedCommit = abci.ExtendedCommitInfo{}
	} else {
		lastSignature, err = c.store.GetSignature(ctx, blockHeight-1)
		if err != nil {
			return types.Hash{}, 0, wrapBlockError(blockHeight-1, err, "load last commit")
		}

		lastHeader, _, err := c.store.GetBlockData(ctx, blockHeight-1)
		if err != nil {
			return types.Hash{}, 0, wrapBlockError(blockHeight-1, err, "load last block")
		}
		lastHeaderHash = lastHeader.Hash()

		extCommit, err := c.store.GetExtendedCommit(ctx, blockHeight-1)
		if err != nil {
			return types.Hash{}, 0, wrapBlockError(blockHeight-1, err, "load extended commit")
		}
		if extCommit != nil {
			lastExtendedCommit = *extCommit
		}
	}

	header, data, err := c.CreateBlock(
		blockHeight,
		lastSignature,
		lastExtendedCommit,
		lastHeaderHash,
		state,
		cmTxs,
		timestamp,
	)
	if err != nil {
		return types.Hash{}, 0, wrapBlockError(blockHeight, err, "create block")
	}

	isValid, err := c.ProcessProposal(header, data, state)
	if err != nil {
		return types.Hash{}, 0, wrapBlockError(blockHeight, err, "process proposal")
	}
	if !isValid {
		return types.Hash{}, 0, fmt.Errorf("proposal at height %d was not valid", blockHeight)
	}

	newState, resp, err := c.ApplyBlock(ctx, state, header, data)
	if err != nil {
		return types.Hash{}, 0, wrapBlockError(blockHeight, err, "apply block")
	}

	appHash, _, err := c.Commit(ctx, newState, header, data, resp)
	if err != nil {
		return types.Hash{}, 0, wrapBlockError(blockHeight, err, "commit block")
	}

	return types.Hash(appHash), uint64(newState.ConsensusParams.Block.MaxBytes), nil
}

// SetFinal marks a block at the given height as final.
func (c *ABCIExecutionClient) SetFinal(ctx context.Context, blockHeight uint64) error {
	header, data, err := c.store.GetBlockData(ctx, blockHeight)
	if err != nil {
		return wrapBlockError(blockHeight, err, "get block data")
	}

	state, err := c.store.GetState(ctx)
	if err != nil {
		return wrapStateError(err, "get current state")
	}

	resp, err := c.proxyApp.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{
		Hash:               header.Hash(),
		Height:             int64(blockHeight),
		Time:               header.Time(),
		Txs:                data.Txs.ToSliceOfBytes(),
		ProposerAddress:    header.ProposerAddress,
		NextValidatorsHash: state.Validators.Hash(),
	})
	if err != nil {
		return wrapBlockError(blockHeight, err, "finalize block")
	}

	state.AppHash = resp.AppHash
	if err := c.store.UpdateState(ctx, state); err != nil {
		return wrapBlockError(blockHeight, err, "update state after finalizing block")
	}

	c.logger.Info("Block finalized", "height", blockHeight, "hash", header.Hash())

	return nil
}

// CreateBlock reaps transactions from mempool and builds a block.
func (c *ABCIExecutionClient) CreateBlock(height uint64, lastSignature *types.Signature, lastExtendedCommit abci.ExtendedCommitInfo, lastHeaderHash types.Hash, state types.State, txs cmtypes.Txs, timestamp time.Time) (*types.SignedHeader, *types.Data, error) {
	maxBytes := state.ConsensusParams.Block.MaxBytes
	emptyMaxBytes := maxBytes == -1
	if emptyMaxBytes {
		maxBytes = int64(cmtypes.MaxBlockSizeBytes)
	}
	if maxBytes > int64(c.maxBytes) {
		c.logger.Debug("limiting maxBytes to", "e.maxBytes=%d", c.maxBytes)
		maxBytes = int64(c.maxBytes)
	}

	header := &types.SignedHeader{
		Header: types.Header{
			Version: types.Version{
				Block: state.Version.Consensus.Block,
				App:   state.Version.Consensus.App,
			},
			BaseHeader: types.BaseHeader{
				ChainID: c.chainID,
				Height:  height,
				Time:    uint64(timestamp.UnixNano()),
			},
			DataHash:        make(types.Hash, 32),
			ConsensusHash:   make(types.Hash, 32),
			AppHash:         state.AppHash,
			LastResultsHash: state.LastResultsHash,
			ProposerAddress: c.proposerAddress,
		},
		Signature: *lastSignature,
	}
	data := &types.Data{
		Txs: toRollkitTxs(txs),
	}

	rpp, err := c.proxyApp.PrepareProposal(
		context.TODO(),
		&abci.RequestPrepareProposal{
			MaxTxBytes:         maxBytes,
			Txs:                txs.ToSliceOfBytes(),
			LocalLastCommit:    lastExtendedCommit,
			Misbehavior:        []abci.Misbehavior{},
			Height:             int64(header.Height()),
			Time:               header.Time(),
			NextValidatorsHash: state.Validators.Hash(),
			ProposerAddress:    c.proposerAddress,
		},
	)
	if err != nil {
		return nil, nil, err
	}

	txl := cmtypes.ToTxs(rpp.Txs)
	if err := txl.Validate(maxBytes); err != nil {
		return nil, nil, err
	}

	data.Txs = toRollkitTxs(txl)
	header.LastCommitHash = lastSignature.GetCommitHash(&header.Header, c.proposerAddress)
	header.LastHeaderHash = lastHeaderHash

	return header, data, nil
}

// ProcessProposal calls the corresponding ABCI method on the app.
func (c *ABCIExecutionClient) ProcessProposal(
	header *types.SignedHeader,
	data *types.Data,
	state types.State,
) (bool, error) {
	resp, err := c.proxyApp.ProcessProposal(context.TODO(), &abci.RequestProcessProposal{
		Hash:   header.Hash(),
		Height: int64(header.Height()),
		Time:   header.Time(),
		Txs:    data.Txs.ToSliceOfBytes(),
		ProposedLastCommit: abci.CommitInfo{
			Round: 0,
			Votes: []abci.VoteInfo{
				{
					Validator: abci.Validator{
						Address: header.Validators.GetProposer().Address,
						Power:   header.Validators.GetProposer().VotingPower,
					},
					BlockIdFlag: cmproto.BlockIDFlagCommit,
				},
			},
		},
		Misbehavior:        []abci.Misbehavior{},
		ProposerAddress:    c.proposerAddress,
		NextValidatorsHash: state.Validators.Hash(),
	})
	if err != nil {
		return false, err
	}
	if resp.IsStatusUnknown() {
		panic(fmt.Sprintf("ProcessProposal responded with status %s", resp.Status.String()))
	}

	return resp.IsAccepted(), nil
}

// ApplyBlock validates and executes the block.
func (c *ABCIExecutionClient) ApplyBlock(ctx context.Context, state types.State, header *types.SignedHeader, data *types.Data) (types.State, *abci.ResponseFinalizeBlock, error) {
	isAppValid, err := c.ProcessProposal(header, data, state)
	if err != nil {
		return types.State{}, nil, err
	}
	if !isAppValid {
		return types.State{}, nil, fmt.Errorf("proposal processing resulted in an invalid application state")
	}

	err = c.Validate(state, header, data)
	if err != nil {
		return types.State{}, nil, err
	}

	resp, err := c.execute(ctx, state, header, data)
	if err != nil {
		return types.State{}, nil, err
	}
	abciValUpdates := resp.ValidatorUpdates

	validatorUpdates, err := cmtypes.PB2TM.ValidatorUpdates(abciValUpdates)
	if err != nil {
		return state, nil, err
	}

	if resp.ConsensusParamUpdates != nil {
		c.metrics.ConsensusParamUpdates.Add(1)
	}

	state, err = c.updateState(state, header, data, resp, validatorUpdates)
	if err != nil {
		return types.State{}, nil, err
	}

	if state.ConsensusParams.Block.MaxBytes <= 0 {
		c.logger.Error("maxBytes<=0", "state.ConsensusParams.Block", state.ConsensusParams.Block, "header", header)
	}

	return state, resp, nil
}

// ExtendVote calls the ExtendVote ABCI method on the proxy app.
func (c *ABCIExecutionClient) ExtendVote(ctx context.Context, header *types.SignedHeader, data *types.Data) ([]byte, error) {
	resp, err := c.proxyApp.ExtendVote(ctx, &abci.RequestExtendVote{
		Hash:   header.Hash(),
		Height: int64(header.Height()),
		Time:   header.Time(),
		Txs:    data.Txs.ToSliceOfBytes(),
		ProposedLastCommit: abci.CommitInfo{
			Votes: []abci.VoteInfo{{
				Validator: abci.Validator{
					Address: header.Validators.GetProposer().Address,
					Power:   header.Validators.GetProposer().VotingPower,
				},
				BlockIdFlag: cmproto.BlockIDFlagCommit,
			}},
		},
		Misbehavior:        nil,
		NextValidatorsHash: header.ValidatorHash,
		ProposerAddress:    header.ProposerAddress,
	})
	if err != nil {
		return nil, err
	}
	return resp.VoteExtension, nil
}

// Commit commits the block
func (c *ABCIExecutionClient) Commit(ctx context.Context, state types.State, header *types.SignedHeader, data *types.Data, resp *abci.ResponseFinalizeBlock) ([]byte, uint64, error) {
	appHash, retainHeight, err := c.commit(ctx, state, header, data, resp)
	if err != nil {
		return []byte{}, 0, err
	}

	state.AppHash = appHash

	c.publishEvents(resp, header, data, state)

	return appHash, retainHeight, nil
}

// updateConsensusParams updates the consensus parameters based on the provided updates.
func (c *ABCIExecutionClient) updateConsensusParams(
	height uint64,
	currentParams cmtypes.ConsensusParams,
	updates *cmproto.ConsensusParams,
) (cmproto.ConsensusParams, uint64, error) {
	if updates == nil {
		return currentParams.ToProto(), currentParams.Version.App, nil
	}

	nextParams := currentParams.Update(updates)
	if err := types.ConsensusParamsValidateBasic(nextParams); err != nil {
		return cmproto.ConsensusParams{}, 0, fmt.Errorf("validating new consensus params: %w", err)
	}

	if err := nextParams.ValidateUpdate(updates, int64(height)); err != nil {
		return cmproto.ConsensusParams{}, 0, fmt.Errorf("updating consensus params: %w", err)
	}

	return nextParams.ToProto(), nextParams.Version.App, nil
}

func (c *ABCIExecutionClient) updateValidatorSet(state types.State, height uint64, validatorUpdates []*cmtypes.Validator) (*cmtypes.ValidatorSet, int64, error) {
	nValSet := state.NextValidators.Copy()
	lastHeightValSetChanged := state.LastHeightValidatorsChanged

	if len(nValSet.Validators) > 0 {
		err := nValSet.UpdateWithChangeSet(validatorUpdates)
		if err != nil {
			if err.Error() != ErrEmptyValSetGenerated.Error() {
				return nil, 0, fmt.Errorf("failed to update validator set: %w", err)
			}
			nValSet = &cmtypes.ValidatorSet{
				Validators: make([]*cmtypes.Validator, 0),
				Proposer:   nil,
			}
		}

		lastHeightValSetChanged = int64(height + 1 + 1)

		if len(nValSet.Validators) > 0 {
			nValSet.IncrementProposerPriority(1)
		}
	}

	return nValSet, lastHeightValSetChanged, nil
}

func (c *ABCIExecutionClient) createNewState(
	state types.State,
	header *types.SignedHeader,
	resp *abci.ResponseFinalizeBlock,
	nValSet *cmtypes.ValidatorSet,
	lastHeightValSetChanged int64,
	nextParams cmproto.ConsensusParams,
	nextParamsHeight uint64,
) types.State {
	newState := types.State{
		Version:         state.Version,
		ChainID:         state.ChainID,
		InitialHeight:   state.InitialHeight,
		LastBlockHeight: header.Height(),
		LastBlockTime:   header.Time(),
		LastBlockID: cmtypes.BlockID{
			Hash: cmbytes.HexBytes(header.Hash()),
		},
		ConsensusParams:                  nextParams,
		LastHeightConsensusParamsChanged: nextParamsHeight,
		AppHash:                          resp.AppHash,
		Validators:                       state.NextValidators.Copy(),
		NextValidators:                   nValSet,
		LastHeightValidatorsChanged:      lastHeightValSetChanged,
		LastValidators:                   state.Validators.Copy(),
	}

	copy(newState.LastResultsHash[:], cmtypes.NewResults(resp.TxResults).Hash())
	return newState
}

// Update the main updateState function to use these helpers
func (c *ABCIExecutionClient) updateState(
	state types.State,
	header *types.SignedHeader,
	_ *types.Data,
	finalizeBlockResponse *abci.ResponseFinalizeBlock,
	validatorUpdates []*cmtypes.Validator,
) (types.State, error) {
	height := header.Height()
	nextParams, appVersion, err := c.updateConsensusParams(
		height,
		types.ConsensusParamsFromProto(state.ConsensusParams),
		finalizeBlockResponse.ConsensusParamUpdates,
	)
	if err != nil {
		return types.State{}, fmt.Errorf("failed to update consensus params: %w", err)
	}

	nValSet, lastHeightValSetChanged, err := c.updateValidatorSet(state, height, validatorUpdates)
	if err != nil {
		return types.State{}, fmt.Errorf("failed to update validator set: %w", err)
	}

	nextParamsHeight := height + 1
	if finalizeBlockResponse.ConsensusParamUpdates != nil {
		state.Version.Consensus.App = appVersion
	}

	newState := c.createNewState(
		state,
		header,
		finalizeBlockResponse,
		nValSet,
		lastHeightValSetChanged,
		nextParams,
		nextParamsHeight,
	)

	return newState, nil
}

func (c *ABCIExecutionClient) commit(ctx context.Context, state types.State, header *types.SignedHeader, data *types.Data, resp *abci.ResponseFinalizeBlock) ([]byte, uint64, error) {
	c.mempool.Lock()
	defer c.mempool.Unlock()

	err := c.mempool.FlushAppConn()
	if err != nil {
		return nil, 0, err
	}

	commitResp, err := c.proxyApp.Commit(ctx)
	if err != nil {
		return nil, 0, err
	}

	maxBytes := state.ConsensusParams.Block.MaxBytes
	maxGas := state.ConsensusParams.Block.MaxGas
	execTxs := fromRollkitTxs(data.Txs)
	cmTxs := fromExecutionTxs(execTxs)
	c.mempoolReaper.UpdateCommitedTxs(cmTxs)
	err = c.mempool.Update(header.Height(), cmTxs, resp.TxResults, mempool.PreCheckMaxBytes(maxBytes), mempool.PostCheckMaxGas(maxGas))
	if err != nil {
		return nil, 0, err
	}

	return resp.AppHash, uint64(commitResp.RetainHeight), err
}

// Validate validates the state and the block for the executor
func (c *ABCIExecutionClient) Validate(state types.State, header *types.SignedHeader, data *types.Data) error {
	if err := header.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid header: %w", err)
	}

	if err := data.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid data: %w", err)
	}

	if err := types.Validate(header, data); err != nil {
		return fmt.Errorf("invalid block: %w", err)
	}

	if err := c.validateVersions(state, header); err != nil {
		return err
	}

	if err := c.validateHeight(state, header); err != nil {
		return err
	}

	if err := c.validateHashes(state, header); err != nil {
		return err
	}

	return nil
}

func (c *ABCIExecutionClient) execute(ctx context.Context, state types.State, header *types.SignedHeader, data *types.Data) (*abci.ResponseFinalizeBlock, error) {
	// Only execute if the node hasn't already shut down
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	abciHeader, err := abciconv.ToABCIHeaderPB(&header.Header)
	if err != nil {
		return nil, err
	}
	abciHeader.ChainID = c.chainID
	abciBlock, err := abciconv.ToABCIBlock(header, data)
	if err != nil {
		return nil, err
	}

	startTime := time.Now().UnixNano()
	finalizeBlockResponse, err := c.proxyApp.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{
		Hash:               header.Hash(),
		NextValidatorsHash: state.Validators.Hash(),
		ProposerAddress:    abciHeader.ProposerAddress,
		Height:             abciHeader.Height,
		Time:               abciHeader.Time,
		DecidedLastCommit: abci.CommitInfo{
			Round: 0,
			Votes: nil,
		},
		Misbehavior: abciBlock.Evidence.Evidence.ToABCI(),
		Txs:         abciBlock.Txs.ToSliceOfBytes(),
	})
	endTime := time.Now().UnixNano()
	c.metrics.BlockProcessingTime.Observe(float64(endTime-startTime) / 1000000)
	if err != nil {
		c.logger.Error("error in proxyAppConn.FinalizeBlock", "err", err)
		return nil, err
	}

	c.logger.Info(
		"finalized block",
		"height", abciBlock.Height,
		"num_txs_res", len(finalizeBlockResponse.TxResults),
		"num_val_updates", len(finalizeBlockResponse.ValidatorUpdates),
		"block_app_hash", fmt.Sprintf("%X", finalizeBlockResponse.AppHash),
	)

	if len(abciBlock.Data.Txs) != len(finalizeBlockResponse.TxResults) {
		return nil, fmt.Errorf("expected tx results length to match size of transactions in block. Expected %d, got %d", len(data.Txs), len(finalizeBlockResponse.TxResults))
	}

	c.logger.Info("executed block", "height", abciHeader.Height, "app_hash", fmt.Sprintf("%X", finalizeBlockResponse.AppHash))

	return finalizeBlockResponse, nil
}

func (c *ABCIExecutionClient) publishEvents(resp *abci.ResponseFinalizeBlock, header *types.SignedHeader, data *types.Data, _ types.State) {
	if c.eventBus == nil {
		return
	}

	if err := c.publishBlockEvent(header, data, resp); err != nil {
		c.logger.Error("failed publishing block event", "err", err)
	}

	if err := c.publishBlockHeaderEvent(header, data); err != nil {
		c.logger.Error("failed publishing block header event", "err", err)
	}

	if err := c.publishBlockEventsData(header, data, resp); err != nil {
		c.logger.Error("failed publishing block events", "err", err)
	}

	abciBlock, err := abciconv.ToABCIBlock(header, data)
	if err != nil {
		c.logger.Error("failed to convert block for evidence check", "err", err)
		return
	}

	if len(abciBlock.Evidence.Evidence) > 0 {
		if err := c.publishEvidenceEvents(header, data); err != nil {
			c.logger.Error("failed publishing evidence events", "err", err)
		}
	}

	if err := c.publishTxEvents(header, data, resp); err != nil {
		c.logger.Error("failed publishing tx events", "err", err)
	}
}

func (c *ABCIExecutionClient) publishBlockEvent(header *types.SignedHeader, data *types.Data, resp *abci.ResponseFinalizeBlock) error {
	if c.eventBus == nil {
		return nil
	}

	abciBlock, err := abciconv.ToABCIBlock(header, data)
	if err != nil {
		return fmt.Errorf("failed to convert block: %w", err)
	}

	return c.eventBus.PublishEventNewBlock(cmtypes.EventDataNewBlock{
		Block: abciBlock,
		BlockID: cmtypes.BlockID{
			Hash: cmbytes.HexBytes(header.Hash()),
		},
		ResultFinalizeBlock: *resp,
	})
}

func (c *ABCIExecutionClient) publishBlockHeaderEvent(header *types.SignedHeader, data *types.Data) error {
	if c.eventBus == nil {
		return nil
	}

	abciBlock, err := abciconv.ToABCIBlock(header, data)
	if err != nil {
		return fmt.Errorf("failed to convert block header: %w", err)
	}

	return c.eventBus.PublishEventNewBlockHeader(cmtypes.EventDataNewBlockHeader{
		Header: abciBlock.Header,
	})
}

func (c *ABCIExecutionClient) publishBlockEventsData(header *types.SignedHeader, data *types.Data, resp *abci.ResponseFinalizeBlock) error {
	if c.eventBus == nil {
		return nil
	}

	abciBlock, err := abciconv.ToABCIBlock(header, data)
	if err != nil {
		return fmt.Errorf("failed to convert block events: %w", err)
	}

	return c.eventBus.PublishEventNewBlockEvents(cmtypes.EventDataNewBlockEvents{
		Height: abciBlock.Height,
		Events: resp.Events,
		NumTxs: int64(len(abciBlock.Txs)),
	})
}

func (c *ABCIExecutionClient) publishEvidenceEvents(header *types.SignedHeader, data *types.Data) error {
	if c.eventBus == nil {
		return nil
	}

	abciBlock, err := abciconv.ToABCIBlock(header, data)
	if err != nil {
		return fmt.Errorf("failed to convert block for evidence: %w", err)
	}

	for _, ev := range abciBlock.Evidence.Evidence {
		if err := c.eventBus.PublishEventNewEvidence(cmtypes.EventDataNewEvidence{
			Evidence: ev,
			Height:   int64(header.Header.Height()),
		}); err != nil {
			return fmt.Errorf("failed to publish evidence: %w", err)
		}
	}

	return nil
}

func (c *ABCIExecutionClient) publishTxEvents(header *types.SignedHeader, data *types.Data, resp *abci.ResponseFinalizeBlock) error {
	if c.eventBus == nil {
		return nil
	}

	abciBlock, err := abciconv.ToABCIBlock(header, data)
	if err != nil {
		return fmt.Errorf("failed to convert block for tx events: %w", err)
	}

	for i, tx := range abciBlock.Data.Txs {
		if err := c.eventBus.PublishEventTx(cmtypes.EventDataTx{
			TxResult: abci.TxResult{
				Height: abciBlock.Height,
				Index:  uint32(i),
				Tx:     tx,
				Result: *(resp.TxResults[i]),
			},
		}); err != nil {
			return fmt.Errorf("failed to publish tx event at index %d: %w", i, err)
		}
	}

	return nil
}

func toRollkitTxs(txs cmtypes.Txs) types.Txs {
	if len(txs) == 0 {
		return nil
	}

	rollkitTxs := make(types.Txs, len(txs))
	for i, tx := range txs {
		rollkitTxs[i] = types.Tx(tx)
	}
	return rollkitTxs
}

func fromRollkitTxs(txs types.Txs) []execution_types.Tx {
	if len(txs) == 0 {
		return nil
	}

	execTxs := make([]execution_types.Tx, len(txs))
	for i, tx := range txs {
		execTxs[i] = execution_types.Tx(tx)
	}
	return execTxs
}

func fromExecutionTxs(txs []execution_types.Tx) cmtypes.Txs {
	if len(txs) == 0 {
		return nil
	}

	cmTxs := make(cmtypes.Txs, len(txs))
	for i, tx := range txs {
		cmTxs[i] = cmtypes.Tx(tx)
	}
	return cmTxs
}

func wrapBlockError(height uint64, err error, msg string) error {
	return fmt.Errorf("failed to %s at height %d: %w", msg, height, err)
}

func wrapStateError(err error, msg string) error {
	return fmt.Errorf("failed to %s: %w", msg, err)
}

// Add these validation helper functions
func (c *ABCIExecutionClient) validateVersions(state types.State, header *types.SignedHeader) error {
	if header.Version.App != state.Version.Consensus.App ||
		header.Version.Block != state.Version.Consensus.Block {
		return fmt.Errorf("block version mismatch: got app=%d,block=%d want app=%d,block=%d",
			header.Version.App, header.Version.Block,
			state.Version.Consensus.App, state.Version.Consensus.Block)
	}
	return nil
}

func (c *ABCIExecutionClient) validateHeight(state types.State, header *types.SignedHeader) error {
	if state.LastBlockHeight <= 0 {
		if header.Height() != state.InitialHeight {
			return fmt.Errorf("initial block height mismatch: got %d, want %d",
				header.Height(), state.InitialHeight)
		}
		return nil
	}

	expectedHeight := state.LastBlockHeight + 1
	if header.Height() != expectedHeight {
		return fmt.Errorf("block height mismatch: got %d, want %d",
			header.Height(), expectedHeight)
	}
	return nil
}

func (c *ABCIExecutionClient) validateHashes(state types.State, header *types.SignedHeader) error {
	if !bytes.Equal(header.AppHash[:], state.AppHash[:]) {
		return fmt.Errorf("AppHash mismatch: got %X, want %X",
			header.AppHash, state.AppHash)
	}

	if !bytes.Equal(header.LastResultsHash[:], state.LastResultsHash[:]) {
		return fmt.Errorf("LastResultsHash mismatch: got %X, want %X",
			header.LastResultsHash, state.LastResultsHash)
	}
	return nil
}
