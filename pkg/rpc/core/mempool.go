package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	"github.com/cometbft/cometbft/mempool"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"

	execp2p "github.com/rollkit/go-execution-abci/pkg/p2p"
)

// Define timeout for waiting for TX commit event
const subscribeTimeout = 5 * time.Second // TODO: Make configurable?

//-----------------------------------------------------------------------------
// NOTE: tx should be signed, but this is only checked at the app level (not by CometBFT!)

// BroadcastTxAsync returns right away, with no response. Does not wait for
// CheckTx nor DeliverTx results.
// More: https://docs.cometbft.com/v0.37/rpc/#/Tx/broadcast_tx_async
func BroadcastTxAsync(ctx *rpctypes.Context, tx cmttypes.Tx) (*ctypes.ResultBroadcastTx, error) {
	unwrappedCtx := ctx.Context()

	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := env.Adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
		responseCh <- response
	}, mempool.TxInfo{})
	if err != nil {
		return nil, fmt.Errorf("error during CheckTx: %w", err)
	}

	// Wait for the callback to be called
	select {
	case res = <-responseCh:
		// Successfully received CheckTx response
	case <-unwrappedCtx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for CheckTx response: %w", unwrappedCtx.Err())
	}

	// Note: Original code didn't gossip on async. If gossiping is desired here,
	// it should be added similarly to BroadcastTxSync, potentially after checking res.Code.

	return &ctypes.ResultBroadcastTx{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Codespace: res.Codespace,
		Hash:      tx.Hash(),
	}, nil
}

// BroadcastTxSync returns with the response from CheckTx. Does not wait for
// DeliverTx result.
// More: https://docs.cometbft.com/v0.37/rpc/#/Tx/broadcast_tx_sync
func BroadcastTxSync(ctx *rpctypes.Context, tx cmttypes.Tx) (*ctypes.ResultBroadcastTx, error) {
	unwrappedCtx := ctx.Context()

	resCh := make(chan *abci.ResponseCheckTx, 1)
	err := env.Adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-unwrappedCtx.Done():
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
	case <-unwrappedCtx.Done():
		return nil, fmt.Errorf("context cancelled waiting for CheckTx: %w", unwrappedCtx.Err())
	}

	// Gossip the transaction if it passed CheckTx.
	if res.Code == abci.CodeTypeOK {
		if env.Adapter.TxGossiper == nil {
			return nil, execp2p.ErrNotReady // Cannot gossip if gossiper is not ready
		}

		err = env.Adapter.TxGossiper.Publish(unwrappedCtx, tx)
		if err != nil {
			// If gossiping fails, remove the tx from the mempool to allow resubmission.
			// This matches the behaviour described in the original comments.
			rmErr := env.Adapter.Mempool.RemoveTxByKey(tx.Key())
			if rmErr != nil {
				// Log if removal also failed, but return the gossip error primarily.
				env.Logger.Error("Failed to remove tx from mempool after gossip failure", "tx_key", tx.Key(), "removal_error", rmErr)
			}
			return nil, fmt.Errorf("failed to gossip tx: %w", err)
		}
	}

	return &ctypes.ResultBroadcastTx{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Codespace: res.Codespace,
		Hash:      tx.Hash(),
	}, nil
}

