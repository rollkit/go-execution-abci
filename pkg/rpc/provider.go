package rpc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/mempool"
	"github.com/cometbft/cometbft/p2p"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/state/indexer"
	blockidxnull "github.com/cometbft/cometbft/state/indexer/block/null"
	"github.com/cometbft/cometbft/state/txindex"
	"github.com/cometbft/cometbft/state/txindex/null"
	cmttypes "github.com/cometbft/cometbft/types"
	cmtypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	execp2p "github.com/rollkit/go-execution-abci/pkg/p2p"
	rlktypes "github.com/rollkit/rollkit/types"
)

// ErrConsensusStateNotAvailable is returned because Rollkit doesn't use Tendermint consensus.
var ErrConsensusStateNotAvailable = errors.New("consensus state not available in Rollkit")

// RpcProvider implements the interfaces required by the JSON-RPC service,
// primarily by delegating calls to the underlying adapter and indexers.
type RpcProvider struct {
	adapter      *adapter.Adapter
	txIndexer    txindex.TxIndexer
	blockIndexer indexer.BlockIndexer
	logger       cmtlog.Logger
}

// NewRpcProvider creates a new instance of rpcProvider.
func NewRpcProvider(
	adapter *adapter.Adapter,
	txIndexer txindex.TxIndexer,
	blockIndexer indexer.BlockIndexer,
	logger cmtlog.Logger,
) *RpcProvider {
	return &RpcProvider{
		adapter:      adapter,
		txIndexer:    txIndexer,
		blockIndexer: blockIndexer,
		logger:       logger,
	}
}

// ABCIInfo implements client.CometRPC.
func (p *RpcProvider) ABCIInfo(context.Context) (*coretypes.ResultABCIInfo, error) {
	info, err := p.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIInfo{
		Response: *info,
	}, nil
}

// ABCIQuery implements client.CometRPC.
func (p *RpcProvider) ABCIQuery(ctx context.Context, path string, data cmtbytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	resp, err := p.adapter.App.Query(ctx, &abci.RequestQuery{
		Data: data,
		Path: path,
	})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{
		Response: *resp,
	}, nil
}

// ABCIQueryWithOptions implements client.CometRPC.
func (p *RpcProvider) ABCIQueryWithOptions(ctx context.Context, path string, data cmtbytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	resp, err := p.adapter.App.Query(ctx, &abci.RequestQuery{
		Data:   data,
		Path:   path,
		Height: opts.Height,
		Prove:  opts.Prove,
	})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{
		Response: *resp,
	}, nil
}

// Status implements client.CometRPC.
func (p *RpcProvider) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	info, err := p.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}

	s, err := p.adapter.LoadState(ctx)
	if err != nil {
		return nil, err
	}

	return &coretypes.ResultStatus{
		NodeInfo: p2p.DefaultNodeInfo{}, // TODO: fill this in
		SyncInfo: coretypes.SyncInfo{
			// LatestBlockHash:   cmtbytes.HexBytes(info.LastBlockAppHash), // TODO: fill this in  latestBlockMeta.BlockID.Hash
			LatestAppHash:     cmtbytes.HexBytes(info.LastBlockAppHash),
			LatestBlockHeight: info.LastBlockHeight,
			LatestBlockTime:   s.LastBlockTime,
		},
		ValidatorInfo: coretypes.ValidatorInfo{
			Address:     s.Validators.Proposer.Address,
			PubKey:      s.Validators.Proposer.PubKey,
			VotingPower: s.Validators.Proposer.VotingPower,
		},
	}, nil
}

// NetInfo implements client.Client.
func (p *RpcProvider) NetInfo(context.Context) (*coretypes.ResultNetInfo, error) {
	res := coretypes.ResultNetInfo{
		Listening: true,
	}
	for _, ma := range p.adapter.P2PClient.Addrs() {
		res.Listeners = append(res.Listeners, ma.String())
	}
	peers := p.adapter.P2PClient.Peers()
	res.NPeers = len(peers)
	for _, peer := range peers {
		res.Peers = append(res.Peers, coretypes.Peer{
			NodeInfo: p2p.DefaultNodeInfo{
				DefaultNodeID: p2p.ID(peer.NodeInfo.NodeID),
				ListenAddr:    peer.NodeInfo.ListenAddr,
				Network:       peer.NodeInfo.Network,
			},
			IsOutbound: peer.IsOutbound,
			RemoteIP:   peer.RemoteIP,
		})
	}

	return &res, nil
}

// Health implements client.Client.
func (p *RpcProvider) Health(context.Context) (*coretypes.ResultHealth, error) {
	return &coretypes.ResultHealth{}, nil
}

