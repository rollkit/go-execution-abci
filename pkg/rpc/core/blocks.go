package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmquery "github.com/cometbft/cometbft/libs/pubsub/query"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/gogoproto/proto"
	storepkg "github.com/rollkit/rollkit/pkg/store"
	rlktypes "github.com/rollkit/rollkit/types"

	abci "github.com/cometbft/cometbft/abci/types"
	networktypes "github.com/rollkit/go-execution-abci/modules/network/types"
	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
)

// BlockSearch searches for a paginated set of blocks matching BeginBlock and
// EndBlock event search criteria.
func BlockSearch(
	ctx *rpctypes.Context,
	query string,
	pagePtr, perPagePtr *int,
	orderBy string,
) (*ctypes.ResultBlockSearch, error) {
	wrappedCtx := ctx.Context()

	q, err := cmquery.New(query)
	if err != nil {
		return nil, err
	}

	results, err := env.BlockIndexer.Search(wrappedCtx, q)
	if err != nil {
		return nil, err
	}

	// Sort the results
	switch orderBy {
	case "desc":
		sort.Slice(results, func(i, j int) bool {
			return results[i] > results[j]
		})

	case "asc", "":
		sort.Slice(results, func(i, j int) bool {
			return results[i] < results[j]
		})
	default:
		return nil, errors.New("expected order_by to be either `asc` or `desc` or empty")
	}

	// Paginate
	totalCount := len(results)
	perPageVal := validatePerPage(perPagePtr)

	pageVal, err := validatePage(pagePtr, perPageVal, totalCount)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(pageVal, perPageVal)
	pageSize := min(perPageVal, totalCount-skipCount)

	blocks := make([]*ctypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		header, data, err := env.Adapter.RollkitStore.GetBlockData(wrappedCtx, uint64(results[i]))
		if err != nil {
			return nil, err
		}

		lastCommit, err := getLastCommit(wrappedCtx, uint64(results[i]))
		if err != nil {
			return nil, fmt.Errorf("failed to get last commit for block %d: %w", results[i], err)
		}

		block, err := cometcompat.ToABCIBlock(header, data, lastCommit)
		if err != nil {
			return nil, err
		}

		blocks = append(blocks, &ctypes.ResultBlock{
			Block: block,
			BlockID: cmttypes.BlockID{
				Hash: block.Hash(),
			},
		})
	}

	return &ctypes.ResultBlockSearch{Blocks: blocks, TotalCount: totalCount}, nil
}

// Block gets block at a given height.
// If no height is provided, it will fetch the latest block.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block
func Block(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultBlock, error) {
	var (
		heightValue uint64
		err         error
	)

	switch {
	case heightPtr != nil && *heightPtr == -1:
		rawVal, err := env.Adapter.RollkitStore.GetMetadata(
			ctx.Context(),
			storepkg.DAIncludedHeightKey,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get DA included height: %w", err)
		}

		if len(rawVal) != 8 {
			return nil, fmt.Errorf("invalid finalized height data length: %d", len(rawVal))
		}

		heightValue = binary.LittleEndian.Uint64(rawVal)
	default:
		heightValue, err = normalizeHeight(ctx.Context(), heightPtr)
		if err != nil {
			return nil, err
		}
	}

	block, err := xxxBlock(ctx, heightValue)
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{Hash: block.Hash()},
		Block:   block,
	}, nil
}

// BlockByHash gets block by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block_by_hash
func BlockByHash(ctx *rpctypes.Context, hash []byte) (*ctypes.ResultBlock, error) {
	// todo (Alex): quick hack for consistent hashes
	header, _, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash))
	if err != nil {
		return nil, err
	}

	block, err := xxxBlock(ctx, header.Height())
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{Hash: block.Hash()},
		Block:   block,
	}, nil
}