// BroadcastTxCommit returns with the responses from CheckTx and DeliverTx.
// More: https://docs.cometbft.com/v0.37/rpc/#/Tx/broadcast_tx_commit
func BroadcastTxCommit(ctx *rpctypes.Context, tx cmttypes.Tx) (*ctypes.ResultBroadcastTxCommit, error) {
	unwrappedCtx := ctx.Context()

	// This implementation corresponds to Tendermint's implementation from rpc/core/mempool.go.
	// ctx.RemoteAddr godoc: If neither HTTPReq nor WSConn is set, an empty string is returned.
	// This code is a local client, so we can assume that subscriber is ""
	// TODO: Check if assuming empty subscriber is always correct for this context.
	subscriber := "" // ctx.RemoteAddr()

	if env.Adapter.EventBus == nil {
		return nil, errors.New("event bus is not configured, cannot subscribe to events")
	}

	// Use CometBFT config values directly if available
	// TODO: Access these config values properly, perhaps via RpcProvider struct if needed
	maxSubs := 100                                       // Placeholder
	maxClients := 100                                    // Placeholder
	commitTimeout := env.Config.TimeoutBroadcastTxCommit // Assuming CometCfg is accessible

	if env.Adapter.EventBus.NumClients() >= maxClients {
		return nil, fmt.Errorf("max_subscription_clients %d reached", maxClients)
	} else if env.Adapter.EventBus.NumClientSubscriptions(subscriber) >= maxSubs {
		return nil, fmt.Errorf("max_subscriptions_per_client %d reached", maxSubs)
	}

	// Subscribe to tx being committed in block.
	subCtx, cancel := context.WithTimeout(unwrappedCtx, subscribeTimeout)
	defer cancel()
	q := cmttypes.EventQueryTxFor(tx)
	deliverTxSub, err := env.Adapter.EventBus.Subscribe(subCtx, subscriber, q)
	if err != nil {
		err = fmt.Errorf("failed to subscribe to tx: %w", err)
		env.Logger.Error("Error on broadcast_tx_commit", "err", err)
		return nil, err
	}
	defer func() {
		if err := env.Adapter.EventBus.Unsubscribe(ctx.Context(), subscriber, q); err != nil {
			env.Logger.Error("Error unsubscribing from eventBus", "err", err)
		}
	}()

	// Add to mempool and wait for CheckTx result
	checkTxResCh := make(chan *abci.ResponseCheckTx, 1)
	err = env.Adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-unwrappedCtx.Done():
			// Context cancelled before CheckTx completed
			env.Logger.Error("Context cancelled during CheckTx in BroadcastTxCommit")
			return
		case checkTxResCh <- res:
		}
	}, mempool.TxInfo{})
	if err != nil {
		env.Logger.Error("Error on CheckTx in BroadcastTxCommit", "err", err)
		return nil, fmt.Errorf("error on CheckTx in broadcastTxCommit: %w", err)
	}

	var checkTxRes *abci.ResponseCheckTx
	select {
	case checkTxRes = <-checkTxResCh:
		// Got CheckTx response
	case <-unwrappedCtx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for CheckTx response: %w", unwrappedCtx.Err())
	}

	if checkTxRes.Code != abci.CodeTypeOK {
		return &ctypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{}, // Included for consistency, though tx didn't make it to a block
			Hash:     tx.Hash(),
		}, nil // Return nil error as CheckTx failed, this is the expected result structure
	}

	// Broadcast tx gossip only if CheckTx passed
	if env.Adapter.TxGossiper == nil {
		return nil, execp2p.ErrNotReady // Cannot gossip if gossiper is nil
	}
	err = env.Adapter.TxGossiper.Publish(unwrappedCtx, tx)
	if err != nil {
		// Note: If gossiping fails, the tx is still in the local mempool and subscribed.
		// Tendermint's original behaviour might differ here. Consider if tx should be removed from mempool.
		env.Logger.Error("tx added to local mempool but failed to broadcast", "err", err)
		return nil, fmt.Errorf("tx added to local mempool but failure to broadcast: %w", err)
	}

	// Wait for the tx to be included in a block or timeout.
	select {
	case msg, ok := <-deliverTxSub.Out():
		if !ok {
			// Channel closed, possibly due to unsubscribe or EventBus shutdown
			err = fmt.Errorf("subscription channel closed unexpectedly: %w", deliverTxSub.Err())
			env.Logger.Error("Error on broadcastTxCommit", "err", err)
			// Return the CheckTx result as the tx wasn't confirmed in a block
			return &ctypes.ResultBroadcastTxCommit{
				CheckTx:  *checkTxRes,
				TxResult: abci.ExecTxResult{},
				Hash:     tx.Hash(),
			}, err
		}
		deliverTxRes, ok := msg.Data().(cmttypes.EventDataTx)
		if !ok {
			err = fmt.Errorf("unexpected event data type: got %T, expected %T", msg.Data(), cmttypes.EventDataTx{})
			env.Logger.Error("Error on broadcastTxCommit", "err", err)
			return &ctypes.ResultBroadcastTxCommit{
				CheckTx:  *checkTxRes,
				TxResult: abci.ExecTxResult{},
				Hash:     tx.Hash(),
			}, err
		}
		return &ctypes.ResultBroadcastTxCommit{
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
		env.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &ctypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-time.After(commitTimeout):
		err = errors.New("timed out waiting for tx to be included in a block")
		env.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &ctypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-unwrappedCtx.Done():
		// Parent context cancelled
		err = fmt.Errorf("context cancelled while waiting for tx commit event: %w", unwrappedCtx.Err())
		env.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &ctypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	}
}

// UnconfirmedTxs gets unconfirmed transactions (maximum ?limit entries)
// including their number.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/unconfirmed_txs
func UnconfirmedTxs(ctx *rpctypes.Context, limitPtr *int) (*ctypes.ResultUnconfirmedTxs, error) {
	txs := env.Adapter.Mempool.ReapMaxTxs(-1) // Reap all transactions

	limit := len(txs)
	if limitPtr != nil {
		limit = cmtmath.MinInt(*limitPtr, limit)
		if limit < 0 {
			limit = 0
		}
	}
	paginatedTxs := txs[:limit]

	return &ctypes.ResultUnconfirmedTxs{
		Count:      len(paginatedTxs),
		Total:      env.Adapter.Mempool.Size(),
		TotalBytes: env.Adapter.Mempool.SizeBytes(),
		Txs:        paginatedTxs,
	}, nil
}

// NumUnconfirmedTxs gets number of unconfirmed transactions.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/num_unconfirmed_txs
func NumUnconfirmedTxs(ctx *rpctypes.Context) (*ctypes.ResultUnconfirmedTxs, error) {
	return &ctypes.ResultUnconfirmedTxs{
		Count:      env.Adapter.Mempool.Size(),
		Total:      env.Adapter.Mempool.Size(), // Assuming Total means current size
		TotalBytes: env.Adapter.Mempool.SizeBytes(),
	}, nil
}

// CheckTx checks the transaction without executing it. The transaction won't
// be added to the mempool either.
// More: https://docs.cometbft.com/v0.37/rpc/#/Tx/check_tx
func CheckTx(ctx *rpctypes.Context, tx cmttypes.Tx) (*ctypes.ResultCheckTx, error) {
	unwrappedCtx := ctx.Context()

	var res *abci.ResponseCheckTx
	responseCh := make(chan *abci.ResponseCheckTx, 1)

	err := env.Adapter.Mempool.CheckTx(tx, func(response *abci.ResponseCheckTx) {
		select {
		case <-unwrappedCtx.Done():
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
		return &ctypes.ResultCheckTx{ResponseCheckTx: *res}, nil
	case <-unwrappedCtx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for CheckTx response: %w", unwrappedCtx.Err())
	}
}