// ConsensusParams implements client.Client.
func (p *RpcProvider) ConsensusParams(ctx context.Context, height *int64) (*coretypes.ResultConsensusParams, error) {
	state, err := p.adapter.LoadState(ctx)
	if err != nil {
		return nil, err
	}
	params := state.ConsensusParams
	// Use normalizeHeight which should be moved to rpcProvider as well
	normalizedHeight := p.normalizeHeight(height) // Changed r.normalizeHeight to p.normalizeHeight
	return &coretypes.ResultConsensusParams{
		BlockHeight: int64(normalizedHeight), //nolint:gosec
		ConsensusParams: cmttypes.ConsensusParams{
			Block: cmttypes.BlockParams{
				MaxBytes: params.Block.MaxBytes,
				MaxGas:   params.Block.MaxGas,
			},
			Evidence: cmttypes.EvidenceParams{
				MaxAgeNumBlocks: params.Evidence.MaxAgeNumBlocks,
				MaxAgeDuration:  params.Evidence.MaxAgeDuration,
				MaxBytes:        params.Evidence.MaxBytes,
			},
			Validator: cmttypes.ValidatorParams{
				PubKeyTypes: params.Validator.PubKeyTypes,
			},
			Version: cmttypes.VersionParams{
				App: params.Version.App,
			},
		},
	}, nil
}

// ConsensusState implements client.Client.
func (p *RpcProvider) ConsensusState(context.Context) (*coretypes.ResultConsensusState, error) {
	return nil, ErrConsensusStateNotAvailable // Assuming ErrConsensusStateNotAvailable is defined in rpc package or imported
}

// DumpConsensusState implements client.Client.
func (p *RpcProvider) DumpConsensusState(context.Context) (*coretypes.ResultDumpConsensusState, error) {
	return nil, ErrConsensusStateNotAvailable // Assuming ErrConsensusStateNotAvailable is defined in rpc package or imported
}

// Genesis implements client.Client.
func (p *RpcProvider) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	// Returning unimplemented as per the original code.
	// Consider implementing or returning a more specific error if needed.
	panic("unimplemented")
}

// GenesisChunked implements client.Client.
func (p *RpcProvider) GenesisChunked(context.Context, uint) (*coretypes.ResultGenesisChunk, error) {
	return nil, errors.New("GenesisChunked RPC method is not yet implemented")
}

// Block implements client.CometRPC.
func (p *RpcProvider) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	var heightValue uint64

	switch {
	// block tag = included
	case height != nil && *height == -1:
		// heightValue = p.adapter.store.GetDAIncludedHeight()
		// TODO: implement
		return nil, errors.New("DA included height not implemented")
	default:
		heightValue = p.normalizeHeight(height)
	}
	header, data, err := p.adapter.Store.GetBlockData(ctx, heightValue)
	if err != nil {
		return nil, err
	}

	hash := header.Hash()
	abciBlock, err := ToABCIBlock(header, data) // Assumes ToABCIBlock is accessible (e.g., in utils.go)
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultBlock{
		BlockID: cmttypes.BlockID{
			Hash: cmtbytes.HexBytes(hash),
			PartSetHeader: cmttypes.PartSetHeader{
				Total: 0,
				Hash:  nil,
			},
		},
		Block: abciBlock,
	}, nil
}

// BlockByHash implements client.CometRPC.
func (p *RpcProvider) BlockByHash(ctx context.Context, hash []byte) (*coretypes.ResultBlock, error) {
	header, data, err := p.adapter.Store.GetBlockByHash(ctx, rlktypes.Hash(hash)) // Used types.Hash from rollkit/types
	if err != nil {
		return nil, err
	}

	abciBlock, err := ToABCIBlock(header, data) // Assumes ToABCIBlock is accessible (e.g., in utils.go)
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultBlock{
		BlockID: cmttypes.BlockID{
			Hash: cmtbytes.HexBytes(hash),
			PartSetHeader: cmttypes.PartSetHeader{
				Total: 0,
				Hash:  nil,
			},
		},
		Block: abciBlock,
	}, nil
}

// BlockResults implements client.CometRPC.
func (p *RpcProvider) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	// Currently, this method returns an empty result as the original implementation was commented out.
	// If block results become available, this implementation should be updated.
	_ = p.normalizeHeight(height) // Use height to avoid unused variable error, logic depends on future implementation
	return &coretypes.ResultBlockResults{}, nil
	// Original commented-out logic:
	// var h uint64
	// if height == nil {
	// 	h = p.adapter.Store.Height(ctx)
	// } else {
	// 	h = uint64(*height)
	// }
	// header, _, err := p.adapter.Store.GetBlockData(ctx, h)
	// if err != nil {
	// 	return nil, err
	// }
	// resp, err := p.adapter.Store.GetBlockResponses(ctx, h)
	// if err != nil {
	// 	return nil, err
	// }
	// return &coretypes.ResultBlockResults{
	// 	Height:                int64(h), //nolint:gosec
	// 	TxsResults:            resp.TxResults,
	// 	FinalizeBlockEvents:   resp.Events,
	// 	ValidatorUpdates:      resp.ValidatorUpdates,
	// 	ConsensusParamUpdates: resp.ConsensusParamUpdates,
	// 	AppHash:               header.Header.AppHash,
	// }, nil
}

