package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
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
	"github.com/cosmos/cosmos-sdk/client"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	"github.com/rs/cors"
	"golang.org/x/net/netutil"

	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/adapter"
	execp2p "github.com/rollkit/go-execution-abci/p2p"
	"github.com/rollkit/go-execution-abci/rpc/json"
)

const (
	// maxQueryLength is the maximum length of a query string that will be
	// accepted. This is just a safety check to avoid outlandish queries.
	maxQueryLength = 512

	defaultPerPage = 30
	maxPerPage     = 100
)

var (
	// ErrConsensusStateNotAvailable is returned because Rollkit doesn't use Tendermint consensus.
	ErrConsensusStateNotAvailable = errors.New("consensus state not available in Rollkit")

	subscribeTimeout = 5 * time.Second
)

type RPCServer struct {
	adapter      *adapter.Adapter
	txIndexer    txindex.TxIndexer
	blockIndexer indexer.BlockIndexer
	config       *cmtcfg.RPCConfig
	server       http.Server
	logger       cmtlog.Logger
}

// Start implements client.Client.
func (r *RPCServer) Start() error {
	return r.startRPC()
}

func (r *RPCServer) startRPC() error {
	if r.config.ListenAddress == "" {
		r.logger.Info("Listen address not specified - RPC will not be exposed")
		return nil
	}
	parts := strings.SplitN(r.config.ListenAddress, "://", 2)
	if len(parts) != 2 {
		return errors.New("invalid RPC listen address: expecting tcp://host:port")
	}
	proto := parts[0]
	addr := parts[1]

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return err
	}

	if r.config.MaxOpenConnections != 0 {
		r.logger.Debug("limiting number of connections", "limit", r.config.MaxOpenConnections)
		listener = netutil.LimitListener(listener, r.config.MaxOpenConnections)
	}

	handler, err := json.GetHTTPHandler(r, r.logger)
	if err != nil {
		return err
	}

	if r.config.IsCorsEnabled() {
		r.logger.Debug("CORS enabled",
			"origins", r.config.CORSAllowedOrigins,
			"methods", r.config.CORSAllowedMethods,
			"headers", r.config.CORSAllowedHeaders,
		)
		c := cors.New(cors.Options{
			AllowedOrigins: r.config.CORSAllowedOrigins,
			AllowedMethods: r.config.CORSAllowedMethods,
			AllowedHeaders: r.config.CORSAllowedHeaders,
		})
		handler = c.Handler(handler)
	}

	go func() {
		err := r.serve(listener, handler)
		if !errors.Is(err, http.ErrServerClosed) {
			r.logger.Error("error while serving HTTP", "error", err)
		}
	}()

	return nil
}

func (r *RPCServer) serve(listener net.Listener, handler http.Handler) error {
	r.logger.Info("serving HTTP", "listen address", listener.Addr())
	r.server = http.Server{
		Handler:           handler,
		ReadHeaderTimeout: time.Second * 2,
	}
	if r.config.TLSCertFile != "" && r.config.TLSKeyFile != "" {
		return r.server.ServeTLS(listener, r.config.CertFile(), r.config.KeyFile())
	}
	return r.server.Serve(listener)
}

var (
	_ rpcclient.Client = &RPCServer{}
	_ client.CometRPC  = &RPCServer{}
)

func NewRPCServer(adapter *adapter.Adapter, cfg *cmtcfg.RPCConfig, txIndexer txindex.TxIndexer, blockIndexer indexer.BlockIndexer, logger log.Logger) *RPCServer {
	return &RPCServer{adapter: adapter, txIndexer: txIndexer, blockIndexer: blockIndexer, config: cfg, logger: servercmtlog.CometLoggerWrapper{Logger: logger}}
}

// ABCIInfo implements client.CometRPC.
func (r *RPCServer) ABCIInfo(context.Context) (*coretypes.ResultABCIInfo, error) {
	info, err := r.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIInfo{
		Response: *info,
	}, nil
}

