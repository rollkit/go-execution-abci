package core

import (
	"errors"
	"fmt"
	"sort"

	"github.com/cometbft/cometbft/crypto/merkle"
	"github.com/cometbft/cometbft/crypto/tmhash"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"

	rlktypes "github.com/rollkit/rollkit/types"
)

// Tx allows you to query the transaction results. `nil` could mean the
// transaction is in the mempool, invalidated, or was not sent in the first
// place.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/tx
func Tx(ctx *rpctypes.Context, hash []byte, prove bool) (*ctypes.ResultTx, error) {
	res, err := env.TxIndexer.Get(hash)
	if err != nil {
		return nil, err
	}

	if res == nil {
		return nil, fmt.Errorf("tx (%X) not found", hash)
	}

	height := res.Height
	index := res.Index

	var proof cmttypes.TxProof
	if prove {
		if proof, err = buildProof(ctx, height, index); err != nil {
			return nil, err
		}
	}
	return &ctypes.ResultTx{
		Hash:     hash,
		Height:   height,
		Index:    index,
		TxResult: res.Result,
		Tx:       res.Tx,
		Proof:    proof,
	}, nil
}

// TxSearch allows you to query for multiple transactions results. It returns a
// list of transactions (maximum ?per_page entries) and the total count.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/tx_search
func TxSearch(
	ctx *rpctypes.Context,
	query string,
	prove bool,
	pagePtr, perPagePtr *int,
	orderBy string,
) (*ctypes.ResultTxSearch, error) {
	unwrappedCtx := ctx.Context()
	q, err := cmtquery.New(query)
	if err != nil {
		return nil, err
	}

	results, err := env.TxIndexer.Search(unwrappedCtx, q)
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
	pageSize := min(perPage, totalCount-skipCount)

	apiResults := make([]*ctypes.ResultTx, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		r := results[i]

		var proof cmttypes.TxProof
		if prove {
			if proof, err = buildProof(ctx, r.Height, r.Index); err != nil {
				return nil, err
			}
		}
		apiResults = append(apiResults, &ctypes.ResultTx{
			Hash:     cmttypes.Tx(r.Tx).Hash(),
			Height:   r.Height,
			Index:    r.Index,
			TxResult: r.Result,
			Tx:       r.Tx,
			Proof:    proof,
		})
	}

	return &ctypes.ResultTxSearch{Txs: apiResults, TotalCount: totalCount}, nil
}

func buildProof(ctx *rpctypes.Context, blockHeight int64, txIndex uint32) (cmttypes.TxProof, error) {
	_, data, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), uint64(blockHeight))
	if err != nil {
		return cmttypes.TxProof{}, fmt.Errorf("failed to get block data: %w", err)
	}
	return proofTXExists(data.Txs, txIndex), nil
}

func proofTXExists(txs []rlktypes.Tx, i uint32) cmttypes.TxProof {
	hl := hashList(txs)
	root, proofs := merkle.ProofsFromByteSlices(hl)

	return cmttypes.TxProof{
		RootHash: root,
		Data:     cmttypes.Tx(txs[i]),
		Proof:    *proofs[i],
	}
}

func hashList(txs []rlktypes.Tx) [][]byte {
	hl := make([][]byte, len(txs))
	for i := 0; i < len(txs); i++ {
		hl[i] = tmhash.Sum(txs[i])
	}
	return hl
}