// Commit implements client.CometRPC.
func (p *RpcProvider) Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error) {
	heightValue := p.normalizeHeight(height)
	header, data, err := p.adapter.Store.GetBlockData(ctx, heightValue)
	if err != nil {
		return nil, err
	}

	// we should have a single validator
	if len(header.ProposerAddress) == 0 {
		return nil, errors.New("empty validator set found in block")
	}

	commit := getABCICommit(heightValue, header.Hash(), header.ProposerAddress, header.Time(), header.Signature) // Assumes getABCICommit is accessible (e.g., in utils.go)

	block, err := ToABCIBlock(header, data) // Assumes ToABCIBlock is accessible (e.g., in utils.go)
	if err != nil {
		return nil, err
	}

	return coretypes.NewResultCommit(&block.Header, commit, true), nil
}

// Header implements client.Client.
func (p *RpcProvider) Header(ctx context.Context, heightPtr *int64) (*coretypes.ResultHeader, error) {
	height := p.normalizeHeight(heightPtr)
	blockMeta := p.getBlockMeta(ctx, height)
	if blockMeta == nil {
		return nil, fmt.Errorf("block at height %d not found", height)
	}
	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// HeaderByHash implements client.Client.
func (p *RpcProvider) HeaderByHash(ctx context.Context, hash cmtbytes.HexBytes) (*coretypes.ResultHeader, error) {
	// N.B. The hash parameter is HexBytes so that the reflective parameter
	// decoding logic in the HTTP service will correctly translate from JSON.
	// See https://github.com/cometbft/cometbft/issues/6802 for context.

	header, data, err := p.adapter.Store.GetBlockByHash(ctx, rlktypes.Hash(hash)) // Used types.Hash from rollkit/types
	if err != nil {
		return nil, err
	}

	blockMeta, err := ToABCIBlockMeta(header, data) // Assumes ToABCIBlockMeta is accessible (e.g., in utils.go)
	if err != nil {
		return nil, err
	}

	if blockMeta == nil {
		// Return empty result without error if block not found by hash, consistent with original behaviour?
		// Or return an error? fmt.Errorf("block with hash %X not found", hash)
		return &coretypes.ResultHeader{}, nil // Current behaviour matches original code
	}

	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// BlockchainInfo implements client.CometRPC.
func (p *RpcProvider) BlockchainInfo(ctx context.Context, minHeight int64, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	const limit int64 = 20 // Default limit used in the original code

	height, err := p.adapter.Store.Height(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current height: %w", err)
	}

	// Assuming base height is 1 as blocks are 1-indexed, adjust if base is different.
	// The original filterMinMax used 0, but blockchain heights typically start at 1.
	const baseHeight int64 = 1
	minHeight, maxHeight, err = filterMinMax(
		baseHeight,
		int64(height),
		minHeight,
		maxHeight,
		limit,
	) // Assumes filterMinMax is accessible (e.g., in utils.go)
	if err != nil {
		return nil, err
	}

	blocks := make([]*cmttypes.BlockMeta, 0, maxHeight-minHeight+1)
	for h := maxHeight; h >= minHeight; h-- {
		// Use getBlockMeta which handles errors and nil checks internally
		bMeta := p.getBlockMeta(ctx, uint64(h))
		if bMeta != nil {
			blocks = append(blocks, bMeta)
		}
		// Decide if we should continue or return error if getBlockMeta fails for a height in range.
		// Current behaviour: skip the block if meta retrieval fails.
	}

	// Re-fetch height in case new blocks were added during the loop?
	// The original code did this.
	finalHeight, err := p.adapter.Store.Height(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get final height: %w", err)
	}

	return &coretypes.ResultBlockchainInfo{
		LastHeight: int64(finalHeight), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}

// BlockSearch implements client.CometRPC.
func (p *RpcProvider) BlockSearch(ctx context.Context, query string, pagePtr *int, perPagePtr *int, orderBy string) (*coretypes.ResultBlockSearch, error) {
	// skip if block indexing is disabled
	if _, ok := p.blockIndexer.(*blockidxnull.BlockerIndexer); ok {
		return nil, errors.New("block indexing is disabled")
	}

	if len(query) > maxQueryLength { // Assumes maxQueryLength is accessible (e.g., defined in rpc package)
		return nil, errors.New("maximum query length exceeded")
	}

	q, err := cmtquery.New(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	results, err := p.blockIndexer.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("block search failed: %w", err)
	}

	// sort results (must be done before pagination)
	switch orderBy {
	case "desc", "":
		sort.Slice(results, func(i, j int) bool { return results[i] > results[j] })

	case "asc":
		sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })

	default:
		return nil, errors.New("expected order_by to be either `asc` or `desc` or empty")
	}

	// paginate results
	totalCount := len(results)
	perPage := validatePerPage(perPagePtr) // Assumes validatePerPage is accessible (e.g., in utils.go)

	page, err := validatePage(pagePtr, perPage, totalCount) // Assumes validatePage is accessible (e.g., in utils.go)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(page, perPage) // Assumes validateSkipCount is accessible (e.g., in utils.go)
	pageSize := cmtmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*coretypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		height := uint64(results[i])
		header, data, err := p.adapter.Store.GetBlockData(ctx, height)
		if err != nil {
			// If a block referenced by indexer is missing, should we error out or just skip?
			// For now, error out.
			return nil, fmt.Errorf("failed to get block data for height %d from store: %w", height, err)
		}
		if header == nil || data == nil {
			return nil, fmt.Errorf("nil header or data for height %d from store", height)
		}
		block, err := ToABCIBlock(header, data) // Assumes ToABCIBlock is accessible (e.g., in utils.go)
		if err != nil {
			return nil, fmt.Errorf("failed to convert block at height %d to ABCI block: %w", height, err)
		}
		apiResults = append(apiResults, &coretypes.ResultBlock{
			Block: block,
			BlockID: cmttypes.BlockID{
				Hash: block.Hash(),
			},
		})
	}

	return &coretypes.ResultBlockSearch{Blocks: apiResults, TotalCount: totalCount}, nil
}