// ABCIQuery implements client.CometRPC.
func (r *RPCServer) ABCIQuery(ctx context.Context, path string, data cmtbytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	resp, err := r.adapter.App.Query(ctx, &abci.RequestQuery{
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
func (r *RPCServer) ABCIQueryWithOptions(ctx context.Context, path string, data cmtbytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	resp, err := r.adapter.App.Query(ctx, &abci.RequestQuery{
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

// Block implements client.CometRPC.
func (r *RPCServer) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	var heightValue uint64

	switch {
	// block tag = included
	case height != nil && *height == -1:
		// heightValue = r.adapter.store.GetDAIncludedHeight()
		// TODO: implement
		return nil, errors.New("DA included height not implemented")
	default:
		heightValue = r.normalizeHeight(height)
	}
	header, data, err := r.adapter.Store.GetBlockData(ctx, heightValue)
	if err != nil {
		return nil, err
	}

	hash := header.Hash()
	abciBlock, err := ToABCIBlock(header, data)
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
func (r *RPCServer) BlockByHash(ctx context.Context, hash []byte) (*coretypes.ResultBlock, error) {
	header, data, err := r.adapter.Store.GetBlockByHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	abciBlock, err := ToABCIBlock(header, data)
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
func (r *RPCServer) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	// var h uint64
	// if height == nil {
	// 	h = r.adapter.Store.Height(ctx)
	// } else {
	// 	h = uint64(*height)
	// }
	// header, _, err := r.adapter.Store.GetBlockData(ctx, h)
	// if err != nil {
	// 	return nil, err
	// }

	// TODO(facu): implement
	// resp, err := r.adapter.Store.GetBlockResponses(ctx, h)
	// if err != nil {
	// 	return nil, err
	// }

	return &coretypes.ResultBlockResults{}, nil
	// 	Height:                int64(h), //nolint:gosec
	// 	TxsResults:            resp.TxResults,
	// 	FinalizeBlockEvents:   resp.Events,
	// 	ValidatorUpdates:      resp.ValidatorUpdates,
	// 	ConsensusParamUpdates: resp.ConsensusParamUpdates,
	// 	AppHash:               header.Header.AppHash,
	// }, nil
}

// BlockSearch implements client.CometRPC.
func (r *RPCServer) BlockSearch(ctx context.Context, query string, pagePtr *int, perPagePtr *int, orderBy string) (*coretypes.ResultBlockSearch, error) {
	// skip if block indexing is disabled
	if _, ok := r.blockIndexer.(*blockidxnull.BlockerIndexer); ok {
		return nil, errors.New("block indexing is disabled")
	}

	q, err := cmtquery.New(query)
	if err != nil {
		return nil, err
	}

	results, err := r.blockIndexer.Search(ctx, q)
	if err != nil {
		return nil, err
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
	perPage := validatePerPage(perPagePtr)

	page, err := validatePage(pagePtr, perPage, totalCount)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(page, perPage)
	pageSize := cmtmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*coretypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		header, data, err := r.adapter.Store.GetBlockData(ctx, uint64(results[i]))
		if err != nil {
			return nil, err
		}
		block, err := ToABCIBlock(header, data)
		if err != nil {
			return nil, err
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

// BlockchainInfo implements client.CometRPC.
func (r *RPCServer) BlockchainInfo(ctx context.Context, minHeight int64, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	const limit int64 = 20

	height, err := r.adapter.Store.Height(ctx)
	if err != nil {
		return nil, err
	}

	// Currently blocks are not pruned and are synced linearly so the base height is 0
	minHeight, maxHeight, err = filterMinMax(
		0,
		int64(height),
		minHeight,
		maxHeight,
		limit)
	if err != nil {
		return nil, err
	}

	blocks := make([]*cmttypes.BlockMeta, 0, maxHeight-minHeight+1)
	for height := maxHeight; height >= minHeight; height-- {
		header, data, err := r.adapter.Store.GetBlockData(ctx, uint64(height))
		if err != nil {
			return nil, err
		}
		if header != nil && data != nil {
			cmblockmeta, err := ToABCIBlockMeta(header, data)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, cmblockmeta)
		}
	}

	height, err = r.adapter.Store.Height(ctx)
	if err != nil {
		return nil, err
	}

	return &coretypes.ResultBlockchainInfo{
		LastHeight: int64(height), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}

// BroadcastTxAsync implements client.CometRPC.
func (r *RPCServer) BroadcastTxAsync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := r.adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
		responseCh <- response
	}, mempool.TxInfo{})
	if err != nil {
		return nil, err
	}

	// Wait for the callback to be called
	res = <-responseCh

	return &coretypes.ResultBroadcastTx{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Codespace: res.Codespace,
		Hash:      tx.Hash(),
	}, nil
}

// BroadcastTxCommit implements client.CometRPC.
func (r *RPCServer) BroadcastTxCommit(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	// This implementation corresponds to Tendermints implementation from rpc/core/mempool.go.
	// ctx.RemoteAddr godoc: If neither HTTPReq nor WSConn is set, an empty string is returned.
	// This code is a local client, so we can assume that subscriber is ""
	subscriber := "" // ctx.RemoteAddr()

	if r.adapter.EventBus.NumClients() >= r.adapter.CometCfg.RPC.MaxSubscriptionClients {
		return nil, fmt.Errorf("max_subscription_clients %d reached", r.adapter.CometCfg.RPC.MaxSubscriptionClients)
	} else if r.adapter.EventBus.NumClientSubscriptions(subscriber) >= r.adapter.CometCfg.RPC.MaxSubscriptionsPerClient {
		return nil, fmt.Errorf("max_subscriptions_per_client %d reached", r.adapter.CometCfg.RPC.MaxSubscriptionsPerClient)
	}

	// Subscribe to tx being committed in block.
	subCtx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	q := cmttypes.EventQueryTxFor(tx)
	deliverTxSub, err := r.adapter.EventBus.Subscribe(subCtx, subscriber, q)
	if err != nil {
		err = fmt.Errorf("failed to subscribe to tx: %w", err)
		r.adapter.Logger.Error("Error on broadcast_tx_commit", "err", err)
		return nil, err
	}
	defer func() {
		if err := r.adapter.EventBus.Unsubscribe(ctx, subscriber, q); err != nil {
			r.adapter.Logger.Error("Error unsubscribing from eventBus", "err", err)
		}
	}()

	// add to mempool and wait for CheckTx result
	checkTxResCh := make(chan *abci.ResponseCheckTx, 1)
	err = r.adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-ctx.Done():
			return
		case checkTxResCh <- res:
		}
	}, mempool.TxInfo{})
	if err != nil {
		r.adapter.Logger.Error("Error on broadcastTxCommit", "err", err)
		return nil, fmt.Errorf("error on broadcastTxCommit: %w", err)
	}
	checkTxRes := <-checkTxResCh
	if checkTxRes.Code != abci.CodeTypeOK {
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, nil
	}

	// broadcast tx
	err = r.adapter.TxGossiper.Publish(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("tx added to local mempool but failure to broadcast: %w", err)
	}

	// Wait for the tx to be included in a block or timeout.
	select {
	case msg := <-deliverTxSub.Out(): // The tx was included in a block.
		deliverTxRes := msg.Data().(cmttypes.EventDataTx)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: deliverTxRes.Result,
			Hash:     tx.Hash(),
			Height:   deliverTxRes.Height,
		}, nil
	case <-deliverTxSub.Canceled():
		var reason string
		if deliverTxSub.Err() == nil {
			reason = "Tendermint exited"
		} else {
			reason = deliverTxSub.Err().Error()
		}
		err = fmt.Errorf("deliverTxSub was cancelled (reason: %s)", reason)
		r.adapter.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-time.After(r.adapter.CometCfg.RPC.TimeoutBroadcastTxCommit):
		err = errors.New("timed out waiting for tx to be included in a block")
		r.adapter.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	}
}

// BroadcastTxSync implements client.CometRPC.
func (r *RPCServer) BroadcastTxSync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	resCh := make(chan *abci.ResponseCheckTx, 1)
	err := r.adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-ctx.Done():
			return
		case resCh <- res:
		}
	}, mempool.TxInfo{})
	if err != nil {
		return nil, err
	}
	res := <-resCh

	// gossip the transaction if it's in the mempool.
	// Note: we have to do this here because, unlike the tendermint mempool reactor, there
	// is no routine that gossips transactions after they enter the pool
	if res.Code == abci.CodeTypeOK {
		if r.adapter.TxGossiper == nil {
			return nil, execp2p.ErrNotReady
		}

		err = r.adapter.TxGossiper.Publish(ctx, tx)
		if err != nil {
			// the transaction must be removed from the mempool if it cannot be gossiped.
			// if this does not occur, then the user will not be able to try again using
			// this node, as the CheckTx call above will return an error indicating that
			// the tx is already in the mempool
			_ = r.adapter.Mempool.RemoveTxByKey(tx.Key())
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

// Commit implements client.CometRPC.
func (r *RPCServer) Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error) {
	heightValue := r.normalizeHeight(height)
	header, data, err := r.adapter.Store.GetBlockData(ctx, heightValue)
	if err != nil {
		return nil, err
	}

	// we should have a single validator
	if len(header.ProposerAddress) == 0 {
		return nil, errors.New("empty validator set found in block")
	}

	commit := getABCICommit(heightValue, header.Hash(), header.ProposerAddress, header.Time(), header.Signature)

	block, err := ToABCIBlock(header, data)
	if err != nil {
		return nil, err
	}

	return coretypes.NewResultCommit(&block.Header, commit, true), nil
}

// Status implements client.CometRPC.
func (r *RPCServer) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	info, err := r.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}

	s, err := r.adapter.LoadState(ctx)
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

