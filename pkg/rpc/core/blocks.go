package core

import (
	"errors"
	"fmt"
	"sort"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	blockidxnull "github.com/cometbft/cometbft/state/indexer/block/null"
	cmttypes "github.com/cometbft/cometbft/types"

	rlktypes "github.com/rollkit/rollkit/types"
)

// BlockSearch searches for a paginated set of blocks matching BeginBlock and
// EndBlock event search criteria.
func BlockSearch(
	ctx *rpctypes.Context,
	query string,
	pagePtr, perPagePtr *int,
	orderBy string,
) (*ctypes.ResultBlockSearch, error) {
	// skip if block indexing is disabled
	if env.BlockIndexer == nil {
		return nil, errors.New("block indexer is not available")
	}
	if _, ok := env.BlockIndexer.(*blockidxnull.BlockerIndexer); ok {
		return nil, errors.New("block indexing is disabled")
	}

	// Use the locally defined maxQueryLength from provider_utils.go
	if len(query) > maxQueryLength {
		return nil, errors.New("maximum query length exceeded")
	}

	q, err := cmtquery.New(query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	results, err := env.BlockIndexer.Search(ctx.Context(), q)
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
	perPage := validatePerPage(perPagePtr) // Use local function

	page, err := validatePage(pagePtr, perPage, totalCount) // Use local function
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(page, perPage) // Use local function
	pageSize := cmtmath.MinInt(perPage, totalCount-skipCount)

	apiResults := make([]*ctypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		height := uint64(results[i])
		header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), height)
		if err != nil {
			// If a block referenced by indexer is missing, should we error out or just skip?
			// For now, error out.
			return nil, fmt.Errorf("failed to get block data for height %d from store: %w", height, err)
		}
		if header == nil || data == nil {
			return nil, fmt.Errorf("nil header or data for height %d from store", height)
		}
		block, err := ToABCIBlock(header, data) // Use local function
		if err != nil {
			return nil, fmt.Errorf("failed to convert block at height %d to ABCI block: %w", height, err)
		}
		apiResults = append(apiResults, &ctypes.ResultBlock{
			Block: block,
			BlockID: cmttypes.BlockID{
				Hash: block.Hash(),
			},
		})
	}

	return &ctypes.ResultBlockSearch{Blocks: apiResults, TotalCount: totalCount}, nil
}

// Block gets block at a given height.
// If no height is provided, it will fetch the latest block.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block
func Block(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultBlock, error) {
	var heightValue uint64

	switch {
	// block tag = included
	case heightPtr != nil && *heightPtr == -1:
		// heightValue = p.adapter.store.GetDAIncludedHeight()
		// TODO: implement
		return nil, errors.New("DA included height not implemented")
	default:
		heightValue = normalizeHeight(heightPtr)
	}
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), heightValue)
	if err != nil {
		return nil, err
	}

	hash := header.Hash()
	abciBlock, err := ToABCIBlock(header, data) // Use local function
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultBlock{
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

// BlockByHash gets block by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block_by_hash
func BlockByHash(ctx *rpctypes.Context, hash []byte) (*ctypes.ResultBlock, error) {
	header, data, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash)) // Used types.Hash from rollkit/types
	if err != nil {
		return nil, err
	}

	abciBlock, err := ToABCIBlock(header, data) // Use local function
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultBlock{
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

// Commit gets block commit at a given height.
// If no height is provided, it will fetch the commit for the latest block.
// More: https://docs.cometbft.com/main/rpc/#/Info/commit
func Commit(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultCommit, error) {
	heightValue := normalizeHeight(heightPtr)
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), heightValue)
	if err != nil {
		return nil, err
	}

	// we should have a single validator
	if len(header.ProposerAddress) == 0 {
		return nil, errors.New("empty validator set found in block")
	}

	commit := getABCICommit(heightValue, header.Hash(), header.ProposerAddress, header.Time(), header.Signature) // Use local function

	block, err := ToABCIBlock(header, data) // Use local function
	if err != nil {
		return nil, err
	}

	return ctypes.NewResultCommit(&block.Header, commit, true), nil
}

