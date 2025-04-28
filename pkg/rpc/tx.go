package rpc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/mempool"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/state/txindex/null"
	cmttypes "github.com/cometbft/cometbft/types"

	execp2p "github.com/rollkit/go-execution-abci/pkg/p2p"
)

// BroadcastTxAsync implements client.CometRPC.
func (r *RPCServer) BroadcastTxAsync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := r.adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
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
func (r *RPCServer) BroadcastTxCommit(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	// This implementation corresponds to Tendermint's implementation from rpc/core/mempool.go.
	// ctx.RemoteAddr godoc: If neither HTTPReq nor WSConn is set, an empty string is returned.
	// This code is a local client, so we can assume that subscriber is ""
	// TODO: Check if assuming empty subscriber is always correct for this context.
	subscriber := "" // ctx.RemoteAddr()

	if r.adapter.EventBus == nil {
		return nil, errors.New("event bus is not configured, cannot subscribe to events")
	}

	// Use CometBFT config values directly if available
	maxSubs := r.adapter.CometCfg.RPC.MaxSubscriptionsPerClient
	maxClients := r.adapter.CometCfg.RPC.MaxSubscriptionClients
	commitTimeout := r.adapter.CometCfg.RPC.TimeoutBroadcastTxCommit

	if r.adapter.EventBus.NumClients() >= maxClients {
		return nil, fmt.Errorf("max_subscription_clients %d reached", maxClients)
	} else if r.adapter.EventBus.NumClientSubscriptions(subscriber) >= maxSubs {
		return nil, fmt.Errorf("max_subscriptions_per_client %d reached", maxSubs)
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

	// Add to mempool and wait for CheckTx result
	checkTxResCh := make(chan *abci.ResponseCheckTx, 1)
	err = r.adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-ctx.Done():
			// Context cancelled before CheckTx completed
			r.adapter.Logger.Error("Context cancelled during CheckTx in BroadcastTxCommit")
			return
		case checkTxResCh <- res:
		}
	}, mempool.TxInfo{})
	if err != nil {
		r.adapter.Logger.Error("Error on CheckTx in BroadcastTxCommit", "err", err)
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
	if r.adapter.TxGossiper == nil {
		return nil, execp2p.ErrNotReady // Cannot gossip if gossiper is nil
	}
	err = r.adapter.TxGossiper.Publish(ctx, tx)
	if err != nil {
		// Note: If gossiping fails, the tx is still in the local mempool and subscribed.
		// Tendermint's original behaviour might differ here. Consider if tx should be removed from mempool.
		r.adapter.Logger.Error("tx added to local mempool but failed to broadcast", "err", err)
		return nil, fmt.Errorf("tx added to local mempool but failure to broadcast: %w", err)
	}

	// Wait for the tx to be included in a block or timeout.
	select {
	case msg, ok := <-deliverTxSub.Out():
		if !ok {
			// Channel closed, possibly due to unsubscribe or EventBus shutdown
			err = fmt.Errorf("subscription channel closed unexpectedly: %w", deliverTxSub.Err())
			r.adapter.Logger.Error("Error on broadcastTxCommit", "err", err)
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
			r.adapter.Logger.Error("Error on broadcastTxCommit", "err", err)
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
		r.adapter.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-time.After(commitTimeout):
		err = errors.New("timed out waiting for tx to be included in a block")
		r.adapter.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &coretypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-ctx.Done():
		// Parent context cancelled
		err = fmt.Errorf("context cancelled while waiting for tx commit event: %w", ctx.Err())
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
		if r.adapter.TxGossiper == nil {
			return nil, execp2p.ErrNotReady // Cannot gossip if gossiper is not ready
		}

		err = r.adapter.TxGossiper.Publish(ctx, tx)
		if err != nil {
			// If gossiping fails, remove the tx from the mempool to allow resubmission.
			// This matches the behaviour described in the original comments.
			rmErr := r.adapter.Mempool.RemoveTxByKey(tx.Key())
			if rmErr != nil {
				// Log if removal also failed, but return the gossip error primarily.
				r.adapter.Logger.Error("Failed to remove tx from mempool after gossip failure", "tx_key", tx.Key(), "removal_error", rmErr)
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
func (r *RPCServer) CheckTx(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultCheckTx, error) {
	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := r.adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
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
func (r *RPCServer) Tx(ctx context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error) {
	if r.txIndexer == nil {
		return nil, errors.New("tx indexing is disabled")
	}
	res, err := r.txIndexer.Get(hash)
	if err != nil {
		// Error could be due to db connection issues, etc.
		return nil, fmt.Errorf("error retrieving tx %X from indexer: %w", hash, err)
	}

	if res == nil {
		// Tx not found is a specific case, return error consistent with Tendermint
		return nil, fmt.Errorf("tx (%X) not found", hash)
	}

	height := res.Height
	index := res.Index

	var proof cmttypes.TxProof
	if prove {
		// Proof generation is currently not supported as per original comments.
		// If implemented, the logic would go here.
		// Example placeholder:
		// block, err := r.adapter.Store.GetBlock(ctx, uint64(height)) // Assuming a GetBlock method
		// if err == nil && block != nil {
		// 	proof = block.Data.Txs.Proof(int(index)) // Need to adapt to actual block structure
		// }
		return nil, errors.New("transaction proof generation is not supported") // Return error if prove=true is requested but not supported
	}

	return &coretypes.ResultTx{
		Hash:     hash,
		Height:   height,
		Index:    index,
		TxResult: res.Result,
		Tx:       res.Tx,
		Proof:    proof, // Will be empty if prove is false or unsupported
	}, nil
}

// TxSearch implements client.CometRPC.
func (r *RPCServer) TxSearch(ctx context.Context, query string, prove bool, pagePtr *int, perPagePtr *int, orderBy string) (*coretypes.ResultTxSearch, error) {
	// if index is disabled, return error
	if _, ok := r.txIndexer.(*null.TxIndex); ok {
		return nil, errors.New("transaction indexing is disabled")
	}
	if r.txIndexer == nil { // Belt-and-suspenders check
		return nil, errors.New("transaction indexing is not available")
	}
	if len(query) > maxQueryLength {
		return nil, errors.New("maximum query length exceeded")
	}

	q, err := cmtquery.New(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	results, err := r.txIndexer.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("tx search failed: %w", err)
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
		if prove {
			// As per Tx method, proofs are not currently supported.
			return nil, errors.New("transaction proof generation is not supported")
		}

		apiResults = append(apiResults, &coretypes.ResultTx{
			Hash:     cmttypes.Tx(r.Tx).Hash(), // Calculate hash from Tx bytes
			Height:   r.Height,
			Index:    r.Index,
			TxResult: r.Result,
			Tx:       r.Tx,
			Proof:    proof, // Will be empty
		})
	}

	return &coretypes.ResultTxSearch{Txs: apiResults, TotalCount: totalCount}, nil
}

// NumUnconfirmedTxs implements client.Client.
func (r *RPCServer) NumUnconfirmedTxs(context.Context) (*coretypes.ResultUnconfirmedTxs, error) {
	return &coretypes.ResultUnconfirmedTxs{
		Count:      r.adapter.Mempool.Size(),
		Total:      r.adapter.Mempool.Size(), // Total and Count seem redundant here, following original.
		TotalBytes: r.adapter.Mempool.SizeBytes(),
	}, nil
}

// UnconfirmedTxs implements client.Client.
func (r *RPCServer) UnconfirmedTxs(ctx context.Context, limitPtr *int) (*coretypes.ResultUnconfirmedTxs, error) {
	limit := validatePerPage(limitPtr) // Use validated per_page logic for limit
	txs := r.adapter.Mempool.ReapMaxTxs(limit)
	return &coretypes.ResultUnconfirmedTxs{
		Count:      len(txs),
		Total:      r.adapter.Mempool.Size(),
		TotalBytes: r.adapter.Mempool.SizeBytes(),
		Txs:        txs,
	}, nil
}