// Tx implements client.CometRPC.
func (r *RPCServer) Tx(ctx context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error) {
	res, err := r.txIndexer.Get(hash)
	if err != nil {
		return nil, err
	}

	if res == nil {
		return nil, fmt.Errorf("tx (%X) not found", hash)
	}

	height := res.Height
	index := res.Index

	var proof cmttypes.TxProof
	if prove { // nolint:staticcheck // todo
		// TODO(facu): implement proofs?
		// _, data, _ := r.adapter.Store.GetBlockData(ctx, uint64(height))
		// blockProof := data.Txs.Proof(int(index)) // XXX: overflow on 32-bit machines
		// proof = cmttypes.TxProof{
		// 	RootHash: blockProof.RootHash,
		// 	Data:     cmttypes.Tx(blockProof.Data),
		// 	Proof:    blockProof.Proof,
		// }
	}

	return &coretypes.ResultTx{
		Hash:     hash,
		Height:   height,
		Index:    index,
		TxResult: res.Result,
		Tx:       res.Tx,
		Proof:    proof,
	}, nil
}

// TxSearch implements client.CometRPC.
func (r *RPCServer) TxSearch(ctx context.Context, query string, prove bool, pagePtr *int, perPagePtr *int, orderBy string) (*coretypes.ResultTxSearch, error) {
	// if index is disabled, return error
	if _, ok := r.txIndexer.(*null.TxIndex); ok {
		return nil, errors.New("transaction indexing is disabled")
	} else if len(query) > maxQueryLength {
		return nil, errors.New("maximum query length exceeded")
	}

	q, err := cmtquery.New(query)
	if err != nil {
		return nil, err
	}

	results, err := r.txIndexer.Search(ctx, q)
	if err != nil {
		return nil, err
	}

	// sort results (must be done before pagination)
	switch orderBy {
	case "desc":
		sort.Slice(results, func(i, j int) bool {
			if results[i].Height == results[j].Height {
				return results[i].Index > results[j].Index
			}
			return results[i].Height > results[j].Height
		})
	case "asc", "":
		sort.Slice(results, func(i, j int) bool {
			if results[i].Height == results[j].Height {
				return results[i].Index < results[j].Index
			}
			return results[i].Height < results[j].Height
		})
	default:
		return nil, errors.New("expected order_by to be either `asc` or `desc` or empty")
	}

	// paginate results
	totalCount := len(results)
	perPage := validatePerPage(perPagePtr)

	page, err := validatePage(pagePtr, perPage, totalCount)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(page, perPage)
	pageSize := cmtmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*coretypes.ResultTx, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		r := results[i]

		var proof cmttypes.TxProof
		if prove { // nolint:staticcheck // todo
			// TODO: implement proofs?
			// block := env.BlockStore.LoadBlock(r.Height)
			// if block != nil {
			// 	proof = block.Data.Txs.Proof(int(r.Index))
			// }
		}

		apiResults = append(apiResults, &coretypes.ResultTx{
			Hash:     cmttypes.Tx(r.Tx).Hash(),
			Height:   r.Height,
			Index:    r.Index,
			TxResult: r.Result,
			Tx:       r.Tx,
			Proof:    proof,
		})
	}

	return &coretypes.ResultTxSearch{Txs: apiResults, TotalCount: totalCount}, nil
}

