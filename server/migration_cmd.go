package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	cometbftcmd "github.com/cometbft/cometbft/cmd/cometbft/commands"
	cfg "github.com/cometbft/cometbft/config"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	cometbfttypes "github.com/cometbft/cometbft/types"
	"github.com/spf13/cobra"

	rollkitstore "github.com/rollkit/rollkit/pkg/store"
	rollkittypes "github.com/rollkit/rollkit/types"
)

var (
	flagDaHeight   = "da-height"
	maxMissedBlock = 50
)

// RollkitMigrationGenesis represents the minimal genesis for rollkit migration.
type RollkitMigrationGenesis struct {
	ChainID       string `json:"chain_id"`
	InitialHeight uint64 `json:"initial_height"`
	GenesisTime   int64  `json:"genesis_time"`
	Sequencer     []byte `json:"sequencer"`
}

// MigrateToRollkitCmd returns a command that migrates the data from the CometBFT chain to Rollkit.
func MigrateToRollkitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollkit-migrate",
		Short: "Migrate the data from the CometBFT chain to Rollkit",
		Long: `Migrate the data from the CometBFT chain to Rollkit. This command should be used to migrate nodes or the sequencer.

This command will:
1. Migrate all blocks from the CometBFT blockstore to the Rollkit store
2. Convert the CometBFT state to Rollkit state format
3. Create a minimal rollkit_genesis.json file for subsequent startups

After migration, start the node normally - it will automatically detect and use the rollkit_genesis.json file.`,
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			config, err := cometbftcmd.ParseConfig(cmd)
			if err != nil {
				return err
			}

			cometBlockStore, cometStateStore, err := loadStateAndBlockStore(config)
			if err != nil {
				return err
			}

			cometBFTstate, err := cometStateStore.Load()
			if err != nil {
				return err
			}

			lastBlockHeight := cometBFTstate.LastBlockHeight

			cmd.Printf("Last block height: %d\n", lastBlockHeight)

			rollkitStore, err := loadRollkitStateStore(config.RootDir)
			if err != nil {
				return err
			}

			daHeight, err := cmd.Flags().GetUint64(flagDaHeight)
			if err != nil {
				return fmt.Errorf("error reading %s flag: %w", flagDaHeight, err)
			}

			rollkitState, err := rollkitStateFromCometBFTState(cometBFTstate, daHeight)
			if err != nil {
				return err
			}

			if err = rollkitStore.UpdateState(ctx, rollkitState); err != nil {
				return fmt.Errorf("failed to update Rollkit state: %w", err)
			}

			// create minimal rollkit genesis file for future startups
			if err := createRollkitMigrationGenesis(config.RootDir, cometBFTstate); err != nil {
				return fmt.Errorf("failed to create rollkit migration genesis: %w", err)
			}

			cmd.Println("Created rollkit_genesis.json for migration - the node will use this on subsequent startups")

			// migrate all the blocks from the CometBFT block store to the rollkit store
			// the migration is done in reverse order, starting from the last block
			missedBlocks := make(map[int64]bool)
			for height := lastBlockHeight; height > 0; height-- {
				cmd.Printf("Migrating block %d...\n", height)

				if len(missedBlocks) >= maxMissedBlock {
					cmd.Println("Too many missed blocks, the node was probably pruned... Stopping now, everything should be fine.")
					break
				}

				block := cometBlockStore.LoadBlock(height)
				if block == nil {
					missedBlocks[height] = true
					cmd.Printf("Block %d not found in CometBFT block store, skipping...\n", height)
					continue
				}

				header, data, signature := cometBlockToRollkit(block)

				if err = rollkitStore.SaveBlockData(ctx, header, data, &signature); err != nil {
					return fmt.Errorf("failed to save block data: %w", err)
				}

				// Only save extended commit info if vote extensions are enabled
				if enabled := cometBFTstate.ConsensusParams.ABCI.VoteExtensionsEnabled(block.Height); enabled {
					extendedCommit := cometBlockStore.LoadBlockExtendedCommit(lastBlockHeight)

					extendedCommitInfo := abci.ExtendedCommitInfo{
						Round: extendedCommit.Round,
					}

					for _, vote := range extendedCommit.ToExtendedVoteSet("", cometBFTstate.LastValidators).List() {
						power := int64(0)
						for _, v := range cometBFTstate.LastValidators.Validators {
							if bytes.Equal(v.Address.Bytes(), vote.ValidatorAddress) {
								power = v.VotingPower
								break
							}
						}

						extendedCommitInfo.Votes = append(extendedCommitInfo.Votes, abci.ExtendedVoteInfo{
							Validator: abci.Validator{
								Address: vote.ValidatorAddress,
								Power:   power,
							},
							VoteExtension:      vote.Extension,
							ExtensionSignature: vote.ExtensionSignature,
							BlockIdFlag:        cmtproto.BlockIDFlag(vote.CommitSig().BlockIDFlag),
						})
					}

					_ = extendedCommitInfo
					// rollkitStore.SaveExtendedCommit(ctx, header.Height(), &extendedCommitInfo)
					panic("Saving extended commit info is not implemented yet")
				}

				cmd.Println("Block", height, "migrated")
			}

			// set the last height in the Rollkit store
			if err = rollkitStore.SetHeight(ctx, uint64(lastBlockHeight)); err != nil {
				return fmt.Errorf("failed to set last height in Rollkit store: %w", err)
			}

			cmd.Println("Migration completed successfully")
			return errors.Join(rollkitStore.Close(), cometBlockStore.Close(), cometStateStore.Close())
		},
	}

	cmd.Flags().Uint64(flagDaHeight, 1, "The DA height to set in the Rollkit state. Defaults to 1.")

	return cmd
}