func BlockResults(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultBlockResults, error) {
	// Currently, this method returns an empty result as the original implementation was commented out.
	// If block results become available, this implementation should be updated.
	_ = normalizeHeight(heightPtr) // Use height to avoid unused variable error, logic depends on future implementation
	return &ctypes.ResultBlockResults{}, nil
	// Original commented-out logic:
	// var h uint64
	// if height == nil {
	// 	 h = p.adapter.Store.Height(ctx)
	// } else {
	// 	 h = uint64(*height)
	// }
	// header, _, err := p.adapter.Store.GetBlockData(ctx, h)
	// if err != nil {
	// 	 return nil, err
	// }
	// resp, err := p.adapter.Store.GetBlockResponses(ctx, h)
	// if err != nil {
	// 	 return nil, err
	// }
	// return &coretypes.ResultBlockResults{
	// 	 Height:                int64(h), //nolint:gosec
	// 	 TxsResults:            resp.TxResults,
	// 	 FinalizeBlockEvents:   resp.Events,
	// 	 ValidatorUpdates:      resp.ValidatorUpdates,
	// 	 ConsensusParamUpdates: resp.ConsensusParamUpdates,
	// 	 AppHash:               header.Header.AppHash,
	// }, nil
}

// Header gets block header at a given height.
// If no height is provided, it will fetch the latest header.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header
func Header(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultHeader, error) {
	height := normalizeHeight(heightPtr)
	blockMeta := getBlockMeta(ctx.Context(), height)
	if blockMeta == nil {
		return nil, fmt.Errorf("block at height %d not found", height)
	}
	return &ctypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// HeaderByHash gets header by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header_by_hash
func HeaderByHash(ctx *rpctypes.Context, hash cmtbytes.HexBytes) (*ctypes.ResultHeader, error) {
	// N.B. The hash parameter is HexBytes so that the reflective parameter
	// decoding logic in the HTTP service will correctly translate from JSON.
	// See https://github.com/cometbft/cometbft/issues/6802 for context.

	header, data, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash)) // Used types.Hash from rollkit/types
	if err != nil {
		return nil, err
	}

	blockMeta, err := ToABCIBlockMeta(header, data) // Use local function
	if err != nil {
		return nil, err
	}

	if blockMeta == nil {
		// Return empty result without error if block not found by hash, consistent with original behaviour?
		// Or return an error? fmt.Errorf("block with hash %X not found", hash)
		return &ctypes.ResultHeader{}, nil // Current behaviour matches original code
	}

	return &ctypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// BlockchainInfo gets block headers for minHeight <= height <= maxHeight.
// Block headers are returned in descending order (highest first).
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/blockchain
func BlockchainInfo(ctx *rpctypes.Context, minHeight, maxHeight int64) (*ctypes.ResultBlockchainInfo, error) {
	const limit int64 = 20 // Default limit used in the original code

	height, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get current height: %w", err)
	}

	// Assuming base height is 1 as blocks are 1-indexed, adjust if base is different.
	// The original filterMinMax used 0, but blockchain heights typically start at 1.
	const baseHeight int64 = 1
	minHeight, maxHeight, err = filterMinMax( // Use local function
		baseHeight,
		int64(height),
		minHeight,
		maxHeight,
		limit,
	)
	if err != nil {
		return nil, err
	}

	blocks := make([]*cmttypes.BlockMeta, 0, maxHeight-minHeight+1)
	for h := maxHeight; h >= minHeight; h-- {
		// Use getBlockMeta which handles errors and nil checks internally
		bMeta := getBlockMeta(ctx.Context(), uint64(h))
		if bMeta != nil {
			blocks = append(blocks, bMeta)
		}
		// Decide if we should continue or return error if getBlockMeta fails for a height in range.
		// Current behaviour: skip the block if meta retrieval fails.
	}

	// Re-fetch height in case new blocks were added during the loop?
	// The original code did this.
	finalHeight, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get final height: %w", err)
	}

	return &ctypes.ResultBlockchainInfo{
		LastHeight: int64(finalHeight), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}
