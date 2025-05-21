package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/rollkit/go-execution-abci/pkg/common"
)

const NodeIDByteLength = 20

func normalizeHeight(height *int64) uint64 {
	var heightValue uint64
	if height == nil {
		var err error
		// TODO: Decide how to handle context here. Using background for now.
		heightValue, err = env.Adapter.RollkitStore.Height(context.Background())
		if err != nil {
			// TODO: Consider logging or returning error
			env.Logger.Error("Failed to get current height in normalizeHeight", "err", err)
			return 0
		}
	} else if *height < 0 {
		// Handle negative heights if they have special meaning (e.g., -1 for latest)
		// Currently, just treat them as 0 or latest, adjust as needed.
		// For now, let's assume negative height means latest valid height.
		var err error
		heightValue, err = env.Adapter.RollkitStore.Height(context.Background())
		if err != nil {
			env.Logger.Error("Failed to get current height for negative height in normalizeHeight", "err", err)
			return 0
		}
	} else {
		heightValue = uint64(*height)
	}

	return heightValue
}

func getBlockMeta(ctx context.Context, n uint64) *cmttypes.BlockMeta {
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx, n)
	if err != nil {
		env.Logger.Error("Failed to get block data in getBlockMeta", "height", n, "err", err)
		return nil
	}
	if header == nil || data == nil {
		env.Logger.Error("Nil header or data returned from GetBlockData", "height", n)
		return nil
	}
	// Assuming ToABCIBlockMeta is now in pkg/rpc/provider/provider_utils.go
	bmeta, err := common.ToABCIBlockMeta(header, data) // Removed rpc. prefix
	if err != nil {
		env.Logger.Error("Failed to convert block to ABCI block meta", "height", n, "err", err)
		return nil
	}

	return bmeta
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
