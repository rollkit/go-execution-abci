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
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	if latestHeight != 0 {
		header, _, err := env.Adapter.RollkitStore.GetBlockData(unwrappedCtx, latestHeight)
		if err != nil {
			return nil, fmt.Errorf("failed to find latest block: %w", err)
		}
		latestBlockHash = cmbytes.HexBytes(header.DataHash)
		latestAppHash = cmbytes.HexBytes(header.AppHash)
		latestBlockTime = header.Time()
	}

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
		VotingPower: int64(1),
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
			LatestBlockHeight:   int64(latestHeight),
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
