package core

import (
	"fmt"
	"time"

	"github.com/cometbft/cometbft/config"
	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	corep2p "github.com/cometbft/cometbft/p2p"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cometbft/cometbft/version"
)

// Status returns CometBFT status including node info, pubkey, latest block
// hash, app hash, block height and time.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/status
func Status(ctx *rpctypes.Context) (*ctypes.ResultStatus, error) {
	unwrappedCtx := ctx.Context()

	var (
		latestBlockHash cmbytes.HexBytes
		latestAppHash   cmbytes.HexBytes
		latestBlockTime time.Time
	)

	latestHeight, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get current height: %w", err)
	}

	// Find the last soft-confirmed height by iterating backwards
	var lastConfirmedHeight uint64
	for h := latestHeight; h > 0; h-- {
		isSoftConfirmed, _, err := checkSoftConfirmation(unwrappedCtx, h)
		if err != nil {
			return nil, fmt.Errorf("failed to check soft confirmation for height %d: %w", h, err)
		}
		if isSoftConfirmed {
			lastConfirmedHeight = h
			break
		}
	}

	// If no confirmed height found (edge case), fallback to 0 or handle appropriately
	if lastConfirmedHeight == 0 {
		return nil, fmt.Errorf("no soft-confirmed height found")
	}

	// Use the confirmed height for block data
	if lastConfirmedHeight > 10 { // Adjust the hack if needed
		lastConfirmedHeight -= 10 // Quick hack to reduce sync diff for relayer
	}
	block, err := xxxBlock(ctx, lastConfirmedHeight)
	if err != nil {
		return nil, err
	}
	latestBlockHash = block.Hash()
	latestAppHash = block.AppHash
	latestBlockTime = block.Time

	initialHeader, _, err := env.Adapter.RollkitStore.GetBlockData(unwrappedCtx, uint64(env.Adapter.AppGenesis.InitialHeight))
	if err != nil {
		return nil, fmt.Errorf("failed to find earliest block: %w", err)
	}

	genesisValidators := env.Adapter.AppGenesis.Consensus.Validators
	if len(genesisValidators) != 1 {
		return nil, fmt.Errorf("there should be exactly one validator in genesis")
	}

	// Changed behavior to get this from genesis
	genesisValidator := genesisValidators[0]
	validator := cmttypes.Validator{
		Address:     genesisValidator.Address,
		PubKey:      genesisValidator.PubKey,
		VotingPower: genesisValidator.Power,
	}

	state, err := env.Adapter.RollkitStore.GetState(unwrappedCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to load the last saved state: %w", err)
	}
	defaultProtocolVersion := corep2p.NewProtocolVersion(
		version.P2PProtocol,
		version.BlockProtocol,
		state.Version.App,
	)
	id, addr, network, err := env.Adapter.P2PClient.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to load node p2p2 info: %w", err)
	}

	processedID, err := TruncateNodeID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to process node ID: %w", err)
	}
	id = processedID

	txIndexerStatus := "on"

	result := &ctypes.ResultStatus{
		NodeInfo: corep2p.DefaultNodeInfo{
			ProtocolVersion: defaultProtocolVersion,
			DefaultNodeID:   corep2p.ID(id),
			ListenAddr:      addr,
			Network:         network,
			Version:         version.TMCoreSemVer,
			Moniker:         config.DefaultBaseConfig().Moniker,
			Other: corep2p.DefaultNodeInfoOther{
				TxIndex:    txIndexerStatus,
				RPCAddress: env.Config.ListenAddress,
			},
		},
		SyncInfo: ctypes.SyncInfo{
			LatestBlockHash:     latestBlockHash,
			LatestAppHash:       latestAppHash,
			LatestBlockHeight:   int64(lastConfirmedHeight),
			LatestBlockTime:     latestBlockTime,
			EarliestBlockHash:   cmbytes.HexBytes(initialHeader.DataHash),
			EarliestAppHash:     cmbytes.HexBytes(initialHeader.AppHash),
			EarliestBlockHeight: int64(initialHeader.Height()),
			EarliestBlockTime:   initialHeader.Time(),
			CatchingUp:          false, // hard-code this to "false" to pass Go IBC relayer's legacy encoding check
		},
		ValidatorInfo: ctypes.ValidatorInfo{
			Address:     validator.Address,
			PubKey:      validator.PubKey,
			VotingPower: validator.VotingPower,
		},
	}

	return result, nil
}
