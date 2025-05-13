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
	err := env.Adapter.Mempool.CheckTx(tx, nil, mempool.TxInfo{})
	if err != nil {
		return nil, fmt.Errorf("error during CheckTx: %w", err)
	}
	// gossipTx optimistically
	err = env.Adapter.TxGossiper.Publish(unwrappedCtx, tx)
	if err != nil {
		err2 := env.Adapter.Mempool.RemoveTxByKey(tx.Key())
		if err2 != nil {
			env.Logger.Error("Error removing tx from mempool", "err", err2)
		}
		return nil, fmt.Errorf("tx added to local mempool but failed to gossip: %w", err)
	}
	return &ctypes.ResultBroadcastTx{Hash: tx.Hash()}, nil
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
		return nil, err
	}
	select {
	case res := <-resCh:
		// gossip the transaction if it's in the mempool.
		// Note: we have to do this here because, unlike the cometbft mempool reactor, there
		// is no routine that gossips transactions after they enter the pool
		if res.Code == abci.CodeTypeOK {
			err = env.Adapter.TxGossiper.Publish(unwrappedCtx, tx)
			if err != nil {
				// the transaction must be removed from the mempool if it cannot be gossiped.
				// if this does not occur, then the user will not be able to try again using
				// this node, as the CheckTx call above will return an error indicating that
				// the tx is already in the mempool
				err2 := env.Adapter.Mempool.RemoveTxByKey(tx.Key())
				if err2 != nil {
					env.Logger.Error("Error removing tx from mempool", "err", err2)
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
	case <-unwrappedCtx.Done():
		// If the context is done, return an error.
		return nil, fmt.Errorf("context finished while waiting for CheckTx result: %w", unwrappedCtx.Err())
	}
}

// BroadcastTxCommit returns with the responses from CheckTx and DeliverTx.
// More: https://docs.cometbft.com/v0.37/rpc/#/Tx/broadcast_tx_commit
func BroadcastTxCommit(ctx *rpctypes.Context, tx cmttypes.Tx) (*ctypes.ResultBroadcastTxCommit, error) {
	unwrappedCtx := ctx.Context()

	// This implementation corresponds to CometBFT implementation from rpc/core/mempool.go.
	// ctx.RemoteAddr godoc: If neither HTTPReq nor WSConn is set, an empty string is returned.
	// This code is a local client, so we can assume that subscriber is ""
	subscriber := "" //ctx.RemoteAddr()

	// Use CometBFT config values directly if available
	// TODO: Access these config values properly, perhaps via RpcProvider struct if needed
	maxSubs := 100    // Placeholder
	maxClients := 100 // Placeholder
	// commitTimeout := env.Config.TimeoutBroadcastTxCommit // Assuming CometCfg is accessible

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
		if err := env.Adapter.EventBus.Unsubscribe(unwrappedCtx, subscriber, q); err != nil {
			env.Logger.Error("Error unsubscribing from eventBus", "err", err)
		}
	}()

	// add to mempool and wait for CheckTx result
	checkTxResCh := make(chan *abci.ResponseCheckTx, 1)
	err = env.Adapter.Mempool.CheckTx(tx, func(res *abci.ResponseCheckTx) {
		select {
		case <-unwrappedCtx.Done():
			return
		case checkTxResCh <- res:
		}
	}, mempool.TxInfo{})
	if err != nil {
		env.Logger.Error("Error on broadcastTxCommit", "err", err)
		return nil, fmt.Errorf("error on broadcastTxCommit: %w", err)
	}
	var checkTxRes *abci.ResponseCheckTx
	select {
	case res := <-checkTxResCh:
		checkTxRes = res
	case <-unwrappedCtx.Done():
		env.Logger.Error("Context finished while waiting for CheckTx result in BroadcastTxCommit", "err", unwrappedCtx.Err())
		return nil, fmt.Errorf("context finished while waiting for CheckTx result: %w", unwrappedCtx.Err())
	}

	if checkTxRes.Code != abci.CodeTypeOK {
		return &ctypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, nil
	}

	// broadcast tx
	err = env.Adapter.TxGossiper.Publish(unwrappedCtx, tx)
	if err != nil {
		return nil, fmt.Errorf("tx added to local mempool but failure to broadcast: %w", err)
	}

	// Wait for the tx to be included in a block or timeout.
	select {
	case msg := <-deliverTxSub.Out(): // The tx was included in a block.
		deliverTxRes := msg.Data().(cmttypes.EventDataTx)
		return &ctypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: deliverTxRes.Result,
			Hash:     tx.Hash(),
			Height:   deliverTxRes.Height,
		}, nil
	case <-deliverTxSub.Canceled():
		var reason string
		if deliverTxSub.Err() == nil {
			reason = "BFT engine exited"
		} else {
			reason = deliverTxSub.Err().Error()
		}
		err = fmt.Errorf("deliverTxSub was cancelled (reason: %s)", reason)
		env.Logger.Error("Error on broadcastTxCommit", "err", err)
		return &ctypes.ResultBroadcastTxCommit{
			CheckTx:  *checkTxRes,
			TxResult: abci.ExecTxResult{},
			Hash:     tx.Hash(),
		}, err
	case <-time.After(subscribeTimeout):
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