// BroadcastTxAsync implements client.CometRPC.
func (p *RpcProvider) BroadcastTxAsync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := p.adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
		responseCh <- response
	}, mempool.TxInfo{})
	if err != nil {
		return nil, fmt.Errorf("error during CheckTx: %w", err)
	}

	// Wait for the callback to be called
	select {
	case res = <-responseCh:
		// Successfully received CheckTx response
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for CheckTx response: %w", ctx.Err())
	}

	// Note: Original code didn't gossip on async. If gossiping is desired here,
	// it should be added similarly to BroadcastTxSync, potentially after checking res.Code.

	return &coretypes.ResultBroadcastTx{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Codespace: res.Codespace,
		Hash:      tx.Hash(),
	}, nil
}

// BroadcastTxCommit implements client.CometRPC.
func (p *RpcProvider) BroadcastTxCommit(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	// This implementation corresponds to Tendermint's implementation from rpc/core/mempool.go.
	// ctx.RemoteAddr godoc: If neither HTTPReq nor WSConn is set, an empty string is returned.
	// This code is a local client, so we can assume that subscriber is ""
	// TODO: Check if assuming empty subscriber is always correct for this context.
	subscriber := "" // ctx.RemoteAddr()

	if p.adapter.EventBus == nil {
		return nil, errors.New("event bus is not configured, cannot subscribe to events")
	}

	// Use CometBFT config values directly if available
	maxSubs := p.adapter.CometCfg.RPC.MaxSubscriptionsPerClient
	maxClients := p.adapter.CometCfg.RPC.MaxSubscriptionClients
	commitTimeout := p.adapter.CometCfg.RPC.TimeoutBroadcastTxCommit

	if p.adapter.EventBus.NumClients() >= maxClients {
		return nil, fmt.Errorf("max_subscription_clients %d reached", maxClients)
	} else if p.adapter.EventBus.NumClientSubscriptions(subscriber) >= maxSubs {
		return nil, fmt.Errorf("max_subscriptions_per_client %d reached", maxSubs)
	}

	// Subscribe to tx being committed in block.
	subCtx, cancel := context.WithTimeout(ctx, subscribeTimeout) // Assumes subscribeTimeout is accessible (e.g., defined in rpc package)
	defer cancel()
	q := cmttypes.EventQueryTxFor(tx)
	deliverTxSub, err := p.adapter.EventBus.Subscribe(subCtx, subscriber, q)
	if err != nil {
		err = fmt.Errorf("failed to subscribe to tx: %w", err)
		p.logger.Error("Error on broadcast_tx_commit", "err", err)
		return nil, err
	}
	defer func() {
		if err := p.adapter.EventBus.Unsubscribe(ctx, subscriber, q); err != nil {
			p.logger.Error("Error unsubscribing from eventBus", "err", err)
		}
	}()

	// Add to mempool and wait for CheckTx result
	checkTxResCh := make(chan *abci.ResponseCheckTx, 1)
	err = p.adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-ctx.Done():
			// Context cancelled before CheckTx completed
			p.logger.Error("Context cancelled during CheckTx in BroadcastTxCommit")
			return
		case checkTxResCh <- res:
		}
	}, mempool.TxInfo{})
	if err != nil {
		p.logger.Error("Error on CheckTx in BroadcastTxCommit", "err", err)
		return nil, fmt.Errorf("error on CheckTx in broadcastTxCommit: %w", err)
	}

	var checkTxRes *abci.ResponseCheckTx
	select {
	case checkTxRes = <-checkTxResCh:
		// Got CheckTx response
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for CheckTx response: %w", ctx.Err())
	}

	if checkTxRes.Code != abci.CodeTypeOK {
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{}, // Included for consistency, though tx didn't make it to a block
			Hash:     tx.Hash(),
		}, nil // Return nil error as CheckTx failed, this is the expected result structure
	}

	// Broadcast tx gossip only if CheckTx passed
	if p.adapter.TxGossiper == nil {
		return nil, execp2p.ErrNotReady // Cannot gossip if gossiper is nil
	}
	err = p.adapter.TxGossiper.Publish(ctx, tx)
	if err != nil {
		// Note: If gossiping fails, the tx is still in the local mempool and subscribed.
		// Tendermint's original behaviour might differ here. Consider if tx should be removed from mempool.
		p.logger.Error("tx added to local mempool but failed to broadcast", "err", err)
		return nil, fmt.Errorf("tx added to local mempool but failure to broadcast: %w", err)
	}

	// Wait for the tx to be included in a block or timeout.
	select {
	case msg, ok := <-deliverTxSub.Out():
		if !ok {
			// Channel closed, possibly due to unsubscribe or EventBus shutdown
			err = fmt.Errorf("subscription channel closed unexpectedly: %w", deliverTxSub.Err())
			p.logger.Error("Error on broadcastTxCommit", "err", err)
			// Return the CheckTx result as the tx wasn't confirmed in a block
			return &coretypes.ResultBroadcastTxCommit{
				CheckTx:  *checkTxRes,
				TxResult: abci.ExecTxResult{},
				Hash:     tx.Hash(),
			}, err
		}
		deliverTxRes, ok := msg.Data().(cmttypes.EventDataTx)
		if !ok {
			err = fmt.Errorf("unexpected event data type: got %T, expected %T", msg.Data(), cmttypes.EventDataTx{})
			p.logger.Error("Error on broadcastTxCommit", "err", err)
			return &coretypes.ResultBroadcastTxCommit{
				CheckTx:  *checkTxRes,
				TxResult: abci.ExecTxResult{},
				Hash:     tx.Hash(),
			}, err
		}
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: deliverTxRes.Result,
			Hash:     tx.Hash(),
			Height:   deliverTxRes.Height,
		}, nil
	case <-deliverTxSub.Canceled():
		var reason string
		if deliverTxSub.Err() == nil {
			reason = "node exited"
		} else {
			reason = deliverTxSub.Err().Error()
		}
		err = fmt.Errorf("subscription was cancelled (reason: %s)", reason)
		p.logger.Error("Error on broadcastTxCommit", "err", err)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-time.After(commitTimeout):
		err = errors.New("timed out waiting for tx to be included in a block")
		p.logger.Error("Error on broadcastTxCommit", "err", err)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-ctx.Done():
		// Parent context cancelled
		err = fmt.Errorf("context cancelled while waiting for tx commit event: %w", ctx.Err())
		p.logger.Error("Error on broadcastTxCommit", "err", err)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	}
}