// Validators implements client.CometRPC.
func (r *RPCServer) Validators(ctx context.Context, height *int64, page *int, perPage *int) (*coretypes.ResultValidators, error) {
	s, err := r.adapter.LoadState(ctx)
	if err != nil {
		return nil, err
	}

	validators := s.Validators.Validators
	totalCount := len(validators)

	currentHeight, err := r.adapter.Store.Height(ctx)
	if err != nil {
		return nil, err
	}

	// Handle pagination
	start := 0
	if page != nil && perPage != nil {
		start = (*page - 1) * *perPage
		end := cmtmath.MinInt(start+*perPage, totalCount)

		if start >= totalCount {
			return &coretypes.ResultValidators{
				BlockHeight: int64(currentHeight),
				Validators:  []*cmttypes.Validator{},
				Total:       totalCount,
			}, nil
		}
		validators = validators[start:end]
	}

	return &coretypes.ResultValidators{
		BlockHeight: int64(currentHeight),
		Validators:  validators,
		Total:       totalCount,
	}, nil
}

// BroadcastEvidence is not implemented
func (r *RPCServer) BroadcastEvidence(_ context.Context, evidence cmttypes.Evidence) (*coretypes.ResultBroadcastEvidence, error) {
	return &coretypes.ResultBroadcastEvidence{
		Hash: evidence.Hash(),
	}, nil
}

