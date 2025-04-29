package provider

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

	rlktypes "github.com/rollkit/rollkit/types"
)

// Genesis implements client.Client.
func (p *RpcProvider) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	// Returning unimplemented as per the original code.
	// Consider implementing or returning a more specific error if needed.
	panic("unimplemented")
}

// GenesisChunked implements client.Client.
func (p *RpcProvider) GenesisChunked(context.Context, uint) (*coretypes.ResultGenesisChunk, error) {
	return nil, errors.New("GenesisChunked RPC method is not yet implemented")
}

// Block implements client.CometRPC.
func (p *RpcProvider) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	var heightValue uint64

	switch {
	// block tag = included
	case height != nil && *height == -1:
		// heightValue = p.adapter.store.GetDAIncludedHeight()
		// TODO: implement
		return nil, errors.New("DA included height not implemented")
	default:
		heightValue = p.normalizeHeight(height)
	}
	header, data, err := p.adapter.Store.GetBlockData(ctx, heightValue)
	if err != nil {
		return nil, err
	}

	hash := header.Hash()
	abciBlock, err := ToABCIBlock(header, data) // Use local function
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
func (p *RpcProvider) BlockByHash(ctx context.Context, hash []byte) (*coretypes.ResultBlock, error) {
	header, data, err := p.adapter.Store.GetBlockByHash(ctx, rlktypes.Hash(hash)) // Used types.Hash from rollkit/types
	if err != nil {
		return nil, err
	}

	abciBlock, err := ToABCIBlock(header, data) // Use local function
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
func (p *RpcProvider) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	// Currently, this method returns an empty result as the original implementation was commented out.
	// If block results become available, this implementation should be updated.
	_ = p.normalizeHeight(height) // Use height to avoid unused variable error, logic depends on future implementation
	return &coretypes.ResultBlockResults{}, nil
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

// Commit implements client.CometRPC.
func (p *RpcProvider) Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error) {
	heightValue := p.normalizeHeight(height)
	header, data, err := p.adapter.Store.GetBlockData(ctx, heightValue)
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

	return coretypes.NewResultCommit(&block.Header, commit, true), nil
}

// Header implements client.Client.
func (p *RpcProvider) Header(ctx context.Context, heightPtr *int64) (*coretypes.ResultHeader, error) {
	height := p.normalizeHeight(heightPtr)
	blockMeta := p.getBlockMeta(ctx, height)
	if blockMeta == nil {
		return nil, fmt.Errorf("block at height %d not found", height)
	}
	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// HeaderByHash implements client.Client.
func (p *RpcProvider) HeaderByHash(ctx context.Context, hash cmtbytes.HexBytes) (*coretypes.ResultHeader, error) {
	// N.B. The hash parameter is HexBytes so that the reflective parameter
	// decoding logic in the HTTP service will correctly translate from JSON.
	// See https://github.com/cometbft/cometbft/issues/6802 for context.

	header, data, err := p.adapter.Store.GetBlockByHash(ctx, rlktypes.Hash(hash)) // Used types.Hash from rollkit/types
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
		return &coretypes.ResultHeader{}, nil // Current behaviour matches original code
	}

	return &coretypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// BlockchainInfo implements client.CometRPC.
func (p *RpcProvider) BlockchainInfo(ctx context.Context, minHeight int64, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	const limit int64 = 20 // Default limit used in the original code

	height, err := p.adapter.Store.Height(ctx)
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
		bMeta := p.getBlockMeta(ctx, uint64(h))
		if bMeta != nil {
			blocks = append(blocks, bMeta)
		}
		// Decide if we should continue or return error if getBlockMeta fails for a height in range.
		// Current behaviour: skip the block if meta retrieval fails.
	}

	// Re-fetch height in case new blocks were added during the loop?
	// The original code did this.
	finalHeight, err := p.adapter.Store.Height(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get final height: %w", err)
	}

	return &coretypes.ResultBlockchainInfo{
		LastHeight: int64(finalHeight), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}

// BlockSearch implements client.CometRPC.
func (p *RpcProvider) BlockSearch(ctx context.Context, query string, pagePtr *int, perPagePtr *int, orderBy string) (*coretypes.ResultBlockSearch, error) {
	// skip if block indexing is disabled
	if p.blockIndexer == nil {
		return nil, errors.New("block indexer is not available")
	}
	if _, ok := p.blockIndexer.(*blockidxnull.BlockerIndexer); ok {
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

	results, err := p.blockIndexer.Search(ctx, q)
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

	apiResults := make([]*coretypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		height := uint64(results[i])
		header, data, err := p.adapter.Store.GetBlockData(ctx, height)
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
		apiResults = append(apiResults, &coretypes.ResultBlock{
			Block: block,
			BlockID: cmttypes.BlockID{
				Hash: block.Hash(),
			},
		})
	}

	return &coretypes.ResultBlockSearch{Blocks: apiResults, TotalCount: totalCount}, nil
}

// Validators implements client.CometRPC.
func (p *RpcProvider) Validators(ctx context.Context, heightPtr *int64, pagePtr *int, perPagePtr *int) (*coretypes.ResultValidators, error) {
	// Determine the height to query validators for.
	// If height is nil or latest, use the current block height.
	// Otherwise, use the specified height (if state for that height is available).
	// Note: Loading state for arbitrary past heights might not be supported
	// depending on state pruning. The current implementation implicitly loads latest state.
	height := p.normalizeHeight(heightPtr)

	s, err := p.adapter.LoadState(ctx) // Loads the *latest* state
	if err != nil {
		return nil, fmt.Errorf("failed to load current state: %w", err)
	}

	// Check if the requested height matches the loaded state height if a specific height was requested.
	// If state history is not kept, this check might be necessary or always fail for past heights.
	if heightPtr != nil && int64(height) != s.LastBlockHeight {
		// This implies state for the requested height is not available with the current LoadState method.
		// Adjust implementation if historical state access is possible and needed.
		return nil, fmt.Errorf("validator set for height %d is not available, latest height is %d", height, s.LastBlockHeight)
	}

	validators := s.Validators.Validators
	totalCount := len(validators)

	// Handle pagination
	perPage := validatePerPage(perPagePtr)                  // Use local function
	page, err := validatePage(pagePtr, perPage, totalCount) // Use local function
	if err != nil {
		return nil, err
	}

	start := validateSkipCount(page, perPage) // Use local function
	end := start + perPage
	if end > totalCount {
		end = totalCount
	}

	// Ensure start index is not out of bounds, can happen if page * perPage > totalCount
	if start >= totalCount {
		validators = []*cmttypes.Validator{} // Return empty slice if page is out of range
	} else {
		validators = validators[start:end]
	}

	return &coretypes.ResultValidators{
		BlockHeight: s.LastBlockHeight, // Return the height for which the validator set is valid (latest)
		Validators:  validators,
		Total:       totalCount, // Total number of validators *before* pagination
	}, nil
}