// BroadcastTxSync implements client.CometRPC.
func (p *RpcProvider) BroadcastTxSync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	resCh := make(chan *abci.ResponseCheckTx, 1)
	err := p.adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-ctx.Done():
			return
		case resCh <- res:
		}
	}, mempool.TxInfo{})
	if err != nil {
		return nil, fmt.Errorf("error during CheckTx: %w", err)
	}

	var res *abci.ResponseCheckTx
	select {
	case res = <-resCh:
		// Got response
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled waiting for CheckTx: %w", ctx.Err())
	}

	// Gossip the transaction if it passed CheckTx.
	if res.Code == abci.CodeTypeOK {
		if p.adapter.TxGossiper == nil {
			return nil, execp2p.ErrNotReady // Cannot gossip if gossiper is not ready
		}

		err = p.adapter.TxGossiper.Publish(ctx, tx)
		if err != nil {
			// If gossiping fails, remove the tx from the mempool to allow resubmission.
			// This matches the behaviour described in the original comments.
			rmErr := p.adapter.Mempool.RemoveTxByKey(tx.Key())
			if rmErr != nil {
				// Log if removal also failed, but return the gossip error primarily.
				p.logger.Error("Failed to remove tx from mempool after gossip failure", "tx_key", tx.Key(), "removal_error", rmErr)
			}
			return nil, fmt.Errorf("failed to gossip tx: %w", err)
		}
	}

	return &coretypes.ResultBroadcastTx{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Codespace: res.Codespace,
		Hash:      tx.Hash(),
	}, nil
}

