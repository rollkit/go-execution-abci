package main

import (
	"context"
	"errors"
	"fmt"
	"sort"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/p2p"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/state/txindex"
	"github.com/cometbft/cometbft/state/txindex/null"
	"github.com/cometbft/cometbft/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/rollkit/go-execution-abci/mempool"

	"github.com/cosmos/cosmos-sdk/client"
)

const (
	// maxQueryLength is the maximum length of a query string that will be
	// accepted. This is just a safety check to avoid outlandish queries.
	maxQueryLength = 512

	defaultPerPage = 30
	maxPerPage     = 100
)

type RPCServer struct {
	adapter   *Adapter
	txIndexer txindex.TxIndexer
}

var _ client.CometRPC = &RPCServer{}

// ABCIInfo implements client.CometRPC.
func (r *RPCServer) ABCIInfo(context.Context) (*coretypes.ResultABCIInfo, error) {
	info, err := r.adapter.app.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIInfo{
		Response: *info,
	}, nil
}

// ABCIQuery implements client.CometRPC.
func (r *RPCServer) ABCIQuery(ctx context.Context, path string, data cmtbytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	resp, err := r.adapter.app.Query(ctx, &abci.RequestQuery{
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
	resp, err := r.adapter.app.Query(ctx, &abci.RequestQuery{
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
	panic("unimplemented")
}

// BlockByHash implements client.CometRPC.
func (r *RPCServer) BlockByHash(ctx context.Context, hash []byte) (*coretypes.ResultBlock, error) {
	panic("unimplemented")
}

// BlockResults implements client.CometRPC.
func (r *RPCServer) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	panic("unimplemented")
}

// BlockSearch implements client.CometRPC.
func (r *RPCServer) BlockSearch(ctx context.Context, query string, page *int, perPage *int, orderBy string) (*coretypes.ResultBlockSearch, error) {
	panic("unimplemented")
}

// BlockchainInfo implements client.CometRPC.
func (r *RPCServer) BlockchainInfo(ctx context.Context, minHeight int64, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	panic("unimplemented")
}

// BroadcastTxAsync implements client.CometRPC.
func (r *RPCServer) BroadcastTxAsync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	err := r.adapter.mempool.CheckTx(tx, nil, mempool.TxInfo{})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultBroadcastTx{
		Code: abci.CodeTypeOK,
		Hash: tx.Hash(),
	}, nil
}

// BroadcastTxCommit implements client.CometRPC.
func (r *RPCServer) BroadcastTxCommit(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	panic("unimplemented")
}

// BroadcastTxSync implements client.CometRPC.
func (r *RPCServer) BroadcastTxSync(ctx context.Context, tx cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	err := r.adapter.mempool.CheckTx(tx, nil, mempool.TxInfo{})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultBroadcastTx{
		Code: abci.CodeTypeOK,
		Hash: tx.Hash(),
	}, nil
}

// Commit implements client.CometRPC.
func (r *RPCServer) Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error) {
	panic("unimplemented")
}

// Status implements client.CometRPC.
func (r *RPCServer) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	info, err := r.adapter.app.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}

	s := r.adapter.state.Load()

	return &coretypes.ResultStatus{
		NodeInfo: p2p.DefaultNodeInfo{}, // TODO: fill this in
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHash:   cmtbytes.HexBytes(info.LastBlockAppHash),
			LatestBlockHeight: info.LastBlockHeight,
			// LatestBlockTime:   s.LastBlockTime, // TODO: fill this in
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
	// TODO: implement once the indexer is implemented
	panic("unimplemented")
	// // Query the app for the transaction
	// res, err := r.adapter.app.Query(&abci.RequestQuery{
	// 	Path: "/tx",
	// 	Data: hash,
	// })
	// if err != nil {
	// 	return nil, err
	// }

	// if res.Code != abci.CodeTypeOK {
	// 	return nil, fmt.Errorf("failed to get tx: %s", res.Log)
	// }

	// // Return empty result if no tx found
	// if len(res.Value) == 0 {
	// 	return nil, fmt.Errorf("tx not found")
	// }

	// return &coretypes.ResultTx{
	// 	Hash:     hash,
	// 	Height:   res.Height,
	// 	Response: *&abci.ExecTxResult{Data: res.Value},
	// }, nil
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

		var proof types.TxProof
		if prove {
			// TODO: implement proofs
			// block := env.BlockStore.LoadBlock(r.Height)
			// if block != nil {
			// 	proof = block.Data.Txs.Proof(int(r.Index))
			// }
		}

		apiResults = append(apiResults, &coretypes.ResultTx{
			Hash:     types.Tx(r.Tx).Hash(),
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
	s := r.adapter.state.Load()

	validators := s.Validators.Validators
	totalCount := len(validators)

	// Handle pagination
	start := 0
	end := totalCount
	if page != nil && perPage != nil {
		start = (*page - 1) * *perPage
		end = cmtmath.MinInt(start+*perPage, totalCount)

		if start >= totalCount {
			return &coretypes.ResultValidators{
				BlockHeight: int64(r.adapter.store.Height()),
				Validators:  []*types.Validator{},
				Total:       totalCount,
			}, nil
		}
		validators = validators[start:end]
	}

	return &coretypes.ResultValidators{
		BlockHeight: int64(r.adapter.store.Height()),
		Validators:  validators,
		Total:       totalCount,
	}, nil
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
		panic(fmt.Sprintf("zero or negative perPage: %d", perPage))
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
