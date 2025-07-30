package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/evstack/ev-abci/pkg/cometcompat"
)

const NodeIDByteLength = 20

func normalizeHeight(ctx context.Context, height *int64) (uint64, error) {
	var (
		heightValue uint64
		err         error
	)

	if height == nil || *height < 0 {
		// Handle negative heights if they have special meaning
		// (e.g., -1 for latest)
		heightValue, err = env.Adapter.RollkitStore.Height(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to get current height: %w", err)
		}
	} else {
		heightValue = uint64(*height)
	}

	return heightValue, nil
}

func getBlockMeta(ctx context.Context, n uint64) (*cmttypes.BlockMeta, *cmttypes.Block) {
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx, n)
	if err != nil {
		env.Logger.Error("Failed to get block data in getBlockMeta", "height", n, "err", err)
		return nil, nil
	}

	if header == nil || data == nil {
		env.Logger.Error("Nil header or data returned from GetBlockData", "height", n)
		return nil, nil
	}

	lastCommit, err := env.Adapter.GetLastCommit(ctx, n)
	if err != nil {
		env.Logger.Error("Failed to get last commit in getBlockMeta", "height", n, "err", err)
		return nil, nil
	}

	abciHeader, err := cometcompat.ToABCIHeader(header.Header, lastCommit)
	if err != nil {
		env.Logger.Error("Failed to convert header to ABCI format", "height", n, "err", err)
		return nil, nil
	}

	abciBlock, err := cometcompat.ToABCIBlock(abciHeader, lastCommit, data)
	if err != nil {
		env.Logger.Error("Failed to convert block to ABCI format", "height", n, "err", err)
		return nil, nil
	}

	abciBlockMeta, err := cometcompat.ToABCIBlockMeta(abciBlock)
	if err != nil {
		env.Logger.Error("Failed to convert block to ABCI block meta", "height", n, "err", err)
		return nil, nil
	}

	return abciBlockMeta, abciBlock
}

func filterMinMax(base, height, mini, maxi, limit int64) (int64, int64, error) {
	// filter negatives
	if mini < 0 || maxi < 0 {
		return mini, maxi, errors.New("height must be greater than zero")
	}

	// adjust for default values
	if mini == 0 {
		mini = 1
	}
	if maxi == 0 {
		maxi = height
	}

	// limit max to the height
	maxi = min(height, maxi)

	// limit min to the base
	mini = max(base, mini)

	// limit min to within `limit` of max
	// so the total number of blocks returned will be `limit`
	mini = max(mini, maxi-limit+1)

	if mini > maxi {
		return mini, maxi, fmt.Errorf("%w: min height %d can't be greater than max height %d",
			errors.New("invalid request"), mini, maxi)
	}
	return mini, maxi, nil
}

// TruncateNodeID from rollkit we receive a 32 bytes node id, but we only need the first 20 bytes
// to be compatible with the ABCI node info
func TruncateNodeID(idStr string) (string, error) {
	idBytes, err := hex.DecodeString(idStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode node ID: %w", err)
	}
	if len(idBytes) < NodeIDByteLength {
		return "", fmt.Errorf("node ID too short, expected at least %d bytes, got %d", NodeIDByteLength, len(idBytes))
	}
	return hex.EncodeToString(idBytes[:NodeIDByteLength]), nil
}