// CheckTx implements client.Client.
func (p *RpcProvider) CheckTx(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultCheckTx, error) {
	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := p.adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
		select {
		case <-ctx.Done():
			return
		case responseCh <- response:
		}
	}, mempool.TxInfo{})
	if err != nil {
		return nil, fmt.Errorf("error submitting tx to mempool CheckTx: %w", err)
	}

	// Wait for the callback to be called or context cancellation
	select {
	case res = <-responseCh:
		return &coretypes.ResultCheckTx{ResponseCheckTx: *res}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for CheckTx response: %w", ctx.Err())
	}
}

// Tx implements client.CometRPC.
func (p *RpcProvider) Tx(ctx context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error) {
	// Check if tx indexing is disabled
	if p.txIndexer == nil {
		return nil, fmt.Errorf("transaction indexing is disabled")
	}
	if _, ok := p.txIndexer.(*null.TxIndex); ok {
		return nil, fmt.Errorf("transaction indexing is disabled")
	}

	txResult, err := p.txIndexer.Get(hash)
	if err != nil {
		// If the tx is not found, return nil result without error, maintaining behaviour.
		// This differs from some Tendermint versions that might return an error.
		// TODO: Consider aligning error handling with target Tendermint version if necessary.
		// A more robust check might involve type asserting the error if txIndexer returns a specific "not found" error type.
		if txResult == nil { // Heuristic check if error implies not found
			return nil, nil
		}
		// Return other errors encountered during Get
		return nil, fmt.Errorf("error getting tx from indexer: %w", err)
	}

	if txResult == nil {
		// Tx not found
		return nil, nil
	}

	var proof cmttypes.TxProof
	if prove {
		// Proof generation is currently not supported.
		// When supported, logic to retrieve block and compute proof would go here.
		// Example (requires block fetching and a Prove method on block.Data.Txs):
		/*
			blockRes, err := p.Block(ctx, &txResult.Height)
			if err != nil {
				return nil, fmt.Errorf("failed to get block %d for proof: %w", txResult.Height, err)
			}
			if blockRes == nil || blockRes.Block == nil {
				return nil, fmt.Errorf("block %d not found for proof", txResult.Height)
			}
			// Assuming blockRes.Block.Data.Txs implements some `Proof(index)` method
			proof = blockRes.Block.Data.Txs.Proof(int(txResult.Index))
		*/
		return nil, errors.New("transaction proof generation is not supported") // Return error as proofs aren't supported
	}

	return &coretypes.ResultTx{
		Hash:     hash,
		Height:   txResult.Height,
		Index:    txResult.Index,
		TxResult: txResult.Result,
		Tx:       txResult.Tx,
		Proof:    proof, // Will be empty if prove is false or unsupported
	}, nil
}

// TxSearch implements client.CometRPC.
func (p *RpcProvider) TxSearch(ctx context.Context, query string, prove bool, pagePtr *int, perPagePtr *int, orderBy string) (*coretypes.ResultTxSearch, error) {
	// Check if tx indexing is disabled
	if p.txIndexer == nil {
		return nil, fmt.Errorf("transaction indexing is disabled")
	}
	if _, ok := p.txIndexer.(*null.TxIndex); ok {
		return nil, fmt.Errorf("transaction indexing is disabled")
	}

	if len(query) > maxQueryLength { // Assumes maxQueryLength is accessible
		return nil, fmt.Errorf("maximum query length exceeded")
	}

	q, err := cmtquery.New(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	txResults, err := p.txIndexer.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("error searching txs: %w", err)
	}

	// Sort results if orderBy is specified.
	switch orderBy {
	case "asc":
		sort.Slice(txResults, func(i, j int) bool {
			if txResults[i].Height == txResults[j].Height {
				return txResults[i].Index < txResults[j].Index
			}
			return txResults[i].Height < txResults[j].Height
		})
	case "desc":
		sort.Slice(txResults, func(i, j int) bool {
			if txResults[i].Height == txResults[j].Height {
				return txResults[i].Index > txResults[j].Index
			}
			return txResults[i].Height > txResults[j].Height
		})
	case "":
		// Default sorting behaviour (usually by height ascending, index ascending)
		// TxIndexer might already return sorted results, but explicitly sort for consistency.
		sort.Slice(txResults, func(i, j int) bool {
			if txResults[i].Height == txResults[j].Height {
				return txResults[i].Index < txResults[j].Index
			}
			return txResults[i].Height < txResults[j].Height
		})
	default:
		return nil, fmt.Errorf("invalid order_by: %s", orderBy)
	}

	// Paginate results.
	totalCount := len(txResults)
	perPage := validatePerPage(perPagePtr)                  // Assumes validatePerPage is accessible
	page, err := validatePage(pagePtr, perPage, totalCount) // Assumes validatePage is accessible
	if err != nil {
		return nil, err
	}
	skipCount := validateSkipCount(page, perPage) // Assumes validateSkipCount is accessible
	pageSize := cmtmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*coretypes.ResultTx, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		result := txResults[i]
		var proof cmttypes.TxProof
		if prove {
			// Proof generation is currently not supported.
			return nil, errors.New("transaction proof generation is not supported")
			/* // Placeholder for future proof generation logic
			blockRes, err := p.Block(ctx, &result.Height)
			if err != nil {
				return nil, fmt.Errorf("failed to get block %d for proof: %w", result.Height, err)
			}
			if blockRes == nil || blockRes.Block == nil {
				return nil, fmt.Errorf("block %d not found for proof", result.Height)
			}
			// Assuming blockRes.Block.Data.Txs implements Proof(index)
			proof = blockRes.Block.Data.Txs.Proof(int(result.Index))
			*/
		}

		apiResults = append(apiResults, &coretypes.ResultTx{
			Hash:     cmttypes.Tx(result.Tx).Hash(), // Correctly calculate hash from []byte
			Height:   result.Height,
			Index:    result.Index,
			TxResult: result.Result,
			Tx:       result.Tx,
			Proof:    proof, // Will be empty
		})
	}

	return &coretypes.ResultTxSearch{
		Txs:        apiResults,
		TotalCount: totalCount,
	}, nil
}