// Commit gets block commit at a given height.
// If no height is provided, it will fetch the commit for the latest block.
// More: https://docs.cometbft.com/main/rpc/#/Info/commit
func Commit(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultCommit, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, err
	}

	// Step 1: Get the header for the *requested* height H. This will be part of the response.
	// To get the correct header, we need the commit from H-1.
	// We can use xxxBlock for this, as it correctly constructs an ABCI block.
	abciBlockForH, err := xxxBlock(ctx, height)
	if err != nil {
		return nil, fmt.Errorf("failed to construct block for height %d: %w", height, err)
	}

	// Step 2: Build the commit for the *requested* height H.
	var commitForH *cmttypes.Commit
	if height > 0 {
		isSoftConfirmed, softConfirmationData, err := checkSoftConfirmation(ctx.Context(), height)
		if err != nil {
			return nil, fmt.Errorf("check soft confirmation status for commit height %d: %w", height, err)
		}
		if !isSoftConfirmed {
			env.Logger.Info("Block not soft-confirmed, generating commit with dummy signatures.", "height", height)
		}
		commitForH, err = buildCommitFromAttestations(ctx.Context(), height, softConfirmationData)
		if err != nil {
			return nil, fmt.Errorf("build commit from attestations for height %d: %w", height, err)
		}
	} else {
		// Genesis commit is empty.
		commitForH = &cmttypes.Commit{}
	}

	// Step 3: Crucial fix. The block hash in the commit must match the hash of the header we are returning.
	// The BlockID in commitForH was calculated using the rollkit header hash. We need to replace it.
	partSet, err := abciBlockForH.MakePartSet(cmttypes.BlockPartSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to make part set for block %d: %w", height, err)
	}
	commitForH.BlockID = cmttypes.BlockID{
		Hash:          abciBlockForH.Header.Hash(),
		PartSetHeader: partSet.Header(),
	}

	return &ctypes.ResultCommit{
		SignedHeader: cmttypes.SignedHeader{
			Header: &abciBlockForH.Header,
			Commit: commitForH,
		},
		CanonicalCommit: true,
	}, nil
}

func xxxBlock(ctx *rpctypes.Context, height uint64) (*cmttypes.Block, error) {
	header, rollkitData, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), height)
	if err != nil {
		return nil, err
	}

	var commitHeight uint64
	if height > 1 {
		commitHeight = height - 1
	} else {
		commitHeight = 0
	}

	var commit *cmttypes.Commit
	if commitHeight == 0 {
		commit = &cmttypes.Commit{
			Height:     0,
			Round:      0,
			BlockID:    cmttypes.BlockID{},
			Signatures: []cmttypes.CommitSig{},
		}
	} else {
		isSoftConfirmed, softConfirmationData, err := checkSoftConfirmation(ctx.Context(), commitHeight)
		if err != nil {
			return nil, fmt.Errorf("check soft confirmation status for commit height %d: %w", commitHeight, err)
		}
		if !isSoftConfirmed {
			// Use dummy empty commit for non-confirmed blocks (reverted for compatibility)
			commit = &cmttypes.Commit{
				Height:     int64(commitHeight),
				Round:      0,
				BlockID:    cmttypes.BlockID{},
				Signatures: []cmttypes.CommitSig{},
			}
			env.Logger.Info("Using dummy commit for non-confirmed block (compatibility mode)", "height", commitHeight)
			// Proceed without error
		}
		commit, err = buildCommitFromAttestations(ctx.Context(), commitHeight, softConfirmationData)
		if err != nil {
			return nil, fmt.Errorf("build commit from attestations: %w", err)
		}
	}

	block, err := cometcompat.ToABCIBlock(header, rollkitData, commit)
	if err != nil {
		return nil, err
	}

	// Update the commit's BlockID to match the PREVIOUS block hash
	block.LastCommit.BlockID.Hash = commit.BlockID.Hash
	block.LastCommit.BlockID.PartSetHeader.Hash = commit.BlockID.Hash

	return block, nil
}

// BlockResults gets block results at a given height.
// If no height is provided, it will fetch the results for the latest block.
func BlockResults(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultBlockResults, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, err
	}

	resp, err := env.Adapter.Store.GetBlockResponse(ctx.Context(), height)
	if err != nil {
		return nil, err
	}

	return &ctypes.ResultBlockResults{
		Height:                int64(height),
		TxsResults:            resp.TxResults,
		FinalizeBlockEvents:   resp.Events,
		ValidatorUpdates:      resp.ValidatorUpdates,
		ConsensusParamUpdates: resp.ConsensusParamUpdates,
		AppHash:               resp.AppHash,
	}, nil
}

// Header gets block header at a given height.
// If no height is provided, it will fetch the latest header.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header
func Header(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultHeader, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, err
	}

	// todo (Alex): quick hack to get a consistent block header
	block, err := xxxBlock(ctx, height)
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultHeader{Header: &block.Header}, nil
}

// HeaderByHash gets header by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header_by_hash
func HeaderByHash(ctx *rpctypes.Context, hash cmbytes.HexBytes) (*ctypes.ResultHeader, error) {
	// N.B. The hash parameter is HexBytes so that the reflective parameter
	// decoding logic in the HTTP service will correctly translate from JSON.
	// See https://github.com/cometbft/cometbft/issues/6802 for context.

	// todo (Alex): quick hack for consistent block headers

	res, err := BlockByHash(ctx, hash.Bytes())
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("block not found")
	}
	return &ctypes.ResultHeader{Header: &res.Block.Header}, nil

}