// CheckTx implements client.Client.
func (r *RPCServer) CheckTx(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultCheckTx, error) {
	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := r.adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
		responseCh <- response
	}, mempool.TxInfo{})
	if err != nil {
		return nil, err
	}

	// Wait for the callback to be called
	res = <-responseCh

	return &coretypes.ResultCheckTx{ResponseCheckTx: *res}, nil
}

// ConsensusParams implements client.Client.
func (r *RPCServer) ConsensusParams(ctx context.Context, height *int64) (*coretypes.ResultConsensusParams, error) {
	state, err := r.adapter.LoadState(ctx)
	if err != nil {
		return nil, err
	}
	params := state.ConsensusParams
	return &coretypes.ResultConsensusParams{
		BlockHeight: int64(r.normalizeHeight(height)), //nolint:gosec
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
func (r *RPCServer) ConsensusState(context.Context) (*coretypes.ResultConsensusState, error) {
	return nil, ErrConsensusStateNotAvailable
}

// DumpConsensusState implements client.Client.
func (r *RPCServer) DumpConsensusState(context.Context) (*coretypes.ResultDumpConsensusState, error) {
	return nil, ErrConsensusStateNotAvailable
}

// Genesis implements client.Client.
func (r *RPCServer) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	panic("unimplemented")
}

// GenesisChunked implements client.Client.
func (r *RPCServer) GenesisChunked(context.Context, uint) (*coretypes.ResultGenesisChunk, error) {
	return nil, errors.New("GenesisChunked RPC method is not yet implemented")
}

// Header implements client.Client.
func (r *RPCServer) Header(ctx context.Context, heightPtr *int64) (*coretypes.ResultHeader, error) {
	height := r.normalizeHeight(heightPtr)
	blockMeta := r.getBlockMeta(ctx, height)
	if blockMeta == nil {
		return nil, fmt.Errorf("block at height %d not found", height)
	}
	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// HeaderByHash implements client.Client.
func (r *RPCServer) HeaderByHash(ctx context.Context, hash cmtbytes.HexBytes) (*coretypes.ResultHeader, error) {
	// N.B. The hash parameter is HexBytes so that the reflective parameter
	// decoding logic in the HTTP service will correctly translate from JSON.
	// See https://github.com/cometbft/cometbft/issues/6802 for context.

	header, data, err := r.adapter.Store.GetBlockByHash(ctx, types.Hash(hash))
	if err != nil {
		return nil, err
	}

	blockMeta, err := ToABCIBlockMeta(header, data)
	if err != nil {
		return nil, err
	}

	if blockMeta == nil {
		return &coretypes.ResultHeader{}, nil
	}

	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// Health implements client.Client.
func (r *RPCServer) Health(context.Context) (*coretypes.ResultHealth, error) {
	return &coretypes.ResultHealth{}, nil
}

// IsRunning implements client.Client.
func (r *RPCServer) IsRunning() bool {
	panic("unimplemented")
}

// NetInfo implements client.Client.
func (r *RPCServer) NetInfo(context.Context) (*coretypes.ResultNetInfo, error) {
	res := coretypes.ResultNetInfo{
		Listening: true,
	}
	for _, ma := range r.adapter.P2PClient.Addrs() {
		res.Listeners = append(res.Listeners, ma.String())
	}
	peers := r.adapter.P2PClient.Peers()
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

// NumUnconfirmedTxs implements client.Client.
func (r *RPCServer) NumUnconfirmedTxs(context.Context) (*coretypes.ResultUnconfirmedTxs, error) {
	return &coretypes.ResultUnconfirmedTxs{
		Count:      r.adapter.Mempool.Size(),
		Total:      r.adapter.Mempool.Size(),
		TotalBytes: r.adapter.Mempool.SizeBytes(),
	}, nil
}

// OnReset implements client.Client.
func (r *RPCServer) OnReset() error {
	panic("unimplemented")
}

// OnStart implements client.Client.
func (r *RPCServer) OnStart() error {
	panic("unimplemented")
}

// OnStop implements client.Client.
func (r *RPCServer) OnStop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.server.Shutdown(ctx); err != nil {
		r.logger.Error("error while shutting down RPC server", "error", err)
	}
}

// Quit implements client.Client.
func (r *RPCServer) Quit() <-chan struct{} {
	panic("unimplemented")
}

// Reset implements client.Client.
func (r *RPCServer) Reset() error {
	panic("unimplemented")
}

// SetLogger implements client.Client.
func (r *RPCServer) SetLogger(logger cmtlog.Logger) {
	r.logger = logger
}

// Stop implements client.Client.
func (r *RPCServer) Stop() error {
	panic("unimplemented")
}

// String implements client.Client.
func (r *RPCServer) String() string {
	panic("unimplemented")
}

// Subscribe implements client.Client.
func (r *RPCServer) Subscribe(ctx context.Context, subscriber string, query string, outCapacity ...int) (out <-chan coretypes.ResultEvent, err error) {
	return nil, errors.New("event subscription functionality is not yet implemented")
}

// UnconfirmedTxs implements client.Client.
func (r *RPCServer) UnconfirmedTxs(ctx context.Context, limitPtr *int) (*coretypes.ResultUnconfirmedTxs, error) {
	limit := validatePerPage(limitPtr)
	txs := r.adapter.Mempool.ReapMaxTxs(limit)
	return &coretypes.ResultUnconfirmedTxs{
		Count:      len(txs),
		Total:      r.adapter.Mempool.Size(),
		TotalBytes: r.adapter.Mempool.SizeBytes(),
		Txs:        txs,
	}, nil
}

// Unsubscribe implements client.Client.
func (r *RPCServer) Unsubscribe(ctx context.Context, subscriber string, query string) error {
	// TODO: implement EventBus
	return errors.New("EventBus subscription functionality is not yet implemented")
}

// UnsubscribeAll implements client.Client.
func (r *RPCServer) UnsubscribeAll(ctx context.Context, subscriber string) error {
	// TODO: implement EventBus
	return errors.New("EventBus subscription functionality is not yet implemented")
}

//----------------------------------------------

func validateSkipCount(page, perPage int) int {
	skipCount := (page - 1) * perPage
	if skipCount < 0 {
		return 0
	}

	return skipCount
}

func validatePerPage(perPagePtr *int) int {
	if perPagePtr == nil { // no per_page parameter
		return defaultPerPage
	}

	perPage := *perPagePtr
	if perPage < 1 {
		return defaultPerPage
	} else if perPage > maxPerPage {
		return maxPerPage
	}
	return perPage
}

func validatePage(pagePtr *int, perPage, totalCount int) (int, error) {
	if perPage < 1 {
		return 0, fmt.Errorf("invalid perPage parameter: %d (must be positive)", perPage)
	}

	if pagePtr == nil { // no page parameter
		return 1, nil
	}

	pages := ((totalCount - 1) / perPage) + 1
	if pages == 0 {
		pages = 1 // one page (even if it's empty)
	}
	page := *pagePtr
	if page <= 0 || page > pages {
		return 1, fmt.Errorf("page should be within [1, %d] range, given %d", pages, page)
	}

	return page, nil
}

func (r *RPCServer) normalizeHeight(height *int64) uint64 {
	var heightValue uint64
	if height == nil {
		var err error
		heightValue, err = r.adapter.Store.Height(context.Background())
		if err != nil {
			return 0
		}
	} else {
		heightValue = uint64(*height)
	}

	return heightValue
}

func (r *RPCServer) getBlockMeta(ctx context.Context, n uint64) *cmttypes.BlockMeta {
	header, data, err := r.adapter.Store.GetBlockData(ctx, n)
	if err != nil {
		return nil
	}
	bmeta, err := ToABCIBlockMeta(header, data)
	if err != nil {
		return nil
	}

	return bmeta
}