// NumUnconfirmedTxs implements client.Client.
func (p *RpcProvider) NumUnconfirmedTxs(context.Context) (*coretypes.ResultUnconfirmedTxs, error) {
	return &coretypes.ResultUnconfirmedTxs{
		Count:      p.adapter.Mempool.Size(),
		Total:      p.adapter.Mempool.Size(), // Assuming Total means current size
		TotalBytes: p.adapter.Mempool.SizeBytes(),
	}, nil
}

// UnconfirmedTxs implements client.CometRPC.
func (p *RpcProvider) UnconfirmedTxs(ctx context.Context, limitPtr *int) (*coretypes.ResultUnconfirmedTxs, error) {
	txs := p.adapter.Mempool.ReapMaxTxs(-1) // Reap all transactions

	limit := len(txs)
	if limitPtr != nil {
		limit = cmtmath.MinInt(*limitPtr, limit)
		if limit < 0 {
			limit = 0
		}
	}
	paginatedTxs := txs[:limit]

	return &coretypes.ResultUnconfirmedTxs{
		Count:      len(paginatedTxs),
		Total:      p.adapter.Mempool.Size(),
		TotalBytes: p.adapter.Mempool.SizeBytes(),
		Txs:        paginatedTxs,
	}, nil
}

// Validators implements client.CometRPC.
func (p *RpcProvider) Validators(ctx context.Context, heightPtr *int64, pagePtr *int, perPagePtr *int) (*coretypes.ResultValidators, error) {
	// Determine the height to query validators for.
	// If height is nil or latest, use the current block height.
	// Otherwise, use the specified height (if state for that height is available).
	// Note: Loading state for arbitrary past heights might not be supported
	// depending on state pruning. The current implementation implicitly loads latest state.
	height := p.normalizeHeight(heightPtr)

	s, err := p.adapter.LoadState(ctx) // Loads the *latest* state
	if err != nil {
		return nil, fmt.Errorf("failed to load current state: %w", err)
	}

	// Check if the requested height matches the loaded state height if a specific height was requested.
	// If state history is not kept, this check might be necessary or always fail for past heights.
	if heightPtr != nil && int64(height) != s.LastBlockHeight {
		// This implies state for the requested height is not available with the current LoadState method.
		// Adjust implementation if historical state access is possible and needed.
		return nil, fmt.Errorf("validator set for height %d is not available, latest height is %d", height, s.LastBlockHeight)
	}

	validators := s.Validators.Validators
	totalCount := len(validators)

	// Handle pagination
	perPage := validatePerPage(perPagePtr)                  // Assumes validatePerPage is accessible (e.g., in utils.go)
	page, err := validatePage(pagePtr, perPage, totalCount) // Assumes validatePage is accessible (e.g., in utils.go)
	if err != nil {
		return nil, err
	}

	start := validateSkipCount(page, perPage) // Assumes validateSkipCount is accessible (e.g., in utils.go)
	end := start + perPage
	if end > totalCount {
		end = totalCount
	}

	// Ensure start index is not out of bounds, can happen if page * perPage > totalCount
	if start >= totalCount {
		validators = []*cmttypes.Validator{} // Return empty slice if page is out of range
	} else {
		validators = validators[start:end]
	}

	return &coretypes.ResultValidators{
		BlockHeight: s.LastBlockHeight, // Return the height for which the validator set is valid (latest)
		Validators:  validators,
		Total:       totalCount, // Total number of validators *before* pagination
	}, nil
}

