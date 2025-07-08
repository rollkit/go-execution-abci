package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	goheaderstore "github.com/celestiaorg/go-header/store"
	dbm "github.com/cometbft/cometbft-db"
	cometbftcmd "github.com/cometbft/cometbft/cmd/cometbft/commands"
	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	cometbfttypes "github.com/cometbft/cometbft/types"
	ds "github.com/ipfs/go-datastore"
	ktds "github.com/ipfs/go-datastore/keytransform"
	"github.com/spf13/cobra"

	rollkitnode "github.com/rollkit/rollkit/node"
	rollkitgenesis "github.com/rollkit/rollkit/pkg/genesis"
	rollkitstore "github.com/rollkit/rollkit/pkg/store"
	rollkittypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

var (
	flagDaHeight   = "da-height"
	maxMissedBlock = 50
)

// rollkitMigrationGenesis represents the minimal genesis for rollkit migration.
type rollkitMigrationGenesis struct {
	ChainID         string        `json:"chain_id"`
	InitialHeight   uint64        `json:"initial_height"`
	GenesisTime     int64         `json:"genesis_time"`
	SequencerAddr   []byte        `json:"sequencer_address"`
	SequencerPubKey crypto.PubKey `json:"sequencer_pub_key,omitempty"`
}

// ToRollkitGenesis converts the rollkit migration genesis to a Rollkit genesis.
func (g rollkitMigrationGenesis) ToRollkitGenesis() *rollkitgenesis.Genesis {
	genesis := rollkitgenesis.NewGenesis(
		g.ChainID,
		g.InitialHeight,
		time.Unix(0, g.GenesisTime),
		g.SequencerAddr,
	)

	return &genesis
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

			rollkitStores, err := loadRollkitStores(config.RootDir)
			if err != nil {
				return err
			}

			// save current CometBFT state to the ABCI exec store
			if err = rollkitStores.abciExecStore.SaveState(ctx, &cometBFTstate); err != nil {
				return fmt.Errorf("failed to save CometBFT state to ABCI exec store: %w", err)
			}

			daHeight, err := cmd.Flags().GetUint64(flagDaHeight)
			if err != nil {
				return fmt.Errorf("error reading %s flag: %w", flagDaHeight, err)
			}

			rollkitState, err := cometbftStateToRollkitState(cometBFTstate, daHeight)
			if err != nil {
				return err
			}

			if err = rollkitStores.rollkitStore.UpdateState(ctx, rollkitState); err != nil {
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
			initSyncStores := true

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

				if err = rollkitStores.rollkitStore.SaveBlockData(ctx, header, data, &signature); err != nil {
					return fmt.Errorf("failed to save block data: %w", err)
				}

				// set last data in sync stores
				if initSyncStores {
					if err = rollkitStores.headerSyncStore.Init(ctx, header); err != nil {
						return fmt.Errorf("failed to initialize header sync store: %w", err)
					}

					if err = rollkitStores.dataSyncStore.Init(ctx, data); err != nil {
						return fmt.Errorf("failed to initialize data sync store: %w", err)
					}

					initSyncStores = false
				}

				// Only save extended commit info if vote extensions are enabled
				if enabled := cometBFTstate.ConsensusParams.ABCI.VoteExtensionsEnabled(block.Height); enabled {
					cmd.Printf("⚠️⚠️⚠️ Vote extensions were enabled at height %d ⚠️⚠️⚠️\n", block.Height)
					cmd.Println("⚠️⚠️⚠️ Vote extensions have no effect when using Rollkit ⚠️⚠️⚠️")
					cmd.Println("⚠️⚠️⚠️ Please consult the docs ⚠️⚠️⚠️")
				}

				cmd.Println("Block", height, "migrated")
			}

			// set the last height in the Rollkit store
			if err = rollkitStores.rollkitStore.SetHeight(ctx, uint64(lastBlockHeight)); err != nil {
				return fmt.Errorf("failed to set last height in Rollkit store: %w", err)
			}

			cmd.Println("Migration completed successfully")
			return errors.Join(rollkitStores.rollkitStore.Close(), cometBlockStore.Close(), cometStateStore.Close())
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

type rollkitStores struct {
	rollkitStore    rollkitstore.Store
	abciExecStore   *adapter.Store
	dataSyncStore   *goheaderstore.Store[*rollkittypes.Data]
	headerSyncStore *goheaderstore.Store[*rollkittypes.SignedHeader]
}

func loadRollkitStores(rootDir string) (rollkitStores, error) {
	store, err := rollkitstore.NewDefaultKVStore(rootDir, "data", "rollkit")
	if err != nil {
		return rollkitStores{}, fmt.Errorf("failed to create rollkit store: %w", err)
	}

	rollkitPrefixStore := ktds.Wrap(store, &ktds.PrefixTransform{
		Prefix: ds.NewKey(rollkitnode.RollkitPrefix),
	})

	ds, err := goheaderstore.NewStore[*rollkittypes.Data](
		rollkitPrefixStore,
		goheaderstore.WithStorePrefix("dataSync"),
		goheaderstore.WithMetrics(),
	)
	if err != nil {
		return rollkitStores{}, err
	}

	hs, err := goheaderstore.NewStore[*rollkittypes.SignedHeader](
		rollkitPrefixStore,
		goheaderstore.WithStorePrefix("headerSync"),
		goheaderstore.WithMetrics(),
	)
	if err != nil {
		return rollkitStores{}, err
	}

	return rollkitStores{
		rollkitStore:    rollkitstore.New(rollkitPrefixStore),
		abciExecStore:   adapter.NewExecABCIStore(store),
		dataSyncStore:   ds,
		headerSyncStore: hs,
	}, nil
}

func cometbftStateToRollkitState(cometBFTState state.State, daHeight uint64) (rollkittypes.State, error) {
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
	var (
		sequencerAddr   []byte
		sequencerPubKey crypto.PubKey
	)

	// use the first validator as sequencer (assuming single validator setup for migration)
	if len(cometBFTState.LastValidators.Validators) == 1 {
		sequencerAddr = cometBFTState.LastValidators.Validators[0].Address.Bytes()
		sequencerPubKey = cometBFTState.LastValidators.Validators[0].PubKey
	} else {
		// TODO(@julienrbrt): Allow to use rollkitmngr state to get the sequencer address
		// ref: https://github.com/rollkit/go-execution-abci/issues/164
		return fmt.Errorf("expected exactly one validator in the last validators, found %d", len(cometBFTState.LastValidators.Validators))
	}

	migrationGenesis := rollkitMigrationGenesis{
		ChainID:         cometBFTState.ChainID,
		InitialHeight:   uint64(cometBFTState.InitialHeight),
		GenesisTime:     cometBFTState.LastBlockTime.UnixNano(),
		SequencerAddr:   sequencerAddr,
		SequencerPubKey: sequencerPubKey,
	}

	// using cmtjson for marshalling to ensure compatibility with cometbft genesis format
	genesisBytes, err := cmtjson.MarshalIndent(migrationGenesis, "", "  ")
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
