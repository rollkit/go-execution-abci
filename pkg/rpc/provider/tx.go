package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"

	cmtmath "github.com/cometbft/cometbft/libs/math"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/state/txindex/null"
	cmttypes "github.com/cometbft/cometbft/types"
)

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

	if len(query) > maxQueryLength {
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
	perPage := validatePerPage(perPagePtr)                  // Removed rpc. prefix
	page, err := validatePage(pagePtr, perPage, totalCount) // Removed rpc. prefix
	if err != nil {
		return nil, err
	}
	skipCount := validateSkipCount(page, perPage) // Removed rpc. prefix
	pageSize := cmtmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*coretypes.ResultTx, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		result := txResults[i]
		var proof cmttypes.TxProof
		if prove {
			// Proof generation is currently not supported.
			return nil, errors.New("transaction proof generation is not supported")
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
