package rpc

import (
	"context"
	"errors"
	"fmt"
	"sort"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	blockidxnull "github.com/cometbft/cometbft/state/indexer/block/null"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/rollkit/types"
)

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
	// Currently, this method returns an empty result as the original implementation was commented out.
	// If block results become available, this implementation should be updated.
	_ = r.normalizeHeight(height) // Use height to avoid unused variable error, logic depends on future implementation
	return &coretypes.ResultBlockResults{}, nil
	// Original commented-out logic:
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
	// resp, err := r.adapter.Store.GetBlockResponses(ctx, h)
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
		// Return empty result without error if block not found by hash, consistent with original behaviour?
		// Or return an error? fmt.Errorf("block with hash %X not found", hash)
		return &coretypes.ResultHeader{}, nil // Current behaviour matches original code
	}

	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// BlockchainInfo implements client.CometRPC.
func (r *RPCServer) BlockchainInfo(ctx context.Context, minHeight int64, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	const limit int64 = 20 // Default limit used in the original code

	height, err := r.adapter.Store.Height(ctx)
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
		limit)
	if err != nil {
		return nil, err
	}

	blocks := make([]*cmttypes.BlockMeta, 0, maxHeight-minHeight+1)
	for h := maxHeight; h >= minHeight; h-- {
		// Use getBlockMeta which handles errors and nil checks internally
		bMeta := r.getBlockMeta(ctx, uint64(h))
		if bMeta != nil {
			blocks = append(blocks, bMeta)
		}
		// Decide if we should continue or return error if getBlockMeta fails for a height in range.
		// Current behaviour: skip the block if meta retrieval fails.
	}

	// Re-fetch height in case new blocks were added during the loop?
	// The original code did this.
	finalHeight, err := r.adapter.Store.Height(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get final height: %w", err)
	}

	return &coretypes.ResultBlockchainInfo{
		LastHeight: int64(finalHeight), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}

// BlockSearch implements client.CometRPC.
func (r *RPCServer) BlockSearch(ctx context.Context, query string, pagePtr *int, perPagePtr *int, orderBy string) (*coretypes.ResultBlockSearch, error) {
	// skip if block indexing is disabled
	if _, ok := r.blockIndexer.(*blockidxnull.BlockerIndexer); ok {
		return nil, errors.New("block indexing is disabled")
	}

	if len(query) > maxQueryLength {
		return nil, errors.New("maximum query length exceeded")
	}

	q, err := cmtquery.New(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	results, err := r.blockIndexer.Search(ctx, q)
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
	perPage := validatePerPage(perPagePtr)

	page, err := validatePage(pagePtr, perPage, totalCount)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(page, perPage)
	pageSize := cmtmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*coretypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		height := uint64(results[i])
		header, data, err := r.adapter.Store.GetBlockData(ctx, height)
		if err != nil {
			// If a block referenced by indexer is missing, should we error out or just skip?
			// For now, error out.
			return nil, fmt.Errorf("failed to get block data for height %d from store: %w", height, err)
		}
		if header == nil || data == nil {
			return nil, fmt.Errorf("nil header or data for height %d from store", height)
		}
		block, err := ToABCIBlock(header, data)
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