// BlockchainInfo gets block headers for minHeight <= height <= maxHeight.
// Block headers are returned in descending order (highest first).
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/blockchain
func BlockchainInfo(ctx *rpctypes.Context, minHeight, maxHeight int64) (*ctypes.ResultBlockchainInfo, error) {
	const limit int64 = 20

	height, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, err
	}

	// Currently blocks are not pruned and are synced linearly so the base height is 0
	minHeight, maxHeight, err = filterMinMax(
		0,
		int64(height), //nolint:gosec
		minHeight,
		maxHeight,
		limit)
	if err != nil {
		return nil, err
	}
	env.Logger.Debug("BlockchainInfo", "maxHeight", maxHeight, "minHeight", minHeight)

	blocks := make([]*cmttypes.BlockMeta, 0, maxHeight-minHeight+1)
	for _, block := range BlockIterator(ctx.Context(), maxHeight, minHeight) {
		if block.header != nil && block.data != nil {
			lastCommit, err := getLastCommit(ctx.Context(), block.header.Height())
			if err != nil {
				return nil, fmt.Errorf("failed to get last commit for block %d: %w", block.header.Height(), err)
			}

			cmblockmeta, err := cometcompat.ToABCIBlockMeta(block.header, block.data, lastCommit)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, cmblockmeta)
		}
	}

	return &ctypes.ResultBlockchainInfo{
		LastHeight: int64(height), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}

// checkSoftConfirmation checks if a block has soft confirmation and returns the attestation data
func checkSoftConfirmation(ctx context.Context, height uint64) (bool, *networktypes.QueryAttestationBitmapResponse, error) {
	// Check soft confirmation status
	softConfirmReq := &networktypes.QuerySoftConfirmationStatusRequest{
		Height: int64(height),
	}
	reqData, err := softConfirmReq.Marshal()
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal soft confirmation request: %w", err)
	}

	// Query soft confirmation status
	abciReq := &abci.RequestQuery{
		Path: "/rollkitsdk.network.v1.Query/SoftConfirmationStatus",
		Data: reqData,
	}

	abciRes, err := env.Adapter.App.Query(ctx, abciReq)
	if err != nil || abciRes.Code != 0 {
		var msg string
		if abciRes != nil {
			msg = abciRes.Log
		}
		env.Logger.Error("query soft confirmation status", "height", height, "error", err, "log", msg)
		return false, nil, fmt.Errorf("failed to query soft confirmation status: %w", err)
	}

	softConfirmResp := &networktypes.QuerySoftConfirmationStatusResponse{}
	if err := softConfirmResp.Unmarshal(abciRes.Value); err != nil {
		return false, nil, fmt.Errorf("failed to unmarshal soft confirmation response: %w", err)
	}

	if !softConfirmResp.IsSoftConfirmed {
		return false, nil, nil
	}

	// Get attestation bitmap data
	attestationReq := &networktypes.QueryAttestationBitmapRequest{
		Height: int64(height),
	}
	reqData, err = attestationReq.Marshal()
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal attestation bitmap request: %w", err)
	}

	abciReq = &abci.RequestQuery{
		Path: "/rollkitsdk.network.v1.Query/AttestationBitmap",
		Data: reqData,
	}

	abciRes, err = env.Adapter.App.Query(ctx, abciReq)
	if err != nil || abciRes.Code != 0 {
		var msg string
		if abciRes != nil {
			msg = abciRes.Log
		}
		env.Logger.Error("query attestation bitmap", "height", height, "error", err, "log", msg)
		return false, nil, fmt.Errorf("failed to query attestation bitmap: %w", err)
	}

	var attestationResp networktypes.QueryAttestationBitmapResponse
	if err := attestationResp.Unmarshal(abciRes.Value); err != nil {
		return false, nil, fmt.Errorf("failed to unmarshal attestation bitmap response: %w", err)
	}

	return true, &attestationResp, nil
}