// cometBlockToRollkit converts a cometBFT block to a rollkit block
func cometBlockToRollkit(block *cometbfttypes.Block) (*rollkittypes.SignedHeader, *rollkittypes.Data, rollkittypes.Signature) {
	var (
		header    *rollkittypes.SignedHeader
		data      *rollkittypes.Data
		signature rollkittypes.Signature
	)

	// find proposer signature
	for _, sig := range block.LastCommit.Signatures {
		if bytes.Equal(sig.ValidatorAddress.Bytes(), block.ProposerAddress.Bytes()) {
			signature = sig.Signature
			break
		}
	}

	header = &rollkittypes.SignedHeader{
		Header: rollkittypes.Header{
			BaseHeader: rollkittypes.BaseHeader{
				Height:  uint64(block.Height),
				Time:    uint64(block.Time.UnixNano()),
				ChainID: block.ChainID,
			},
			Version: rollkittypes.Version{
				Block: block.Version.Block,
				App:   block.Version.App,
			},
			LastHeaderHash:  block.LastBlockID.Hash.Bytes(),
			LastCommitHash:  block.LastCommitHash.Bytes(),
			DataHash:        block.DataHash.Bytes(),
			ConsensusHash:   block.ConsensusHash.Bytes(),
			AppHash:         block.AppHash.Bytes(),
			LastResultsHash: block.LastResultsHash.Bytes(),
			ValidatorHash:   block.ValidatorsHash.Bytes(),
			ProposerAddress: block.ProposerAddress.Bytes(),
		},
		Signature: signature, // TODO: figure out this.
	}

	data = &rollkittypes.Data{
		Metadata: &rollkittypes.Metadata{
			ChainID:      block.ChainID,
			Height:       uint64(block.Height),
			Time:         uint64(block.Time.UnixNano()),
			LastDataHash: block.DataHash.Bytes(),
		},
	}

	for _, tx := range block.Txs {
		data.Txs = append(data.Txs, rollkittypes.Tx(tx))
	}

	return header, data, signature
}

func loadStateAndBlockStore(config *cfg.Config) (*store.BlockStore, state.Store, error) {
	dbType := dbm.BackendType(config.DBBackend)

	if ok, err := fileExists(filepath.Join(config.DBDir(), "blockstore.db")); !ok || err != nil {
		return nil, nil, fmt.Errorf("no blockstore found in %v: %w", config.DBDir(), err)
	}

	// Get BlockStore
	blockStoreDB, err := dbm.NewDB("blockstore", dbType, config.DBDir())
	if err != nil {
		return nil, nil, err
	}
	blockStore := store.NewBlockStore(blockStoreDB)

	if ok, err := fileExists(filepath.Join(config.DBDir(), "state.db")); !ok || err != nil {
		return nil, nil, fmt.Errorf("no statestore found in %v: %w", config.DBDir(), err)
	}

	// Get StateStore
	stateDB, err := dbm.NewDB("state", dbType, config.DBDir())
	if err != nil {
		return nil, nil, err
	}
	stateStore := state.NewStore(stateDB, state.StoreOptions{
		DiscardABCIResponses: config.Storage.DiscardABCIResponses,
	})

	return blockStore, stateStore, nil
}

func loadRollkitStateStore(rootDir string) (rollkitstore.Store, error) {
	baseKV, err := rollkitstore.NewDefaultKVStore(rootDir, "data", "rollkit")
	if err != nil {
		return nil, err
	}

	return rollkitstore.New(baseKV), nil
}

func rollkitStateFromCometBFTState(cometBFTState state.State, daHeight uint64) (rollkittypes.State, error) {
	return rollkittypes.State{
		Version: rollkittypes.Version{
			Block: cometBFTState.Version.Consensus.Block,
			App:   cometBFTState.Version.Consensus.App,
		},
		ChainID:         cometBFTState.ChainID,
		InitialHeight:   uint64(cometBFTState.LastBlockHeight), // The initial height is the migration height
		LastBlockHeight: uint64(cometBFTState.LastBlockHeight),
		LastBlockTime:   cometBFTState.LastBlockTime,

		DAHeight: daHeight,

		LastResultsHash: cometBFTState.LastResultsHash,
		AppHash:         cometBFTState.AppHash,
	}, nil
}

// createRollkitMigrationGenesis creates a minimal rollkit genesis file for migration.
// This creates a lightweight genesis containing only the essential information needed
// for rollkit to start after migration. The full state is stored separately in the
// rollkit state store.
func createRollkitMigrationGenesis(rootDir string, cometBFTState state.State) error {
	// use the first validator as sequencer (assuming single validator setup for migration)
	var sequencerAddr []byte // TODO(@julienrbrt): Allow to rollkitmanager for sequencer address instead. However rollkitmanager is an optional module
	if len(cometBFTState.LastValidators.Validators) > 0 {
		sequencerAddr = cometBFTState.LastValidators.Validators[0].Address.Bytes()
	}

	migrationGenesis := RollkitMigrationGenesis{
		ChainID:       cometBFTState.ChainID,
		InitialHeight: uint64(cometBFTState.InitialHeight),
		GenesisTime:   cometBFTState.LastBlockTime.UnixNano(),
		Sequencer:     sequencerAddr,
	}

	genesisBytes, err := json.MarshalIndent(migrationGenesis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal rollkit migration genesis: %w", err)
	}

	genesisPath := filepath.Join(rootDir, rollkitGenesisFilename)
	if err := os.WriteFile(genesisPath, genesisBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write rollkit migration genesis to %s: %w", genesisPath, err)
	}

	return nil
}

// fileExists checks if a file/directory exists.
func fileExists(filename string) (bool, error) {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("error checking file %s: %w", filename, err)
	}

	return true, nil
}
