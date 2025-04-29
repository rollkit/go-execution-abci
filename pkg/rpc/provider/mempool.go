package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types" // Check if logger is used here
	cmtmath "github.com/cometbft/cometbft/libs/math"
	"github.com/cometbft/cometbft/mempool"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"

	execp2p "github.com/rollkit/go-execution-abci/pkg/p2p"
)

// Define timeout for waiting for TX commit event
const subscribeTimeout = 5 * time.Second // TODO: Make configurable?

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
	// TODO: Access these config values properly, perhaps via RpcProvider struct if needed
	maxSubs := 100                                                   // Placeholder
	maxClients := 100                                                // Placeholder
	commitTimeout := p.adapter.CometCfg.RPC.TimeoutBroadcastTxCommit // Assuming CometCfg is accessible

	if p.adapter.EventBus.NumClients() >= maxClients {
		return nil, fmt.Errorf("max_subscription_clients %d reached", maxClients)
	} else if p.adapter.EventBus.NumClientSubscriptions(subscriber) >= maxSubs {
		return nil, fmt.Errorf("max_subscriptions_per_client %d reached", maxSubs)
	}

	// Subscribe to tx being committed in block.
	subCtx, cancel := context.WithTimeout(ctx, subscribeTimeout)
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