// buildCommitFromAttestations constructs a commit with real signatures from attestations
func buildCommitFromAttestations(ctx context.Context, height uint64, attestationData *networktypes.QueryAttestationBitmapResponse) (*cmttypes.Commit, error) {
	// Base case for height 1 or 0: empty commit
	if height <= 1 {
		return &cmttypes.Commit{
			Height:     int64(height),
			Round:      0,
			BlockID:    cmttypes.BlockID{},
			Signatures: []cmttypes.CommitSig{},
		}, nil
	}

	// Get header and data for current height
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx, height)
	if err != nil {
		return nil, fmt.Errorf("failed to get block data for height %d: %w", height, err)
	}

	// Recursively get commit for previous height (LastCommit for this block)
	var lastCommit *cmttypes.Commit
	if height > 1 {
		_, lastAttestationData, err := checkSoftConfirmation(ctx, height-1)
		if err != nil {
			return nil, fmt.Errorf("failed to check soft confirmation for height %d: %w", height-1, err)
		}

		// Recurse with potentially nil attestation data, the function handles it.
		lastCommit, err = buildCommitFromAttestations(ctx, height-1, lastAttestationData)
		if err != nil {
			return nil, fmt.Errorf("failed to get commit for previous height %d: %w", height-1, err)
		}
	} else {
		// Base case for height 0 or 1
		lastCommit = &cmttypes.Commit{Height: int64(height - 1)}
	}

	// Convert to ABCI block using real LastCommit to compute accurate hash
	abciBlock, err := cometcompat.ToABCIBlock(header, data, lastCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to ABCI block: %w", err)
	}

	partSet, err := abciBlock.MakePartSet(cmttypes.BlockPartSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to make part set for block %d: %w", height, err)
	}
	blockID := cmttypes.BlockID{
		Hash:          abciBlock.Hash(),
		PartSetHeader: partSet.Header(),
	}

	// Now build the real commit for this height using the computed hash
	// Get validators from genesis
	genesisValidators := env.Adapter.AppGenesis.Consensus.Validators
	if len(genesisValidators) == 0 {
		return nil, fmt.Errorf("no validators found in genesis")
	}
	votes := make([]cmttypes.CommitSig, len(genesisValidators))

	var bitmap []byte
	if attestationData != nil {
		if bitmap = attestationData.Bitmap.Bitmap; bitmap == nil {
			return nil, fmt.Errorf("no attestation bitmap found for height %d", height)
		}
	}
	// Iterate only through the actual validators (not all bits in bitmap)
	for i, genesisValidator := range genesisValidators {
		// Check if this validator voted (bit is set in bitmap)
		if attestationData != nil && i < len(bitmap)*8 && (bitmap[i/8]&(1<<(i%8))) != 0 {
			// Try to get the real signature using the validator's address
			validatorAddr := string(genesisValidator.Address.Bytes()) // todo (Alex): use proper format
			vote, err := getValidatorSignatureFromQuery(ctx, int64(height), validatorAddr)
			if err != nil {
				return nil, fmt.Errorf("get validator signature for height %d: %w", height, err)
			}

			votes[i] = cmttypes.CommitSig{
				BlockIDFlag:      cmttypes.BlockIDFlagCommit,
				ValidatorAddress: vote.ValidatorAddress,
				Timestamp:        vote.Timestamp,
				Signature:        vote.Signature,
			}

		} else {
			// Validator didn't vote, add absent vote
			votes[i] = cmttypes.CommitSig{
				BlockIDFlag: cmttypes.BlockIDFlagAbsent,
			}
		}
	}

	commit := &cmttypes.Commit{
		Height:     int64(height),
		Round:      0,
		BlockID:    blockID,
		Signatures: votes,
	}

	return commit, nil
}

// getValidatorSignatureFromQuery queries the signature for a specific validator
func getValidatorSignatureFromQuery(ctx context.Context, height int64, validatorAddr string) (*cmtproto.Vote, error) {
	sigReq := &networktypes.QueryValidatorSignatureRequest{
		BlockHeight: height,
		Validator:   validatorAddr,
	}

	reqData, err := sigReq.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signature request: %w", err)
	}

	fmt.Printf("+++ getValidatorSignatureFromQuery addr: %X, height: %d\n", []byte(validatorAddr), height)

	abciReq := &abci.RequestQuery{
		Path: "/rollkitsdk.network.v1.Query/ValidatorSignature",
		Data: reqData,
	}

	res, err := env.Adapter.App.Query(ctx, abciReq)
	if err != nil {
		return nil, fmt.Errorf("signature query failed: %w", err)
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("signature query failed: %s", res.Log)
	}

	var sigResp networktypes.QueryValidatorSignatureResponse
	if err := sigResp.Unmarshal(res.Value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal signature response: %w", err)
	}
	if !sigResp.Found {
		return nil, fmt.Errorf("vote not found")
	}
	var vote cmtproto.Vote
	if err := proto.Unmarshal(sigResp.Signature, &vote); err != nil {
		return nil, fmt.Errorf("failed to unmarshal signature payload: %w", err)
	}
	return &vote, nil
}