// BroadcastEvidence implements client.Client but is essentially a no-op in this context,
// as Rollkit doesn't handle evidence in the same way as Tendermint.
// It returns a successful response with the evidence hash, mimicking Tendermint's behaviour
// without actually processing or storing the evidence.
func (p *RpcProvider) BroadcastEvidence(_ context.Context, evidence cmttypes.Evidence) (*coretypes.ResultBroadcastEvidence, error) {
	// Log that evidence broadcasting is not supported or is a no-op?
	p.logger.Debug("BroadcastEvidence called, but evidence handling is not implemented in Rollkit RPC.")
	return &coretypes.ResultBroadcastEvidence{
		Hash: evidence.Hash(),
	}, nil
}

// Subscribe implements client.Client.
// This functionality is currently not implemented.
func (p *RpcProvider) Subscribe(ctx context.Context, subscriber string, query string, outCapacity ...int) (out <-chan coretypes.ResultEvent, err error) {
	// Check if EventBus is available
	if p.adapter.EventBus == nil {
		return nil, errors.New("event bus is not configured, cannot subscribe to events")
	}
	// TODO: Implement subscription logic using p.adapter.EventBus
	// Example structure (needs actual implementation):
	// q, err := cmtquery.New(query)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to parse query: %w", err)
	// }
	// sub, err := p.adapter.EventBus.Subscribe(ctx, subscriber, q, outCapacity...)
	// if err != nil {
	// 	 return nil, fmt.Errorf("failed to subscribe: %w", err)
	// }
	// // Need a way to convert EventBus messages to coretypes.ResultEvent
	// outChan := make(chan coretypes.ResultEvent, ...) // Use appropriate capacity
	// go func() {
	// 	 for msg := range sub.Out() {
	// 		 // Conversion logic here
	// 		 outChan <- coretypes.ResultEvent{Query: query, Data: msg.Data(), Events: msg.Events()}
	// 	 }
	// 	 close(outChan)
	// }()
	// return outChan, nil
	return nil, errors.New("event subscription functionality is not yet implemented")
}

// Unsubscribe implements client.Client.
// This functionality is currently not implemented.
func (p *RpcProvider) Unsubscribe(ctx context.Context, subscriber string, query string) error {
	if p.adapter.EventBus == nil {
		return errors.New("event bus is not configured, cannot unsubscribe")
	}
	// TODO: Implement unsubscription logic using p.adapter.EventBus
	// Example structure:
	// q, err := cmtquery.New(query)
	// if err != nil {
	// 	return fmt.Errorf("failed to parse query: %w", err)
	// }
	// return p.adapter.EventBus.Unsubscribe(ctx, subscriber, q)
	return errors.New("event unsubscription functionality is not yet implemented")
}

// UnsubscribeAll implements client.Client.
// This functionality is currently not implemented.
func (p *RpcProvider) UnsubscribeAll(ctx context.Context, subscriber string) error {
	if p.adapter.EventBus == nil {
		return errors.New("event bus is not configured, cannot unsubscribe")
	}
	// TODO: Implement unsubscribe all logic using p.adapter.EventBus
	// return p.adapter.EventBus.UnsubscribeAll(ctx, subscriber)
	return errors.New("event unsubscribe all functionality is not yet implemented")
}

// ----------------------------------------------------------------------------
// Helper methods

func (p *RpcProvider) normalizeHeight(height *int64) uint64 {
	var heightValue uint64
	if height == nil {
		var err error
		// TODO: Decide how to handle context here. Using background for now.
		heightValue, err = p.adapter.Store.Height(context.Background())
		if err != nil {
			// TODO: Consider logging or returning error
			p.logger.Error("Failed to get current height in normalizeHeight", "err", err)
			return 0
		}
	} else if *height < 0 {
		// Handle negative heights if they have special meaning (e.g., -1 for latest)
		// Currently, just treat them as 0 or latest, adjust as needed.
		// For now, let's assume negative height means latest valid height.
		var err error
		heightValue, err = p.adapter.Store.Height(context.Background())
		if err != nil {
			p.logger.Error("Failed to get current height for negative height in normalizeHeight", "err", err)
			return 0
		}
	} else {
		heightValue = uint64(*height)
	}

	return heightValue
}

func (p *RpcProvider) getBlockMeta(ctx context.Context, n uint64) *cmtypes.BlockMeta {
	header, data, err := p.adapter.Store.GetBlockData(ctx, n)
	if err != nil {
		p.logger.Error("Failed to get block data in getBlockMeta", "height", n, "err", err)
		return nil
	}
	// Handle case where GetBlockData might return nil header/data without error?
	if header == nil || data == nil {
		p.logger.Error("Nil header or data returned from GetBlockData", "height", n)
		return nil
	}
	bmeta, err := ToABCIBlockMeta(header, data) // Assumes ToABCIBlockMeta is accessible (e.g., in utils.go)
	if err != nil {
		p.logger.Error("Failed to convert block to ABCI block meta", "height", n, "err", err)
		return nil
	}

	return bmeta
}
